package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClassroomMessageValidationRejectsIdentityFields(t *testing.T) {
	deviceID := uuid.New()
	message := EdgeEvent{
		SchemaVersion: 1, DeviceID: deviceID, EventID: uuid.New(), CameraID: uuid.New(),
		EventType: "classroom.behavior.hand-raising", Severity: "medium", SentAt: time.Now().UTC(), OccurredAt: time.Now().UTC(),
		AlgorithmKey: "classroom_behavior_detection", AlgorithmVersion: "1.0.0",
		Attributes: json.RawMessage(`{"behavior_class":"hand-raising","confidence":0.92,"observed_duration_ms":1200,"rule_config_version":3,"student_id":"private"}`),
	}
	if reason := validateEdgeEvent(message); reason != "" {
		t.Fatalf("valid event header rejected: %s", reason)
	}
	var attributes ClassroomAttributes
	if err := decodeClassroomAttributes(message.Attributes, &attributes); err == nil {
		t.Fatal("personal identity fields must not enter classroom event attributes")
	}
}

func TestEventTypesRequireMatchingClassroomBehavior(t *testing.T) {
	for eventType, expected := range classroomEventTypes {
		if expected == "" || !validSeverities["critical"] {
			t.Fatalf("invalid event mapping: %s -> %q", eventType, expected)
		}
	}
	if _, ok := classroomEventTypes["classroom.behavior.student-tracking"]; ok {
		t.Fatal("unsupported classroom behavior event type accepted")
	}
}
