//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type controlledPushSender struct {
	mu       sync.Mutex
	statuses []int
	calls    atomic.Int32
}

func (s *controlledPushSender) Send(context.Context, webpush.Subscription, []byte, string) (*http.Response, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	status := http.StatusCreated
	if len(s.statuses) > 0 {
		status, s.statuses = s.statuses[0], s.statuses[1:]
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}

func integrationApp(t *testing.T) (*App, *gorm.DB, *controlledPushSender) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must point to an isolated integration-test database")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !(strings.Contains(strings.ToLower(parsed.Path), "test") || strings.Contains(strings.ToLower(parsed.Path), "_it")) {
		t.Fatal("TEST_DATABASE_URL must name an isolated test database")
	}
	db, err := openDatabase(dsn)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	db.Logger = logger.Default.LogMode(logger.Silent)
	gin.SetMode(gin.ReleaseMode)
	if err := db.Exec("TRUNCATE push.deliveries, push.session_revocations, push.inbox, push.subscriptions").Error; err != nil {
		t.Fatalf("clear push integration tables: %v", err)
	}
	sender := &controlledPushSender{}
	app, err := NewApp(Config{PublicOrigin: "https://app.example.test", VAPIDPublicKey: "public-test-key", VAPIDPrivateKey: "private-test-key",
		VAPIDSubject: "mailto:ops@example.test", EncryptionKey: bytes.Repeat([]byte{0x42}, 32), AuthServiceToken: "auth-it-token",
		EventServiceToken: "event-it-token", MaxEventAge: 24 * time.Hour}, db, sender)
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return app, db, sender
}

func makeSubscription(t *testing.T, app *App, accountID, sessionID uuid.UUID, endpoint string) uuid.UUID {
	t.Helper()
	priv, x, y, err := elliptic.GenerateKey(elliptic.P256(), bytes.NewReader(bytes.Repeat([]byte{7}, 64)))
	if err != nil {
		// A deterministic reader can be rejected if a scalar falls outside the curve order.
		priv, x, y, err = elliptic.GenerateKey(elliptic.P256(), strings.NewReader(strings.Repeat("x", 256)))
	}
	if err != nil {
		t.Fatalf("generate browser key: %v", err)
	}
	_ = priv
	p256dh := elliptic.Marshal(elliptic.P256(), x, y)
	input := subscriptionInput{Endpoint: endpoint}
	input.Keys.P256dh = base64.RawURLEncoding.EncodeToString(p256dh)
	input.Keys.Auth = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 16))
	body, _ := json.Marshal(input)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/push/subscriptions", bytes.NewReader(body))
	request.Header.Set("X-Account-ID", accountID.String())
	request.Header.Set("X-Session-ID", sessionID.String())
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("subscription upsert returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		SubscriptionID uuid.UUID `json:"subscription_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.SubscriptionID == uuid.Nil {
		t.Fatalf("invalid subscription response: %v %s", err, recorder.Body.String())
	}
	return result.SubscriptionID
}

