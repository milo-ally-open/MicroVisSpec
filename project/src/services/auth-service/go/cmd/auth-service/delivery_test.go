package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthEnvelopeKeepsStableIdentityAndMinimalPayload(t *testing.T) {
	accountID, sessionID, eventID := uuid.New(), uuid.New(), uuid.New()
	occurredAt := time.Date(2026, 10, 19, 8, 30, 0, 0, time.UTC)
	item := Outbox{ID: eventID, EventType: "session.revoked", Payload: []byte(`{"account_id":"` + accountID.String() + `","session_id":"` + sessionID.String() + `","occurred_at":"` + occurredAt.Format(time.RFC3339Nano) + `","secret":"must-not-be-forwarded"}`)}

	envelope, err := authEnvelope(item)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Producer != "auth-service" || envelope.EventID != eventID || envelope.SchemaVersion != 1 || !envelope.OccurredAt.Equal(occurredAt) {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	var payload map[string]any
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 || payload["account_id"] != accountID.String() || payload["session_id"] != sessionID.String() {
		t.Fatalf("unexpected revocation payload: %s", envelope.Payload)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "must-not-be-forwarded") {
		t.Fatalf("unexpected field leaked in envelope: %s", encoded)
	}
}
