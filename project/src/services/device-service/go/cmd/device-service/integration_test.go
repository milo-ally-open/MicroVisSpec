//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeDeviceBroker struct {
	mu             sync.Mutex
	provisions     int
	disables       int
	provisionError error
	disableError   error
}

func (b *fakeDeviceBroker) Check(context.Context) error { return nil }
func (b *fakeDeviceBroker) Provision(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.provisions++
	return b.provisionError
}
func (b *fakeDeviceBroker) Disable(context.Context, uuid.UUID, string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.disables++
	return b.disableError
}

func integrationApp(t *testing.T) (*gorm.DB, http.Handler) {
	t.Helper()
	dsn := strings.TrimSpace(env("TEST_DATABASE_URL", ""))
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set; service integration test requires an isolated PostgreSQL database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	name := strings.ToLower(strings.Trim(parsed.Path, "/"))
	if !strings.Contains(name, "_it") && !strings.Contains(name, "test") {
		t.Fatalf("refusing integration test against database %q; use a dedicated *_it or *test database", name)
	}
	db, err := openDatabase(dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	gin.SetMode(gin.TestMode)
	return db, NewHandler(db)
}

func requestAs(handler http.Handler, method, path string, account uuid.UUID, body any) *httptest.ResponseRecorder {
	var payload bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&payload).Encode(body)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, &payload)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if account != uuid.Nil {
		request.Header.Set("X-Account-ID", account.String())
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

type staticDeviceAuthorizer struct {
	deviceIDs  []uuid.UUID
	capability string
	called     bool
	err        error
}

func (a *staticDeviceAuthorizer) ListAuthorizedDevices(_ context.Context, _ uuid.UUID, _, capability, cursor string, limit int, _ string) ([]uuid.UUID, string, error) {
	a.called = true
	a.capability = capability
	if a.err != nil {
		return nil, "", a.err
	}
	if cursor != "" || limit < 1 {
		return nil, "", errors.New("unexpected pagination request")
	}
	return a.deviceIDs, "", nil
}

func (a *staticDeviceAuthorizer) CheckDeviceCapability(context.Context, uuid.UUID, string, uuid.UUID, string, string) error {
	return nil
}

func TestStreamCameraDiscoveryRejectsPCBeforeAuthorizationLookup(t *testing.T) {
	authorizer := &staticDeviceAuthorizer{}
	app := NewApp(Config{}, nil, nil, nil)
	app.authorization = authorizer
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices/stream-cameras", nil)
	request.Header.Set("X-Account-ID", uuid.NewString())
	request.Header.Set("X-Session-ID", uuid.NewString())
	request.Header.Set("X-User-Type", "pc")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("PC stream discovery status=%d body=%s", response.Code, response.Body.String())
	}
	if authorizer.called {
		t.Fatal("PC request reached mobile member device-grant lookup")
	}
}

func TestStreamCameraDiscoveryUsesOnlyStreamCapability(t *testing.T) {
	db, _ := integrationApp(t)
	accountID, deviceID, cameraID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	device := Device{ID: deviceID, AccountID: accountID, Name: "Lab camera hub", IP: "192.168.10.20", Location: "Room 1",
		ActivationState: "active", Active: true, ReportedState: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now}
	camera := Camera{ID: cameraID, AccountID: accountID, DeviceID: deviceID, Name: "North entrance", CameraKey: "north",
		Active: true, CameraState: "ready", StreamState: "ready", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&camera).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Delete(&Camera{}, "id = ?", cameraID).Error
		_ = db.Delete(&Device{}, "id = ?", deviceID).Error
	})
	authorizer := &staticDeviceAuthorizer{deviceIDs: []uuid.UUID{deviceID}}
	app := NewApp(Config{}, db, nil, nil)
	app.authorization = authorizer
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices/stream-cameras?limit=20", nil)
	request.Header.Set("X-Account-ID", uuid.NewString())
	request.Header.Set("X-Session-ID", uuid.NewString())
	request.Header.Set("X-User-Type", "mobile")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("stream camera list status=%d body=%s", response.Code, response.Body.String())
	}
	if authorizer.capability != "stream.view" {
		t.Fatalf("camera discovery must require stream.view, got %q", authorizer.capability)
	}
	body := response.Body.String()
	if !strings.Contains(body, cameraID.String()) || !strings.Contains(body, "North entrance") || !strings.Contains(body, "Lab camera hub") {
		t.Fatalf("authorized camera is missing: %s", body)
	}
	if strings.Contains(body, "192.168.10.20") || strings.Contains(body, "camera_key") || strings.Contains(body, "algorithms") {
		t.Fatalf("stream camera list exposed private device fields: %s", body)
	}
}

