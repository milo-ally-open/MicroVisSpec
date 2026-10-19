//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var integrationDeviceID = uuid.MustParse("11111111-2222-4333-8444-555555555555")

func newEventAPIIntegration(t *testing.T) (*App, *gorm.DB, http.Handler, func()) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must point to an isolated integration-test database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !(strings.Contains(strings.ToLower(parsed.Path), "_it") || strings.Contains(strings.ToLower(parsed.Path), "test")) {
		t.Fatal("TEST_DATABASE_URL must name an isolated test database")
	}
	db, err := openDatabase(dsn)
	if err != nil {
		t.Fatalf("open event integration database: %v", err)
	}
	if err := db.Exec("TRUNCATE event.outbox, event.dispositions, event.evidence, event.events CASCADE").Error; err != nil {
		t.Fatalf("clear event integration tables: %v", err)
	}
	gin.SetMode(gin.TestMode)
	deviceStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/devices/"+integrationDeviceID.String()+"/http-authenticate" || r.Header.Get("X-Internal-Service-Token") != "device-it-token" {
			http.Error(w, "invalid device validation request", http.StatusBadRequest)
			return
		}
		var credential map[string]string
		if err := json.NewDecoder(r.Body).Decode(&credential); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if credential["username"] != "device-user" || credential["password"] != "device-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(deviceHTTPAuthResult{Allowed: true, DeviceID: integrationDeviceID})
	}))
	config := Config{EvidenceDir: t.TempDir(), DeviceServiceURL: deviceStub.URL, DeviceServiceToken: "device-it-token",
		PushServiceURL: "http://push.internal", PushServiceToken: "push-it-token", CursorSigningKey: bytes.Repeat([]byte{0x35}, 32)}
	app := NewApp(config, db, &captureEventMessages{}, deviceStub.Client())
	cleanup := func() {
		deviceStub.Close()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	return app, db, app.Handler(), cleanup
}

func insertAPIEvent(t *testing.T, db *gorm.DB, accountID, id uuid.UUID, occurredAt time.Time, status, severity string) Event {
	t.Helper()
	now := time.Now().UTC()
	event := Event{ID: id, AccountID: accountID, DeviceID: uuid.New(), CameraID: uuid.New(), EdgeEventID: uuid.New(),
		EventType: "classroom.behavior.hand-raising", Severity: severity, OccurredAt: occurredAt, ReceivedAt: now,
		DeviceName: "Atlas test", CameraName: "Front", Location: "Room 101", AlgorithmKey: "classroom_behavior_detection",
		AlgorithmVersion: "1.0.0", RuleConfigVersion: 1,
		Attributes: json.RawMessage("{\"behavior_class\":\"hand-raising\",\"confidence\":0.9,\"observed_duration_ms\":1000,\"rule_config_version\":1}"),
		Status:     status, Version: 1, CreatedAt: now}
	if isTerminalEventStatus(status) {
		event.ResolvedAt = &now
	}
	if err := db.Table(Event{}.TableName()).Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	return event
}

