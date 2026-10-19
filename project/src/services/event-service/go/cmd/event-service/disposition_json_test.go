package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDispositionJSONUsesSnakeCaseContract(t *testing.T) {
	disposition := Disposition{
		ID: uuid.New(), EventID: uuid.New(), ActorAccountID: uuid.New(),
		FromStatus: "pending", ToStatus: "processing", Result: "review started",
		ActedAt: time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC),
	}
	payload, err := json.Marshal(disposition)
	if err != nil {
		t.Fatalf("marshal disposition: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("unmarshal disposition: %v", err)
	}
	for _, name := range []string{"disposition_id", "event_id", "actor_account_id", "from_status", "to_status", "result", "acted_at"} {
		if _, ok := fields[name]; !ok {
			t.Errorf("snake_case field %q missing from %s", name, payload)
		}
	}
	if _, ok := fields["ID"]; ok {
		t.Errorf("Go field name leaked into response: %s", payload)
	}
}