func TestDeviceServiceTenantIsolationAndDisableOutbox(t *testing.T) {
	db, handler := integrationApp(t)
	accountA, accountB := uuid.New(), uuid.New()
	defer db.Where("account_id IN ?", []uuid.UUID{accountA, accountB}).Delete(&Device{})
	defer db.Where("account_id IN ?", []uuid.UUID{accountA, accountB}).Delete(&Camera{})

	created := requestAs(handler, http.MethodPost, "/api/v1/devices", accountA, map[string]string{
		"name": "Atlas classroom", "ip": "192.168.0.2", "location": "Building A / Room 101",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create device: status %d, body %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		DeviceID string `json:"device_id"`
		State    string `json:"activation_state"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	deviceID, err := uuid.Parse(createdBody.DeviceID)
	if err != nil || createdBody.State != "pending" {
		t.Fatalf("unexpected create response: %+v", createdBody)
	}
	if got := requestAs(handler, http.MethodGet, "/api/v1/devices", accountA, nil).Code; got != http.StatusOK {
		t.Fatalf("owner list status = %d", got)
	}
	if got := requestAs(handler, http.MethodGet, "/api/v1/devices", accountB, nil).Body.String(); strings.Contains(got, deviceID.String()) {
		t.Fatalf("other account can see device: %s", got)
	}
	if got := requestAs(handler, http.MethodGet, "/api/v1/devices/"+deviceID.String(), accountB, nil).Code; got != http.StatusNotFound {
		t.Fatalf("cross-account detail should be 404, got %d", got)
	}
	if got := requestAs(handler, http.MethodPatch, "/api/v1/devices/"+deviceID.String(), accountB, map[string]string{"location": "Other"}).Code; got != http.StatusNotFound {
		t.Fatalf("cross-account patch should be 404, got %d", got)
	}

	camera := map[string]string{"name": "Front", "camera_key": "front-door"}
	if got := requestAs(handler, http.MethodPost, "/api/v1/devices/"+deviceID.String()+"/cameras", accountA, camera).Code; got != http.StatusCreated {
		t.Fatalf("create camera status = %d", got)
	}
	if got := requestAs(handler, http.MethodPost, "/api/v1/devices/"+deviceID.String()+"/cameras", accountA, camera).Code; got != http.StatusConflict {
		t.Fatalf("duplicate camera key status = %d", got)
	}
	if got := requestAs(handler, http.MethodGet, "/api/v1/devices/stats", accountA, nil).Body.String(); !strings.Contains(got, `"registered":1`) || !strings.Contains(got, `"online":0`) {
		t.Fatalf("new unactivated device should be registered and offline: %s", got)
	}

	if got := requestAs(handler, http.MethodDelete, "/api/v1/devices/"+deviceID.String(), accountB, nil).Code; got != http.StatusNotFound {
		t.Fatalf("cross-account disable should be 404, got %d", got)
	}
	if got := requestAs(handler, http.MethodDelete, "/api/v1/devices/"+deviceID.String(), accountA, nil).Code; got != http.StatusNoContent {
		t.Fatalf("disable status = %d", got)
	}
	if got := requestAs(handler, http.MethodDelete, "/api/v1/devices/"+deviceID.String(), accountA, nil).Code; got != http.StatusNoContent {
		t.Fatalf("repeated disable should be idempotent, got %d", got)
	}
	var outboxes []Outbox
	if err := db.Where("event_type = ? AND payload->>'device_id' = ?", "device.disabled", deviceID.String()).Find(&outboxes).Error; err != nil {
		t.Fatal(err)
	}
	if len(outboxes) != 1 {
		t.Fatalf("expected exactly one device.disabled outbox row, got %d", len(outboxes))
	}
	var payload map[string]string
	if err := json.Unmarshal(outboxes[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["account_id"] != accountA.String() || payload["device_id"] != deviceID.String() || len(payload) != 2 {
		t.Fatalf("outbox payload violates minimum contract: %v", payload)
	}
}

func TestDeviceServiceAcceptsAtlasAgentHeartbeatState(t *testing.T) {
	db, _ := integrationApp(t)
	accountID := uuid.New()
	created := requestAs(NewHandler(db), http.MethodPost, "/api/v1/devices", accountID, map[string]string{
		"name": "Atlas classroom", "ip": "192.168.0.2", "location": "Room 101",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create device: status %d, body %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	deviceID, err := uuid.Parse(createdBody.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Device{}).Where("id = ?", deviceID).Updates(map[string]any{
		"activation_state": "active", "active": true,
	}).Error; err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "device_id": deviceID.String(), "sent_at": time.Now().UTC().Format(time.RFC3339Nano),
		"device_online": true, "camera_state": "unknown", "npu_state": "not_ready",
		"model_catalog": map[string]any{"models": []any{}}, "model_state": "model_missing",
		"stream_state": "not_configured", "mqtt_state": "connected",
		"desired_config_version": 0, "applied_config_version": 0, "queue_depth": 0,
		"outbox": map[string]any{"pending": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(Config{}, db, nil, nil)
	topic := "devices/" + deviceID.String() + "/state"
	if err := app.processDeviceState(topic, payload); err != nil {
		t.Fatalf("accept Atlas agent heartbeat: %v", err)
	}
	var updated Device
	if err := db.First(&updated, "id = ?", deviceID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.LastSeenAt == nil {
		t.Fatal("valid Atlas heartbeat did not update last_seen_at")
	}
	var stored map[string]any
	if err := json.Unmarshal(updated.ReportedState, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["applied_config_version"] != float64(0) || stored["mqtt_state"] != "connected" {
		t.Fatalf("Atlas heartbeat fields were not persisted: %v", stored)
	}

	invalid := append([]byte(nil), payload[:len(payload)-1]...)
	invalid = append(invalid, []byte(`,"unknown_field":true}`)...)
	if err := app.processDeviceState(topic, invalid); err == nil {
		t.Fatal("unknown heartbeat field was accepted")
	}
}

func TestHTTPDeviceCredentialIsValidatedWithoutReturningSecrets(t *testing.T) {
	db, _ := integrationApp(t)
	deviceID := uuid.New()
	now := time.Now().UTC()
	device := Device{ID: deviceID, AccountID: uuid.New(), Name: "test", IP: "192.0.2.10", Location: "test", Active: true,
		ActivationState: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.Table(Device{}.TableName()).Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	password := "integration-test-device-password-with-entropy"
	credential := BrokerCredential{ID: uuid.New(), DeviceID: deviceID, Username: brokerUsername(deviceID), ClientID: brokerClientID(deviceID),
		PasswordHash: hashOpaque(password), CreatedAt: now}
	if err := db.Table(BrokerCredential{}.TableName()).Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithDependencies(Config{InternalToken: "device-it-internal"}, db, nil)
	request := func(candidate string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"username": credential.Username, "password": candidate})
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/devices/"+deviceID.String()+"/http-authenticate", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Service-Token", "device-it-internal")
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	accepted := request(password)
	if accepted.Code != http.StatusOK || !strings.Contains(accepted.Body.String(), `"allowed":true`) || strings.Contains(accepted.Body.String(), password) {
		t.Fatalf("valid credential response is invalid or leaks the secret: %d %s", accepted.Code, accepted.Body.String())
	}
	if rejected := request("wrong-password-with-enough-length-to-pass-input-check"); rejected.Code != http.StatusUnauthorized {
		t.Fatalf("wrong credential returned %d", rejected.Code)
	}
	if err := db.Table(Device{}.TableName()).Where("id = ?", deviceID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if rejected := request(password); rejected.Code != http.StatusUnauthorized {
		t.Fatalf("disabled device credential returned %d", rejected.Code)
	}
}

func TestDeviceListUsesCursorAndAccountScopedStats(t *testing.T) {
	db, handler := integrationApp(t)
	account := uuid.New()
	defer db.Where("account_id = ?", account).Delete(&Device{})
	for i := 0; i < 3; i++ {
		response := requestAs(handler, http.MethodPost, "/api/v1/devices", account, map[string]string{
			"name": fmt.Sprintf("Device %d", i), "ip": fmt.Sprintf("192.168.0.%d", i+2), "location": "Room 1",
		})
		if response.Code != http.StatusCreated {
			t.Fatalf("create device %d: %d %s", i, response.Code, response.Body.String())
		}
	}
	first := requestAs(handler, http.MethodGet, "/api/v1/devices?limit=2", account, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("list page: %d %s", first.Code, first.Body.String())
	}
	var page struct {
		Items      []DeviceView `json:"items"`
		NextCursor string       `json:"next_cursor"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("expected 2 items and a next cursor, got %d, cursor %q", len(page.Items), page.NextCursor)
	}
	second := requestAs(handler, http.MethodGet, "/api/v1/devices?limit=2&cursor="+url.QueryEscape(page.NextCursor), account, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("second page: %d %s", second.Code, second.Body.String())
	}
	var next struct {
		Items []DeviceView `json:"items"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].ID == page.Items[1].ID {
		t.Fatalf("cursor did not return the remaining device: %+v", next.Items)
	}
	if got := requestAs(handler, http.MethodGet, "/api/v1/devices/stats", uuid.New(), nil).Body.String(); !strings.Contains(got, `"registered":0`) {
		t.Fatalf("statistics must be account scoped, got %s", got)
	}

	var oldest Device
	if err := db.Where("account_id = ?", account).Order("created_at ASC, id ASC").First(&oldest).Error; err != nil {
		t.Fatal(err)
	}
	last := time.Now().UTC().Add(-heartbeatTimeout + time.Second)
	if err := db.Model(&Device{}).Where("id = ?", oldest.ID).Updates(map[string]any{"activation_state": "active", "last_seen_at": last}).Error; err != nil {
		t.Fatal(err)
	}
	if got := requestAs(handler, http.MethodGet, "/api/v1/devices/stats", account, nil).Body.String(); !strings.Contains(got, `"online":1`) {
		t.Fatalf("only fresh activated heartbeat should count online, got %s", got)
	}
}

func TestActivationTicketIsHashedOneTimeAndReissueInvalidatesOldTicket(t *testing.T) {
	db, _ := integrationApp(t)
	account := uuid.New()
	broker := &fakeDeviceBroker{}
	handler := NewHandlerWithDependencies(Config{BrokerURL: "ssl://mosquitto:8883", AdvertisedBrokerURL: "ssl://edge.example.test:18888", ActivationRatePerMin: 30}, db, broker)
	defer func() {
		var deviceIDs []uuid.UUID
		db.Model(&Device{}).Where("account_id = ?", account).Pluck("id", &deviceIDs)
		db.Where("device_id IN ?", deviceIDs).Delete(&ActivationTicket{})
		db.Where("device_id IN ?", deviceIDs).Delete(&BrokerCredential{})
		db.Where("account_id = ?", account).Delete(&Device{})
	}()

	created := requestAs(handler, http.MethodPost, "/api/v1/devices", account, map[string]string{"name": "Atlas", "ip": "192.168.0.2", "location": "Room 101"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create device: %d %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	deviceID, err := uuid.Parse(createdBody.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	issue := func() string {
		t.Helper()
		response := requestAs(handler, http.MethodPost, "/api/v1/devices/"+deviceID.String()+"/activation-tickets", account, map[string]string{})
		if response.Code != http.StatusCreated {
			t.Fatalf("issue activation ticket: %d %s", response.Code, response.Body.String())
		}
		var body struct {
			Ticket string `json:"activation_ticket"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Ticket) < 40 {
			t.Fatalf("ticket does not have sufficient entropy: length %d", len(body.Ticket))
		}
		var record ActivationTicket
		if err := db.Where("device_id = ? AND token_hash = ?", deviceID, hashOpaque(body.Ticket)).First(&record).Error; err != nil {
			t.Fatalf("ticket hash not found in database: %v", err)
		}
		if record.TokenHash == body.Ticket || !record.ExpiresAt.After(time.Now().UTC()) || record.ExpiresAt.Sub(record.CreatedAt) > activationTicketLifetime {
			t.Fatalf("ticket persistence violates hashed 10-minute contract: %+v", record)
		}
		return body.Ticket
	}
	activate := func(ticket string) *httptest.ResponseRecorder {
		t.Helper()
		return requestAs(handler, http.MethodPost, "/edge/v1/activate", uuid.Nil, map[string]string{"device_id": deviceID.String(), "ticket": ticket})
	}

	firstTicket := issue()
	first := activate(firstTicket)
	if first.Code != http.StatusOK {
		t.Fatalf("activate device: %d %s", first.Code, first.Body.String())
	}
	var firstBody struct {
		MQTT struct {
			BrokerURL string `json:"broker_url"`
			ClientID  string `json:"client_id"`
			Username  string `json:"username"`
			Password  string `json:"password"`
			Version   string `json:"protocol_version"`
			TLS       bool   `json:"tls_required"`
		} `json:"mqtt"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	if firstBody.MQTT.BrokerURL != "ssl://edge.example.test:18888" || firstBody.MQTT.BrokerURL == "ssl://mosquitto:8883" || firstBody.MQTT.ClientID != brokerClientID(deviceID) || firstBody.MQTT.Username != brokerUsername(deviceID) || len(firstBody.MQTT.Password) < 40 || firstBody.MQTT.Version != "3.1.1" || !firstBody.MQTT.TLS {
		t.Fatalf("unexpected MQTT activation response: %+v", firstBody.MQTT)
	}
	var credential BrokerCredential
	if err := db.Where("device_id = ?", deviceID).First(&credential).Error; err != nil {
		t.Fatal(err)
	}
	if credential.PasswordHash != hashOpaque(firstBody.MQTT.Password) || credential.PasswordHash == firstBody.MQTT.Password {
		t.Fatal("broker password must be stored as a hash only")
	}
	var active Device
	if err := db.Where("id = ?", deviceID).First(&active).Error; err != nil || active.ActivationState != "active" {
		t.Fatalf("device activation state not committed: %q, %v", active.ActivationState, err)
	}
	if replay := activate(firstTicket); replay.Code != http.StatusUnauthorized {
		t.Fatalf("consumed ticket replay should be rejected, got %d %s", replay.Code, replay.Body.String())
	}

	staleTicket := issue()
	currentTicket := issue()
	if replay := activate(staleTicket); replay.Code != http.StatusUnauthorized {
		t.Fatalf("reissued ticket should invalidate previous ticket, got %d", replay.Code)
	}
	second := activate(currentTicket)
	if second.Code != http.StatusOK {
		t.Fatalf("reactivation should rotate credentials for same device: %d %s", second.Code, second.Body.String())
	}
	if broker.provisions != 2 {
		t.Fatalf("expected exactly two successful provisioning calls, got %d", broker.provisions)
	}
}

func TestExpiredActivationTicketIsRejected(t *testing.T) {
	db, _ := integrationApp(t)
	account := uuid.New()
	broker := &fakeDeviceBroker{}
	handler := NewHandlerWithDependencies(Config{BrokerURL: "ssl://mosquitto:8883", AdvertisedBrokerURL: "ssl://edge.example.test:18888"}, db, broker)
	defer db.Where("account_id = ?", account).Delete(&Device{})
	created := requestAs(handler, http.MethodPost, "/api/v1/devices", account, map[string]string{"name": "Atlas", "ip": "192.168.0.2", "location": "Room 102"})
	var body struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	deviceID, _ := uuid.Parse(body.DeviceID)
	issued := requestAs(handler, http.MethodPost, "/api/v1/devices/"+deviceID.String()+"/activation-tickets", account, map[string]string{})
	var ticketBody struct {
		Ticket string `json:"activation_ticket"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &ticketBody); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&ActivationTicket{}).Where("token_hash = ?", hashOpaque(ticketBody.Ticket)).Update("expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	response := requestAs(handler, http.MethodPost, "/edge/v1/activate", uuid.Nil, map[string]string{"device_id": deviceID.String(), "ticket": ticketBody.Ticket})
	if response.Code != http.StatusGone || broker.provisions != 0 {
		t.Fatalf("expired activation ticket should not provision a broker client: status=%d provisions=%d", response.Code, broker.provisions)
	}
}