func eventRequest(handler http.Handler, method, path string, accountID uuid.UUID, body any) *httptest.ResponseRecorder {
	var requestBody io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		requestBody = bytes.NewReader(encoded)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, requestBody)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if accountID != uuid.Nil {
		request.Header.Set("X-Account-ID", accountID.String())
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestEventAPIUsesAccountIsolationSignedStableCursorAndVersionedTransitions(t *testing.T) {
	_, db, handler, cleanup := newEventAPIIntegration(t)
	defer cleanup()
	accountA, accountB := uuid.New(), uuid.New()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	id1 := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	id2 := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	id3 := uuid.MustParse("00000000-0000-4000-8000-000000000003")
	e1 := insertAPIEvent(t, db, accountA, id1, base, "pending", "high")
	_ = insertAPIEvent(t, db, accountA, id2, base, "pending", "medium")
	_ = insertAPIEvent(t, db, accountA, id3, base, "processing", "low")
	other := insertAPIEvent(t, db, accountB, uuid.New(), base, "pending", "critical")
	first := eventRequest(handler, http.MethodGet, "/api/v1/events?limit=2", accountA, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("list events returned %d: %s", first.Code, first.Body.String())
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	var items []Event
	var cursor string
	if err := json.Unmarshal(page["items"], &items); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(page["next_cursor"], &cursor); err != nil || len(items) != 2 || cursor == "" {
		t.Fatalf("first page invalid: items=%d cursor=%q err=%v", len(items), cursor, err)
	}
	if items[0].ID != id3 || items[1].ID != id2 {
		t.Fatalf("events with identical occurrence time were not stably ordered: %v", []uuid.UUID{items[0].ID, items[1].ID})
	}
	secondPath := "/api/v1/events?limit=2&cursor=" + url.QueryEscape(cursor)
	second := eventRequest(handler, http.MethodGet, secondPath, accountA, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("second page returned %d: %s", second.Code, second.Body.String())
	}
	page = nil
	if err := json.Unmarshal(second.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(page["items"], &items); err != nil || len(items) != 1 || items[0].ID != id1 {
		t.Fatalf("second page invalid: items=%v err=%v", items, err)
	}
	changedFilter := eventRequest(handler, http.MethodGet, secondPath+"&severity=high", accountA, nil)
	if changedFilter.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cursor reused with changed filters returned %d", changedFilter.Code)
	}
	if got := eventRequest(handler, http.MethodGet, "/api/v1/events/"+other.ID.String(), accountA, nil).Code; got != http.StatusNotFound {
		t.Fatalf("cross-account detail returned %d", got)
	}

	processing := eventRequest(handler, http.MethodPost, "/api/v1/events/"+e1.ID.String()+"/transitions", accountA,
		map[string]any{"to_status": "processing", "expected_version": 1})
	if processing.Code != http.StatusOK {
		t.Fatalf("pending->processing returned %d: %s", processing.Code, processing.Body.String())
	}
	noResult := eventRequest(handler, http.MethodPost, "/api/v1/events/"+e1.ID.String()+"/transitions", accountA,
		map[string]any{"to_status": "resolved", "result": " ", "expected_version": 2})
	if noResult.Code != http.StatusUnprocessableEntity {
		t.Fatalf("terminal transition without result returned %d", noResult.Code)
	}
	resolved := eventRequest(handler, http.MethodPost, "/api/v1/events/"+e1.ID.String()+"/transitions", accountA,
		map[string]any{"to_status": "resolved", "result": "teacher confirmed normal behavior", "expected_version": 2})
	if resolved.Code != http.StatusOK {
		t.Fatalf("processing->resolved returned %d: %s", resolved.Code, resolved.Body.String())
	}
	stale := eventRequest(handler, http.MethodPost, "/api/v1/events/"+e1.ID.String()+"/transitions", accountA,
		map[string]any{"to_status": "processing", "expected_version": 2})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale transition version returned %d", stale.Code)
	}
	var dispositionCount int64
	if err := db.Table(Disposition{}.TableName()).Where("event_id = ?", e1.ID).Count(&dispositionCount).Error; err != nil || dispositionCount != 2 {
		t.Fatalf("disposition history count=%d err=%v", dispositionCount, err)
	}
}

func TestEventStatsUseShanghaiDaysAndSeparateAllTimeOpenCount(t *testing.T) {
	_, db, handler, cleanup := newEventAPIIntegration(t)
	defer cleanup()
	account := uuid.New()
	location, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, location).UTC()
	insertAPIEvent(t, db, account, uuid.New(), today, "resolved", "high")
	insertAPIEvent(t, db, account, uuid.New(), time.Now().UTC().Add(-10*24*time.Hour), "pending", "low")
	response := eventRequest(handler, http.MethodGet, "/api/v1/events/stats?range=today", account, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("today stats returned %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "\"total_events\":1") || !strings.Contains(response.Body.String(), "\"open_events\":1") {
		t.Fatalf("today stats use the wrong interval or open-event scope: %s", response.Body.String())
	}
	empty := eventRequest(handler, http.MethodGet, "/api/v1/events/stats?range=30d", uuid.New(), nil)
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), "\"total_events\":0") {
		t.Fatalf("empty account stats invalid: %d %s", empty.Code, empty.Body.String())
	}
}