func postInbox(t *testing.T, app *App, producer, token string, value any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/inbox", bytes.NewReader(body))
	request.Header.Set("X-Internal-Service", producer)
	request.Header.Set("X-Internal-Service-Token", token)
	app.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestInboxDeduplicatesEventsAndTombstonesRevokedSessions(t *testing.T) {
	app, db, _ := integrationApp(t)
	accountID, sessionID := uuid.New(), uuid.New()
	makeSubscription(t, app, accountID, sessionID, "https://push.example.test/subscription/one")
	eventID := uuid.New()
	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	payload := createdEventPayload{AccountID: accountID, EventID: eventID, Location: "C101", Severity: "high", OccurredAt: occurredAt, DetailPath: "/events/" + eventID.String()}
	envelope := internalInboxEnvelope{Producer: "event-service", EventType: "event.created", EventID: eventID, SchemaVersion: 1,
		OccurredAt: occurredAt, Payload: mustJSONRaw(t, payload)}
	for attempt := 0; attempt < 2; attempt++ {
		recorder := postInbox(t, app, "event-service", "event-it-token", envelope)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("event inbox attempt %d returned %d: %s", attempt, recorder.Code, recorder.Body.String())
		}
	}
	var inboxCount, deliveryCount int64
	if err := db.Table(Inbox{}.TableName()).Where("event_id = ? AND event_type = ?", eventID, "event.created").Count(&inboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(Delivery{}.TableName()).Where("event_id = ?", eventID).Count(&deliveryCount).Error; err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 || deliveryCount != 1 {
		t.Fatalf("duplicate inbox created inbox=%d deliveries=%d", inboxCount, deliveryCount)
	}

	revoked := uuid.New()
	revocation := internalInboxEnvelope{Producer: "auth-service", EventType: "session.revoked", EventID: revoked, SchemaVersion: 1,
		OccurredAt: time.Now().UTC(), Payload: mustJSONRaw(t, revokedSessionPayload{AccountID: accountID, SessionID: sessionID})}
	recorder := postInbox(t, app, "auth-service", "auth-it-token", revocation)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("session revocation returned %d: %s", recorder.Code, recorder.Body.String())
	}
	var subscription Subscription
	if err := db.Table(Subscription{}.TableName()).Where("account_id = ?", accountID).Take(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	if subscription.DisabledAt == nil {
		t.Fatal("session revocation did not disable its subscription")
	}
	var delivery Delivery
	if err := db.Table(Delivery{}.TableName()).Where("event_id = ?", eventID).Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.Status != "cancelled" {
		t.Fatalf("expected revoked-session delivery to cancel, got %q", delivery.Status)
	}
	if got := makeSubscriptionStatus(app, accountID, sessionID, "https://push.example.test/subscription/two"); got != http.StatusUnauthorized {
		t.Fatalf("revoked session rebound returned %d, want 401", got)
	}
}

func TestEndpointRebindAndForeignDeleteDoNotCrossAccounts(t *testing.T) {
	app, db, _ := integrationApp(t)
	accountA, sessionA := uuid.New(), uuid.New()
	accountB, sessionB := uuid.New(), uuid.New()
	endpoint := "https://push.example.test/subscription/rebound"
	subscriptionID := makeSubscription(t, app, accountA, sessionA, endpoint)
	now := time.Now().UTC()
	oldEventID := uuid.New()
	oldDelivery := Delivery{ID: uuid.New(), EventID: oldEventID, SubscriptionID: subscriptionID, AccountID: accountA,
		Payload: []byte(`{}`), Status: "pending", NextAttemptAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := db.Table(Delivery{}.TableName()).Create(&oldDelivery).Error; err != nil {
		t.Fatal(err)
	}
	newID := makeSubscription(t, app, accountB, sessionB, endpoint)
	var rebound Subscription
	if err := db.Table(Subscription{}.TableName()).Where("id = ?", newID).Take(&rebound).Error; err != nil {
		t.Fatal(err)
	}
	if rebound.AccountID != accountB || rebound.SourceSessionID != sessionB {
		t.Fatal("endpoint was not rebound to the current account and session")
	}
	var delivery Delivery
	if err := db.Table(Delivery{}.TableName()).Where("id = ?", oldDelivery.ID).Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.Status != "cancelled" {
		t.Fatalf("old account delivery remained %q after endpoint rebind", delivery.Status)
	}
	foreignEventID := uuid.New()
	foreignDelivery := Delivery{ID: uuid.New(), EventID: foreignEventID, SubscriptionID: newID, AccountID: accountB,
		Payload: []byte(`{}`), Status: "pending", NextAttemptAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := db.Table(Delivery{}.TableName()).Create(&foreignDelivery).Error; err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/push/subscriptions/"+newID.String(), nil)
	request.Header.Set("X-Account-ID", accountA.String())
	request.Header.Set("X-Session-ID", sessionA.String())
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("foreign delete returned %d", recorder.Code)
	}
	delivery = Delivery{}
	if err := db.Table(Delivery{}.TableName()).Where("id = ?", foreignDelivery.ID).Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.Status != "pending" {
		t.Fatalf("foreign delete changed another account delivery to %q", delivery.Status)
	}
}

func TestDeliveryRetriesAndDistinguishesAcceptedFromDisplayed(t *testing.T) {
	app, db, sender := integrationApp(t)
	accountID, sessionID := uuid.New(), uuid.New()
	subscriptionID := makeSubscription(t, app, accountID, sessionID, "https://push.example.test/subscription/retry")
	sender.statuses = []int{http.StatusTooManyRequests, http.StatusCreated}
	item := insertDelivery(t, db, accountID, subscriptionID, uuid.New(), time.Now().UTC().Add(time.Hour))
	claimed, err := app.claimDelivery(context.Background())
	if err != nil || claimed == nil || claimed.ID != item.ID {
		t.Fatalf("claim failed: item=%v err=%v", claimed, err)
	}
	if err := app.deliverClaimed(context.Background(), *claimed); err != nil {
		t.Fatal(err)
	}
	var current Delivery
	if err := db.Table(Delivery{}.TableName()).Where("id = ?", item.ID).Take(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != "pending" || current.LastClass != "temporary_http_error" || current.Attempts != 1 {
		t.Fatalf("429 did not retain a retryable delivery: %+v", current)
	}
	if err := db.Table(Delivery{}.TableName()).Where("id = ?", item.ID).Update("next_attempt_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err = app.claimDelivery(context.Background())
	if err != nil || claimed == nil {
		t.Fatalf("second claim failed: item=%v err=%v", claimed, err)
	}
	if err := app.deliverClaimed(context.Background(), *claimed); err != nil {
		t.Fatal(err)
	}
	if err := db.Table(Delivery{}.TableName()).Where("id = ?", item.ID).Take(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != "accepted" || current.AcceptedAt == nil || current.LastClass != "accepted" || sender.calls.Load() != 2 {
		t.Fatalf("successful provider acceptance was recorded incorrectly: %+v calls=%d", current, sender.calls.Load())
	}
}

func TestConcurrentWorkersClaimDeliveryOnlyOnce(t *testing.T) {
	app, db, _ := integrationApp(t)
	accountID, subscriptionID := uuid.New(), uuid.New()
	insertDelivery(t, db, accountID, subscriptionID, uuid.New(), time.Now().UTC().Add(time.Hour))
	var claimed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := app.claimDelivery(context.Background())
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if item != nil {
				claimed.Add(1)
			}
		}()
	}
	wg.Wait()
	if count := claimed.Load(); count != 1 {
		t.Fatalf("expected one worker to claim the delivery, got %d", count)
	}
}

func TestGoneSubscriptionIsStoppedAndOldDeliveryExpires(t *testing.T) {
	app, db, sender := integrationApp(t)
	accountID, sessionID := uuid.New(), uuid.New()
	subscriptionID := makeSubscription(t, app, accountID, sessionID, "https://push.example.test/subscription/gone")
	sender.statuses = []int{http.StatusGone}
	current := insertDelivery(t, db, accountID, subscriptionID, uuid.New(), time.Now().UTC().Add(time.Hour))
	other := insertDelivery(t, db, accountID, subscriptionID, uuid.New(), time.Now().UTC().Add(time.Hour))
	claimed, err := app.claimDelivery(context.Background())
	if err != nil || claimed == nil {
		t.Fatalf("claim before 410 failed: item=%v err=%v", claimed, err)
	}
	if err := app.deliverClaimed(context.Background(), *claimed); err != nil {
		t.Fatal(err)
	}
	var subscription Subscription
	if err := db.Table(Subscription{}.TableName()).Where("id = ?", subscriptionID).Take(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	if subscription.DisabledAt == nil {
		t.Fatal("410 response did not disable the invalid subscription")
	}
	var delivery Delivery
	if err := db.Table(Delivery{}.TableName()).Where("id = ?", current.ID).Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.Status != "permanent_failure" || delivery.LastClass != "subscription_gone" {
		t.Fatalf("410 response was not classified permanently: %+v", delivery)
	}
	delivery = Delivery{}
	if err := db.Table(Delivery{}.TableName()).Where("id = ?", other.ID).Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.Status != "cancelled" {
		t.Fatalf("pending sibling delivery was not cancelled: %q", delivery.Status)
	}

	expired := insertDelivery(t, db, accountID, uuid.New(), uuid.New(), time.Now().UTC().Add(-time.Second))
	if err := app.expirePendingDeliveries(context.Background()); err != nil {
		t.Fatal(err)
	}
	delivery = Delivery{}
	if err := db.Table(Delivery{}.TableName()).Where("id = ?", expired.ID).Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.Status != "expired" || delivery.LastClass != "event_too_old" {
		t.Fatalf("expired delivery was not stopped: %+v", delivery)
	}
}

func insertDelivery(t *testing.T, db *gorm.DB, accountID, subscriptionID, eventID uuid.UUID, expires time.Time) Delivery {
	t.Helper()
	now := time.Now().UTC()
	notice := notificationPayload{EventID: eventID, Location: "C101", Severity: "high", OccurredAt: now, DetailPath: "/events/" + eventID.String()}
	payload, _ := json.Marshal(notice)
	delivery := Delivery{ID: uuid.New(), EventID: eventID, SubscriptionID: subscriptionID, AccountID: accountID, Payload: payload,
		Status: "pending", NextAttemptAt: now.Add(-time.Second), ExpiresAt: expires, CreatedAt: now, UpdatedAt: now}
	if err := db.Table(Delivery{}.TableName()).Create(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	return delivery
}

func mustJSONRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func makeSubscriptionStatus(app *App, accountID, sessionID uuid.UUID, endpoint string) int {
	_, x, y, _ := elliptic.GenerateKey(elliptic.P256(), bytes.NewReader(bytes.Repeat([]byte{3}, 96)))
	input := subscriptionInput{Endpoint: endpoint}
	input.Keys.P256dh = base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y))
	input.Keys.Auth = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 16))
	body, _ := json.Marshal(input)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/push/subscriptions", bytes.NewReader(body))
	request.Header.Set("X-Account-ID", accountID.String())
	request.Header.Set("X-Session-ID", sessionID.String())
	app.Handler().ServeHTTP(recorder, request)
	return recorder.Code
}