func TestActivationRequiresAdvertisedBrokerURL(t *testing.T) {
	db, _ := integrationApp(t)
	account := uuid.New()
	broker := &fakeDeviceBroker{}
	handler := NewHandlerWithDependencies(Config{BrokerURL: "ssl://mosquitto:8883"}, db, broker)
	defer db.Where("account_id = ?", account).Delete(&Device{})

	created := requestAs(handler, http.MethodPost, "/api/v1/devices", account, map[string]string{
		"name": "Atlas", "ip": "192.168.0.2", "location": "Room 101",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create device: %d", created.Code)
	}
	var deviceBody struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &deviceBody); err != nil {
		t.Fatal(err)
	}
	deviceID, err := uuid.Parse(deviceBody.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	issued := requestAs(handler, http.MethodPost, "/api/v1/devices/"+deviceID.String()+"/activation-tickets", account, map[string]string{})
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue activation ticket: %d", issued.Code)
	}
	var ticketBody struct {
		Ticket string `json:"activation_ticket"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &ticketBody); err != nil {
		t.Fatal(err)
	}
	response := requestAs(handler, http.MethodPost, "/edge/v1/activate", uuid.Nil, map[string]string{
		"device_id": deviceID.String(), "ticket": ticketBody.Ticket,
	})
	if response.Code != http.StatusServiceUnavailable || broker.provisions != 0 {
		t.Fatalf("activation without advertised broker URL must fail before provisioning; status=%d provisions=%d", response.Code, broker.provisions)
	}
	var stored ActivationTicket
	if err := db.Where("device_id = ? AND token_hash = ?", deviceID, hashOpaque(ticketBody.Ticket)).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ConsumedAt != nil {
		t.Fatal("activation ticket must remain unused when the advertised broker URL is missing")
	}
}

func TestDynamicSecuritySeparatesDeviceTopicPermissions(t *testing.T) {
	brokerURL := strings.TrimSpace(os.Getenv("TEST_MQTT_BROKER_URL"))
	adminUsername := os.Getenv("TEST_MQTT_ADMIN_USERNAME")
	adminPassword := os.Getenv("TEST_MQTT_ADMIN_PASSWORD")
	if brokerURL == "" || adminUsername == "" || adminPassword == "" {
		t.Skip("TEST_MQTT_BROKER_URL and isolated Dynamic Security admin credentials are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	manager := &DynamicSecurityBroker{url: brokerURL, username: adminUsername, password: adminPassword, clientID: "yunshao-dynsec-it-" + uuid.NewString(), timeout: 5 * time.Second}
	deviceA, deviceB := uuid.New(), uuid.New()
	usernameA, clientIDA, passwordA := brokerUsername(deviceA), brokerClientID(deviceA), "integration-device-secret-a"
	usernameB, clientIDB, passwordB := brokerUsername(deviceB), brokerClientID(deviceB), "integration-device-secret-b"
	if err := manager.Provision(ctx, deviceA, usernameA, clientIDA, passwordA); err != nil {
		t.Fatalf("provision device A Dynamic Security identity: %v", err)
	}
	if err := manager.Provision(ctx, deviceB, usernameB, clientIDB, passwordB); err != nil {
		t.Fatalf("provision device B Dynamic Security identity: %v", err)
	}

	observed := make(chan string, 8)
	observerOptions := mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID("yunshao-observer-it-" + uuid.NewString()).
		SetUsername(adminUsername).SetPassword(adminPassword).SetProtocolVersion(4).SetConnectTimeout(3 * time.Second)
	observerOptions.SetDefaultPublishHandler(func(_ mqtt.Client, message mqtt.Message) {
		select {
		case observed <- message.Topic():
		default:
		}
	})
	observer := mqtt.NewClient(observerOptions)
	connect := observer.Connect()
	if !connect.WaitTimeout(4*time.Second) || connect.Error() != nil {
		t.Fatalf("connect Dynamic Security observer: %v", connect.Error())
	}
	defer observer.Disconnect(100)
	observe := observer.Subscribe("devices/#", 1, nil)
	if !observe.WaitTimeout(4*time.Second) || observe.Error() != nil {
		t.Fatalf("subscribe test observer: %v", observe.Error())
	}

	deviceOptions := mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID(clientIDA).SetUsername(usernameA).
		SetPassword(passwordA).SetProtocolVersion(4).SetCleanSession(false).SetConnectTimeout(3 * time.Second)
	device := mqtt.NewClient(deviceOptions)
	deviceConnect := device.Connect()
	if !deviceConnect.WaitTimeout(4*time.Second) || deviceConnect.Error() != nil {
		t.Fatalf("connect provisioned device: %v", deviceConnect.Error())
	}
	defer device.Disconnect(100)

	ownTopic := "devices/" + deviceA.String() + "/commands"
	ownSubscribeToken := device.Subscribe(ownTopic, 1, nil)
	ownSubscribe, ok := ownSubscribeToken.(*mqtt.SubscribeToken)
	if !ok {
		t.Fatal("own command subscription did not return a SUBSCRIBE token")
	}
	if !ownSubscribe.WaitTimeout(4*time.Second) || ownSubscribe.Error() != nil {
		t.Fatalf("subscribe own command topic: %v", ownSubscribe.Error())
	}
	if granted := ownSubscribe.Result()[ownTopic]; granted > 1 {
		t.Fatalf("own command topic was not granted: %x", granted)
	}
	otherTopic := "devices/" + deviceB.String() + "/commands"
	otherSubscribeToken := device.Subscribe(otherTopic, 1, nil)
	otherSubscribe, ok := otherSubscribeToken.(*mqtt.SubscribeToken)
	if !ok {
		t.Fatal("cross-device subscription did not return a SUBSCRIBE token")
	}
	if !otherSubscribe.WaitTimeout(4 * time.Second) {
		t.Fatal("cross-device subscription did not receive a SUBACK")
	}
	if granted := otherSubscribe.Result()[otherTopic]; granted != 0x80 {
		t.Fatalf("device A must be denied device B command subscription, got grant code %x", granted)
	}

	ownState := "devices/" + deviceA.String() + "/state"
	ownPublish := device.Publish(ownState, 1, false, []byte(`{"schema_version":1}`))
	if !ownPublish.WaitTimeout(4*time.Second) || ownPublish.Error() != nil {
		t.Fatalf("publish own state topic: %v", ownPublish.Error())
	}
	select {
	case topic := <-observed:
		if topic != ownState {
			t.Fatalf("observer received unexpected topic %q", topic)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("broker observer did not receive device A own-topic message")
	}
	otherState := "devices/" + deviceB.String() + "/state"
	crossPublish := device.Publish(otherState, 1, false, []byte(`{"schema_version":1}`))
	_ = crossPublish.WaitTimeout(3 * time.Second)
	select {
	case topic := <-observed:
		if topic == otherState {
			t.Fatal("device A published to device B topic")
		}
	case <-time.After(500 * time.Millisecond):
	}

	if err := manager.Disable(ctx, deviceA, usernameA); err != nil {
		t.Fatalf("disable device A Dynamic Security identity: %v", err)
	}
}

func TestDeviceDisabledOutboxRetriesWithStableEnvelope(t *testing.T) {
	db, _ := integrationApp(t)
	if err := db.Where("delivered_at IS NULL").Delete(&Outbox{}).Error; err != nil {
		t.Fatal(err)
	}
	eventID := uuid.New()
	accountID := uuid.New()
	deviceID := uuid.New()
	payload, _ := json.Marshal(map[string]string{"account_id": accountID.String(), "device_id": deviceID.String()})
	item := Outbox{ID: eventID, EventType: "device.disabled", Payload: payload, CreatedAt: time.Now().UTC(), NextAttemptAt: time.Now().UTC().Add(-time.Second)}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Where("id = ?", eventID).Delete(&Outbox{})

	var mu sync.Mutex
	var observed []internalInboxEnvelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/inbox" {
			t.Errorf("unexpected inbox request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Internal-Service-Token"); got != "media-it-token" {
			t.Errorf("internal token header mismatch")
		}
		if got := r.Header.Get("X-Internal-Service"); got != "device-service" {
			t.Errorf("internal service identity header mismatch")
		}
		var envelope internalInboxEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode inbox envelope: %v", err)
		}
		mu.Lock()
		observed = append(observed, envelope)
		attempt := len(observed)
		mu.Unlock()
		if attempt == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	app := NewApp(Config{MediaServiceURL: server.URL, MediaServiceToken: "media-it-token"}, db, nil, nil)
	app.outboxHTTP = server.Client()
	ctx := context.Background()
	first, err := app.claimOutbox(ctx)
	if err != nil || first == nil {
		t.Fatalf("claim first delivery: item=%v err=%v", first, err)
	}
	if err := app.deliverOutbox(ctx, *first); err == nil {
		t.Fatal("first temporary inbox failure should be retried")
	}
	if err := app.retryOutbox(ctx, *first, errors.New("temporary inbox failure")); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Outbox{}).Where("id = ?", eventID).Update("next_attempt_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	second, err := app.claimOutbox(ctx)
	if err != nil || second == nil {
		t.Fatalf("claim retry: item=%v err=%v", second, err)
	}
	if err := app.deliverOutbox(ctx, *second); err != nil {
		t.Fatal(err)
	}
	if err := app.acknowledgeOutbox(ctx, *second); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 2 || observed[0].EventID != eventID || observed[1].EventID != eventID {
		t.Fatalf("retries must preserve stable event id, got %+v", observed)
	}
	for _, envelope := range observed {
		if envelope.Producer != "device-service" || envelope.EventType != "device.disabled" || envelope.SchemaVersion != 1 || envelope.Payload == nil {
			t.Fatalf("invalid internal envelope: %+v", envelope)
		}
	}
	var stored Outbox
	if err := db.First(&stored, "id = ?", eventID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.DeliveredAt == nil || stored.Attempts != 2 || stored.LeaseID != nil || stored.LeaseUntil != nil {
		t.Fatalf("outbox delivery state was not finalized: %+v", stored)
	}
}

func TestDisabledBrokerIdentityIsReconciledAfterTransientFailure(t *testing.T) {
	db, _ := integrationApp(t)
	if err := db.Where("revoked_at IS NULL").Delete(&BrokerCredential{}).Error; err != nil {
		t.Fatal(err)
	}
	deviceID := uuid.New()
	credentialID := uuid.New()
	device := Device{ID: deviceID, AccountID: uuid.New(), Name: "Atlas", IP: "192.168.0.2", Location: "Room 1", Active: false, ActivationState: "disabled", ReportedState: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	credential := BrokerCredential{ID: credentialID, DeviceID: deviceID, Username: brokerUsername(deviceID), ClientID: brokerClientID(deviceID), PasswordHash: hashOpaque("test-only"), CreatedAt: time.Now().UTC()}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Device{}).Where("id = ?", deviceID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&BrokerCredential{}, "id = ?", credentialID)
	defer db.Delete(&Device{}, "id = ?", deviceID)

	broker := &fakeDeviceBroker{disableError: errors.New("temporary broker outage")}
	app := NewApp(Config{}, db, broker, nil)
	if err := app.revokeDeviceCredentials(context.Background(), deviceID); err == nil {
		t.Fatal("broker failure must leave revocation pending")
	}
	var pending BrokerCredential
	if err := db.First(&pending, "id = ?", credentialID).Error; err != nil || pending.RevokedAt != nil {
		t.Fatalf("failed broker revoke was recorded as successful: credential=%+v err=%v", pending, err)
	}
	broker.mu.Lock()
	broker.disableError = nil
	broker.mu.Unlock()
	app.retryBrokerRevocations(context.Background())
	if err := db.First(&pending, "id = ?", credentialID).Error; err != nil || pending.RevokedAt == nil {
		t.Fatalf("broker reconciliation did not record successful revocation: credential=%+v err=%v", pending, err)
	}
}

func TestInternalCameraOwnershipReturnsTrustedSnapshot(t *testing.T) {
	db, _ := integrationApp(t)
	device := Device{ID: uuid.New(), AccountID: uuid.New(), Name: "Atlas classroom", IP: "192.168.0.2", Location: "Room 102", Active: true, ActivationState: "active", ReportedState: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	camera := Camera{ID: uuid.New(), AccountID: device.AccountID, DeviceID: device.ID, Name: "Front camera", CameraKey: "front", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&camera).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&Camera{}, "id = ?", camera.ID)
	defer db.Delete(&Device{}, "id = ?", device.ID)

	handler := NewHandlerWithDependencies(Config{InternalToken: "device-internal-it-token"}, db, nil)
	path := "/internal/v1/devices/" + device.ID.String() + "/cameras/" + camera.ID.String() + "/ownership"
	request := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("X-Internal-Service-Token", token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if response := request("wrong"); response.Code != http.StatusUnauthorized {
		t.Fatalf("ownership query without the correct service credential should be rejected, got %d", response.Code)
	}
	response := request("device-internal-it-token")
	if response.Code != http.StatusOK {
		t.Fatalf("ownership query status=%d body=%s", response.Code, response.Body.String())
	}
	var snapshot CameraOwnershipSnapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.AccountID != device.AccountID || snapshot.DeviceID != device.ID || snapshot.CameraID != camera.ID || snapshot.DeviceLocation != device.Location || snapshot.CameraName != camera.Name {
		t.Fatalf("ownership query returned an untrusted or incomplete snapshot: %+v", snapshot)
	}
	globalRequest := httptest.NewRequest(http.MethodGet, "/internal/v1/cameras/"+camera.ID.String()+"/ownership", nil)
	globalRequest.Header.Set("X-Internal-Service-Token", "device-internal-it-token")
	globalResponse := httptest.NewRecorder()
	handler.ServeHTTP(globalResponse, globalRequest)
	if globalResponse.Code != http.StatusOK {
		t.Fatalf("camera-id ownership query status=%d body=%s", globalResponse.Code, globalResponse.Body.String())
	}
	if err := db.Model(&Device{}).Where("id = ?", device.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if response := request("device-internal-it-token"); response.Code != http.StatusNotFound {
		t.Fatalf("disabled device ownership should be rejected, got %d", response.Code)
	}
}