func TestEvidenceUploadDeduplicatesValidatesDeviceAndHonorsExpiry(t *testing.T) {
	_, db, handler, cleanup := newEventAPIIntegration(t)
	defer cleanup()
	account := uuid.New()
	event := Event{ID: uuid.New(), AccountID: account, DeviceID: integrationDeviceID, CameraID: uuid.New(), EdgeEventID: uuid.New(),
		EventType: "classroom.behavior.hand-raising", Severity: "high", OccurredAt: time.Now().UTC().Add(-time.Minute), ReceivedAt: time.Now().UTC(),
		DeviceName: "Atlas", CameraName: "Front", Location: "Room 101", AlgorithmKey: "classroom_behavior_detection", AlgorithmVersion: "1.0.0",
		RuleConfigVersion: 1, Attributes: json.RawMessage("{\"behavior_class\":\"hand-raising\",\"confidence\":0.9,\"observed_duration_ms\":1000,\"rule_config_version\":1}"),
		Status: "pending", Version: 1, CreatedAt: time.Now().UTC()}
	if err := db.Table(Event{}.TableName()).Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	var imageBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 90, A: 255})
		}
	}
	if err := jpeg.Encode(&imageBytes, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	upload := func(data []byte, username, password, contentType string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/edge/v1/devices/"+integrationDeviceID.String()+"/events/"+event.EdgeEventID.String()+"/evidence", bytes.NewReader(data))
		request.Header.Set("Content-Type", contentType)
		request.SetBasicAuth(username, password)
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	if rejected := upload(imageBytes.Bytes(), "device-user", "wrong-secret", "image/jpeg"); rejected.Code != http.StatusUnauthorized {
		t.Fatalf("invalid device credential returned %d", rejected.Code)
	}
	first := upload(imageBytes.Bytes(), "device-user", "device-secret", "image/jpeg")
	if first.Code != http.StatusCreated {
		t.Fatalf("valid evidence upload returned %d: %s", first.Code, first.Body.String())
	}
	var firstResult map[string]json.RawMessage
	if err := json.Unmarshal(first.Body.Bytes(), &firstResult); err != nil {
		t.Fatal(err)
	}
	var evidenceID uuid.UUID
	var firstExpiry time.Time
	var status string
	_ = json.Unmarshal(firstResult["evidence_id"], &evidenceID)
	_ = json.Unmarshal(firstResult["expires_at"], &firstExpiry)
	_ = json.Unmarshal(firstResult["status"], &status)
	if evidenceID == uuid.Nil || status != "available" {
		t.Fatalf("evidence response invalid: %s", first.Body.String())
	}
	second := upload(imageBytes.Bytes(), "device-user", "device-secret", "image/jpeg")
	var secondResult map[string]json.RawMessage
	_ = json.Unmarshal(second.Body.Bytes(), &secondResult)
	var secondID uuid.UUID
	var secondExpiry time.Time
	_ = json.Unmarshal(secondResult["evidence_id"], &secondID)
	_ = json.Unmarshal(secondResult["expires_at"], &secondExpiry)
	if second.Code != http.StatusOK || !secondExpiry.Equal(firstExpiry) || secondID != evidenceID {
		t.Fatalf("identical upload reset expiry or created another record: status=%d body=%s", second.Code, second.Body.String())
	}
	changedImage := image.NewRGBA(img.Bounds())
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			changedImage.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 90, A: 255})
		}
	}
	changedImage.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var changed bytes.Buffer
	if err := jpeg.Encode(&changed, changedImage, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if conflict := upload(changed.Bytes(), "device-user", "device-secret", "image/jpeg"); conflict.Code != http.StatusConflict {
		t.Fatalf("different valid image upload returned %d: %s", conflict.Code, conflict.Body.String())
	}
	read := eventRequest(handler, http.MethodGet, "/api/v1/events/"+event.ID.String()+"/evidence", account, nil)
	if read.Code != http.StatusOK || !bytes.Equal(read.Body.Bytes(), imageBytes.Bytes()) || read.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("evidence read failed or is cacheable: status=%d", read.Code)
	}
	if foreign := eventRequest(handler, http.MethodGet, "/api/v1/events/"+event.ID.String()+"/evidence", uuid.New(), nil); foreign.Code != http.StatusNotFound {
		t.Fatalf("cross-account evidence returned %d", foreign.Code)
	}
	if err := db.Table(Evidence{}.TableName()).Where("id = ?", evidenceID).Update("expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if expired := eventRequest(handler, http.MethodGet, "/api/v1/events/"+event.ID.String()+"/evidence", account, nil); expired.Code != http.StatusGone {
		t.Fatalf("expired evidence returned %d", expired.Code)
	}
}

