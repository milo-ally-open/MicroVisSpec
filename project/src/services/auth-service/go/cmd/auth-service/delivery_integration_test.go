//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthOutboxRetriesBothRevocationConsumersWithStableEventID(t *testing.T) {
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
	if err := db.Table(Outbox{}.TableName()).Where("delivered_at IS NULL").Delete(&Outbox{}).Error; err != nil {
		t.Fatalf("clear pending records from isolated auth test database: %v", err)
	}

	var mediaCalls, pushCalls atomic.Int32
	var mu sync.Mutex
	var eventIDs []uuid.UUID
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/inbox" || r.Header.Get("X-Internal-Service") != "auth-service" {
			t.Errorf("unexpected request: %s %s service=%q", r.Method, r.URL.Path, r.Header.Get("X-Internal-Service"))
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var envelope authInboxEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode envelope: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		eventIDs = append(eventIDs, envelope.EventID)
		mu.Unlock()
		if envelope.EventType != "session.revoked" || envelope.Producer != "auth-service" || envelope.SchemaVersion != 1 || envelope.EventID == uuid.Nil {
			t.Errorf("invalid revocation envelope: %+v", envelope)
			http.Error(w, "bad envelope", http.StatusBadRequest)
			return
		}
		switch r.Header.Get("X-Internal-Service-Token") {
		case "media-token":
			mediaCalls.Add(1)
		case "push-token":
			if pushCalls.Add(1) == 1 {
				http.Error(w, "temporary failure", http.StatusServiceUnavailable)
				return
			}
		default:
			t.Errorf("unexpected service token")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	eventID, accountID, sessionID := uuid.New(), uuid.New(), uuid.New()
	createdAt := time.Now().UTC().Add(-time.Second)
	payload, err := json.Marshal(map[string]string{
		"account_id": accountID.String(), "session_id": sessionID.String(), "occurred_at": createdAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	item := Outbox{ID: eventID, EventType: "session.revoked", Payload: payload, CreatedAt: createdAt, NextAttemptAt: createdAt}
	if err := db.Table(Outbox{}.TableName()).Create(&item).Error; err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
	t.Cleanup(func() { _ = db.Table(Outbox{}.TableName()).Where("id = ?", eventID).Delete(&Outbox{}).Error })

	config := Config{MediaServiceURL: server.URL, MediaServiceToken: "media-token", PushServiceURL: server.URL, PushServiceToken: "push-token"}
	ctx := context.Background()
	client := server.Client()
	claimed, err := claimAuthOutbox(ctx, db)
	if err != nil || claimed == nil || claimed.ID != eventID {
		t.Fatalf("claim outbox: item=%v err=%v", claimed, err)
	}
	deliveryErr := deliverAuthOutbox(ctx, *claimed, config, client)
	if deliveryErr == nil {
		t.Fatal("temporary push failure should keep the outbox pending")
	}
	if err := retryAuthOutbox(ctx, db, *claimed, deliveryErr); err != nil {
		t.Fatalf("schedule retry: %v", err)
	}
	if err := db.Table(Outbox{}.TableName()).Where("id = ?", eventID).Update("next_attempt_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatalf("make retry due: %v", err)
	}
	claimed, err = claimAuthOutbox(ctx, db)
	if err != nil || claimed == nil || claimed.ID != eventID || claimed.Attempts != 2 {
		t.Fatalf("retry did not reclaim same event: item=%+v err=%v", claimed, err)
	}
	if err := deliverAuthOutbox(ctx, *claimed, config, client); err != nil {
		t.Fatalf("deliver retry: %v", err)
	}
	if err := acknowledgeAuthOutbox(ctx, db, *claimed); err != nil {
		t.Fatalf("acknowledge outbox: %v", err)
	}

	var stored Outbox
	if err := db.Table(Outbox{}.TableName()).Where("id = ?", eventID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.DeliveredAt == nil || mediaCalls.Load() != 2 || pushCalls.Load() != 2 {
		t.Fatalf("outbox not delivered to both consumers after retry: row=%+v media=%d push=%d", stored, mediaCalls.Load(), pushCalls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(eventIDs) != 4 || eventIDs[0] != eventID || eventIDs[1] != eventID || eventIDs[2] != eventID || eventIDs[3] != eventID {
		t.Fatalf("retries changed event id: %v", eventIDs)
	}
}
