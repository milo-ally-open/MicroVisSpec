package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var classroomEventTypes = map[string]string{
	"classroom.behavior.hand-raising": "hand-raising",
	"classroom.behavior.reading":      "reading",
	"classroom.behavior.writing":      "writing",
}

var validSeverities = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}

type EdgeEvent struct {
	SchemaVersion    int             `json:"schema_version"`
	DeviceID         uuid.UUID       `json:"device_id"`
	SentAt           time.Time       `json:"sent_at"`
	EventID          uuid.UUID       `json:"event_id"`
	CameraID         uuid.UUID       `json:"camera_id"`
	EventType        string          `json:"event_type"`
	Severity         string          `json:"severity"`
	OccurredAt       time.Time       `json:"occurred_at"`
	AlgorithmKey     string          `json:"algorithm_key"`
	AlgorithmVersion string          `json:"algorithm_version"`
	RegionKey        string          `json:"region_key,omitempty"`
	Attributes       json.RawMessage `json:"attributes"`
}

type ClassroomAttributes struct {
	BehaviorClass      string  `json:"behavior_class"`
	Confidence         float64 `json:"confidence"`
	ObservedDurationMS int64   `json:"observed_duration_ms"`
	RuleConfigVersion  int64   `json:"rule_config_version"`
}