func TestOutboxRetriesPushServiceFailureAndPersistsAcceptance(t *testing.T) {
	_, db, _, cleanup := newEventAPIIntegration(t)
	defer cleanup()
	var calls atomic.Int32
	pushStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/inbox" || r.Header.Get("X-Internal-Service") != "event-service" || r.Header.Get("X-Internal-Service-Token") != "push-it-token" {
			http.Error(w, "invalid push delivery", http.StatusBadRequest)
			return
		}
		var envelope pushInboxEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil || envelope.Producer != "event-service" || envelope.SchemaVersion != 1 || envelope.EventID == uuid.Nil {
			http.Error(w, "invalid envelope", http.StatusBadRequest)
			return
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer pushStub.Close()
	app := NewApp(Config{PushServiceURL: pushStub.URL, PushServiceToken: "push-it-token"}, db, nil, nil)
	app.pushHTTP = pushStub.Client()
	eventID := uuid.New()
	occurredAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	payload, _ := json.Marshal(map[string]any{"account_id": uuid.New(), "event_id": eventID, "location": "Room 101", "severity": "high",
		"occurred_at": occurredAt, "detail_path": "/events/" + eventID.String()})
	now := time.Now().UTC()
	item := Outbox{ID: eventID, EventType: "event.created", Payload: payload, CreatedAt: now, UpdatedAt: now, NextAttemptAt: now}
	if err := db.Table(Outbox{}.TableName()).Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := app.claimOutbox(context.Background())
	if err != nil || claimed == nil {
		t.Fatalf("claim outbox: item=%v err=%v", claimed, err)
	}
	if err := app.deliverOutbox(context.Background(), *claimed); err == nil {
		t.Fatal("503 response should remain retryable")
	}
	if err := app.retryOutbox(context.Background(), *claimed, io.ErrUnexpectedEOF); err != nil {
		t.Fatal(err)
	}
	if err := db.Table(Outbox{}.TableName()).Where("id = ?", eventID).Update("next_attempt_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err = app.claimOutbox(context.Background())
	if err != nil || claimed == nil {
		t.Fatalf("reclaim after temporary failure: item=%v err=%v", claimed, err)
	}
	if err := app.deliverOutbox(context.Background(), *claimed); err != nil {
		t.Fatal(err)
	}
	if err := app.ackOutbox(context.Background(), *claimed); err != nil {
		t.Fatal(err)
	}
	var delivered Outbox
	if err := db.Table(Outbox{}.TableName()).Where("id = ?", eventID).Take(&delivered).Error; err != nil {
		t.Fatal(err)
	}
	if delivered.DeliveredAt == nil || delivered.Attempts != 2 || calls.Load() != 2 {
		t.Fatalf("outbox did not record provider acceptance: %+v calls=%d", delivered, calls.Load())
	}
}
