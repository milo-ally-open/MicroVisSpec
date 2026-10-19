//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type captureEventMessages struct {
	mu       sync.Mutex
	topics   []string
	payloads [][]byte
}

func (m *captureEventMessages) Check(context.Context) error                       { return nil }
func (m *captureEventMessages) Start(context.Context, func(string, []byte)) error { return nil }
func (m *captureEventMessages) Close()                                            {}
func (m *captureEventMessages) Publish(_ context.Context, topic string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topics = append(m.topics, topic)
	m.payloads = append(m.payloads, append([]byte(nil), payload...))
	return nil
}

func TestEventPersistenceDeduplicatesAndOnlyACKsDurableOrPermanentResults(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must point at an isolated PostgreSQL test database")
	}
	db, err := openDatabase(dsn)
	if err != nil {
		t.Fatalf("open isolated test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("TRUNCATE event.outbox, event.dispositions, event.evidence, event.events CASCADE").Error; err != nil {
		t.Fatalf("clear isolated event test database: %v", err)
	}

	deviceID, cameraID, accountID := uuid.New(), uuid.New(), uuid.New()
	var validationStatus int
	var validationReason string
	deviceStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/devices/"+deviceID.String()+"/classroom-events/validate" || r.Header.Get("X-Internal-Service-Token") != "device-token" {
			t.Errorf("unexpected ownership request %s %s", r.Method, r.URL.Path)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		var validation classroomEventValidation
		if err := json.NewDecoder(r.Body).Decode(&validation); err != nil {
			t.Errorf("decode rule validation: %v", err)
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if validation.CameraID != cameraID.String() || validation.BehaviorClass != "hand-raising" || validation.RuleConfigVersion != 7 {
			t.Errorf("unexpected rule validation payload: %+v", validation)
		}
		w.WriteHeader(validationStatus)
		if validationStatus == http.StatusOK {
			_ = json.NewEncoder(w).Encode(ownershipResult{Allowed: true, AccountID: accountID, DeviceID: deviceID, DeviceName: "Atlas room 1", CameraID: cameraID, CameraName: "front", Location: "Room 1", AlgorithmVersion: "1.0.0", RuleConfigVersion: 7})
		} else if validationStatus == http.StatusUnprocessableEntity {
			_ = json.NewEncoder(w).Encode(map[string]string{"reason_code": validationReason})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "dependency unavailable"})
		}
	}))
	defer deviceStub.Close()

	messages := &captureEventMessages{}
	app := NewApp(Config{EvidenceDir: t.TempDir(), DeviceServiceURL: deviceStub.URL, DeviceServiceToken: "device-token"}, db, messages, deviceStub.Client())
	edgeEvent := EdgeEvent{
		SchemaVersion: 1, DeviceID: deviceID, SentAt: time.Now().UTC(), EventID: uuid.New(), CameraID: cameraID,
		EventType: "classroom.behavior.hand-raising", Severity: "medium", OccurredAt: time.Now().UTC().Add(-time.Second),
		AlgorithmKey: "classroom_behavior_detection", AlgorithmVersion: "1.0.0",
		Attributes: json.RawMessage(`{"behavior_class":"hand-raising","confidence":0.92,"observed_duration_ms":1200,"rule_config_version":7}`),
	}
	payload, err := json.Marshal(edgeEvent)
	if err != nil {
		t.Fatal(err)
	}
	topic := "devices/" + deviceID.String() + "/events"
	validationStatus = http.StatusOK
	first, err := app.processEdgeEvent(context.Background(), topic, payload)
	if err != nil || first == nil || first.Result != "accepted" {
		t.Fatalf("first event should persist before accepted ACK: ack=%+v err=%v", first, err)
	}
	second, err := app.processEdgeEvent(context.Background(), topic, payload)
	if err != nil || second == nil || second.Result != "duplicate" || second.EventID != first.EventID {
		t.Fatalf("repeated event should return the same durable result: ack=%+v err=%v", second, err)
	}
	var eventCount, outboxCount int64
	var storedEvent Event
	if err := db.Table(Event{}.TableName()).Where("device_id = ? AND edge_event_id = ?", deviceID, edgeEvent.EventID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(Event{}.TableName()).Where("device_id = ? AND edge_event_id = ?", deviceID, edgeEvent.EventID).Take(&storedEvent).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(Outbox{}.TableName()).Where("id = ? AND event_type = ?", storedEvent.ID, "event.created").Count(&outboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || outboxCount != 1 {
		t.Fatalf("idempotency failed: events=%d outbox=%d", eventCount, outboxCount)
	}

	validationStatus, validationReason = http.StatusUnprocessableEntity, "rule_disabled"
	rejected, err := app.processEdgeEvent(context.Background(), topic, newEventPayload(t, edgeEvent, uuid.New()))
	if err != nil || rejected == nil || rejected.Result != "rejected" || rejected.ReasonCode != "rule_disabled" {
		t.Fatalf("permanent rule mismatch should get rejection ACK: ack=%+v err=%v", rejected, err)
	}
	validationStatus = http.StatusServiceUnavailable
	transient, err := app.processEdgeEvent(context.Background(), topic, newEventPayload(t, edgeEvent, uuid.New()))
	if err == nil || transient != nil {
		t.Fatalf("temporary device-service failure must not send a terminal ACK: ack=%+v err=%v", transient, err)
	}

	validationStatus = http.StatusOK
	app.handleMQTTMessage(topic, payload)
	messages.mu.Lock()
	defer messages.mu.Unlock()
	if len(messages.topics) != 1 || messages.topics[0] != "devices/"+deviceID.String()+"/event-acks" {
		t.Fatalf("application ACK sent to wrong topic: %v", messages.topics)
	}
	var ack EventApplicationAck
	if err := json.Unmarshal(messages.payloads[0], &ack); err != nil || ack.Result != "duplicate" || ack.EventID != edgeEvent.EventID || ack.DeviceID != deviceID {
		t.Fatalf("unexpected MQTT application ACK: %+v err=%v", ack, err)
	}
}

func newEventPayload(t *testing.T, original EdgeEvent, eventID uuid.UUID) []byte {
	t.Helper()
	original.EventID = eventID
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestMalformedEventDoesNotACKWithoutStableEventID(t *testing.T) {
	deviceID := uuid.New()
	app := NewApp(Config{}, nil, &captureEventMessages{}, nil)
	ack, err := app.processEdgeEvent(context.Background(), "devices/"+deviceID.String()+"/events", []byte(`{"schema_version":1}`))
	if err != nil || ack != nil {
		t.Fatalf("event without stable ID cannot be acknowledged: ack=%+v err=%v", ack, err)
	}
}