type EventApplicationAck struct {
	SchemaVersion int       `json:"schema_version"`
	DeviceID      uuid.UUID `json:"device_id"`
	EventID       uuid.UUID `json:"event_id"`
	Result        string    `json:"result"`
	ReasonCode    string    `json:"reason_code,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
}

type persistedEventResult struct {
	EventID   uuid.UUID
	Duplicate bool
}

func (a *App) handleMQTTMessage(topic string, payload []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ack, err := a.processEdgeEvent(ctx, topic, payload)
	if err != nil {
		slog.Error("event_ingest_temporarily_failed", "topic", topic, "error", err.Error())
		return
	}
	if ack == nil {
		return
	}
	ackBytes, err := json.Marshal(ack)
	if err != nil {
		slog.Error("event_ack_encode_failed", "event_id", ack.EventID, "error", err.Error())
		return
	}
	if err := a.messages.Publish(ctx, "devices/"+ack.DeviceID.String()+"/event-acks", ackBytes); err != nil {
		slog.Error("event_ack_publish_failed", "event_id", ack.EventID, "device_id", ack.DeviceID, "result", ack.Result, "error", err.Error())
		return
	}
	slog.Info("event_application_ack_published", "event_id", ack.EventID, "device_id", ack.DeviceID, "result", ack.Result, "reason_code", ack.ReasonCode)
}

func (a *App) processEdgeEvent(ctx context.Context, topic string, payload []byte) (*EventApplicationAck, error) {
	topicDeviceID, err := parseEventTopic(topic)
	if err != nil {
		return nil, nil
	}
	if len(payload) == 0 || len(payload) > 64<<10 {
		return nil, nil
	}
	var peek struct {
		EventID uuid.UUID `json:"event_id"`
	}
	if err := json.Unmarshal(payload, &peek); err != nil || peek.EventID == uuid.Nil {
		return nil, nil
	}
	ack := &EventApplicationAck{SchemaVersion: 1, DeviceID: topicDeviceID, EventID: peek.EventID, OccurredAt: time.Now().UTC()}
	reject := func(reason string) (*EventApplicationAck, error) {
		ack.Result, ack.ReasonCode = "rejected", reason
		return ack, nil
	}

	var event EdgeEvent
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return reject("invalid_schema")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return reject("invalid_schema")
	}
	if event.EventID == uuid.Nil {
		return nil, nil
	}
	if event.DeviceID != topicDeviceID {
		return reject("device_id_mismatch")
	}
	if reason := validateEdgeEvent(event); reason != "" {
		return reject(reason)
	}
	var attributes ClassroomAttributes
	if err := decodeClassroomAttributes(event.Attributes, &attributes); err != nil {
		return reject("invalid_classroom_attributes")
	}
	if attributes.BehaviorClass != classroomEventTypes[event.EventType] {
		return reject("behavior_class_mismatch")
	}
	validation, reason, err := a.validateClassroomEvent(ctx, event, attributes)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return reject(reason)
	}
	if !validation.Allowed {
		return reject("rule_rejected")
	}
	result, err := a.persistEvent(ctx, event, attributes, validation)
	if err != nil {
		return nil, fmt.Errorf("persist edge event: %w", err)
	}
	if result.Duplicate {
		ack.Result = "duplicate"
		return ack, nil
	}
	ack.Result = "accepted"
	return ack, nil
}

func parseEventTopic(topic string) (uuid.UUID, error) {
	parts := strings.Split(topic, "/")
	if len(parts) != 3 || parts[0] != "devices" || parts[2] != "events" {
		return uuid.Nil, errors.New("unexpected event topic")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid device id in event topic")
	}
	return id, nil
}

func validateEdgeEvent(event EdgeEvent) string {
	if event.SchemaVersion != 1 {
		return "unsupported_schema_version"
	}
	if event.DeviceID == uuid.Nil || event.CameraID == uuid.Nil {
		return "invalid_device_or_camera_id"
	}
	if event.EventID == uuid.Nil {
		return "invalid_event_id"
	}
	if _, ok := classroomEventTypes[event.EventType]; !ok {
		return "unsupported_event_type"
	}
	if !validSeverities[event.Severity] {
		return "invalid_severity"
	}
	if event.SentAt.IsZero() || event.OccurredAt.IsZero() {
		return "invalid_event_time"
	}
	if event.OccurredAt.After(time.Now().UTC().Add(5 * time.Minute)) {
		return "invalid_occurred_at"
	}
	if strings.TrimSpace(event.AlgorithmKey) != "classroom_behavior_detection" || strings.TrimSpace(event.AlgorithmVersion) == "" || len(event.AlgorithmVersion) > 120 {
		return "invalid_algorithm"
	}
	if len(event.RegionKey) > 64 {
		return "invalid_region_key"
	}
	if len(event.Attributes) == 0 || string(event.Attributes) == "null" {
		return "invalid_classroom_attributes"
	}
	return ""
}

func decodeClassroomAttributes(raw json.RawMessage, attributes *ClassroomAttributes) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("attributes must be an object")
	}
	allowed := map[string]bool{"behavior_class": true, "confidence": true, "observed_duration_ms": true, "rule_config_version": true}
	for key := range fields {
		if !allowed[key] {
			return fmt.Errorf("unsupported attribute %q", key)
		}
	}
	for key := range allowed {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("required attribute %q is missing", key)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(attributes); err != nil {
		return err
	}
	if !containsString([]string{"hand-raising", "reading", "writing"}, attributes.BehaviorClass) || math.IsNaN(attributes.Confidence) || math.IsInf(attributes.Confidence, 0) || attributes.Confidence < 0 || attributes.Confidence > 1 || attributes.ObservedDurationMS <= 0 || attributes.RuleConfigVersion < 1 {
		return errors.New("classroom attributes are outside supported bounds")
	}
	return nil
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func (a *App) persistEvent(ctx context.Context, message EdgeEvent, attributes ClassroomAttributes, owner ownershipResult) (persistedEventResult, error) {
	now := time.Now().UTC()
	if skew := now.Sub(message.SentAt.UTC()); skew > 5*time.Minute || skew < -5*time.Minute {
		slog.Warn("event_device_clock_skew", "edge_event_id", message.EventID, "skew_seconds", int64(skew.Seconds()))
	}
	internalID := uuid.New()
	workspaceID := owner.AccountID
	entity := Event{ID: internalID, AccountID: owner.AccountID, WorkspaceID: &workspaceID, DeviceID: message.DeviceID, CameraID: message.CameraID,
		EdgeEventID: message.EventID, EventType: message.EventType, Severity: message.Severity,
		OccurredAt: message.OccurredAt.UTC(), ReceivedAt: now, DeviceName: owner.DeviceName, CameraName: owner.CameraName,
		Location: owner.Location, AlgorithmKey: message.AlgorithmKey, AlgorithmVersion: message.AlgorithmVersion,
		RuleConfigVersion: attributes.RuleConfigVersion, RegionKey: message.RegionKey, Attributes: append(json.RawMessage(nil), message.Attributes...),
		Status: "pending", Version: 1, CreatedAt: now}
	result := persistedEventResult{EventID: internalID}
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created := tx.Table(Event{}.TableName()).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "device_id"}, {Name: "edge_event_id"}}, DoNothing: true,
		}).Create(&entity)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			var existing Event
			if err := tx.Table(Event{}.TableName()).Where("device_id = ? AND edge_event_id = ?", message.DeviceID, message.EventID).Take(&existing).Error; err != nil {
				return err
			}
			result = persistedEventResult{EventID: existing.ID, Duplicate: true}
			return nil
		}
		outboxPayload, err := json.Marshal(map[string]any{
			"account_id": entity.AccountID, "event_id": entity.ID, "device_id": entity.DeviceID, "location": entity.Location,
			"severity": entity.Severity, "occurred_at": entity.OccurredAt.Format(time.RFC3339Nano),
			"detail_path": "/events/" + entity.ID.String(),
		})
		if err != nil {
			return err
		}
		outbox := Outbox{ID: entity.ID, WorkspaceID: &workspaceID, EventType: "event.created", Payload: outboxPayload, CreatedAt: now, UpdatedAt: now, NextAttemptAt: now}
		if err := tx.Table(Outbox{}.TableName()).Create(&outbox).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return persistedEventResult{}, err
	}
	return result, nil
}
