package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const classroomAlgorithmKey = "classroom_behavior_detection"

var classroomBehaviorClasses = []string{"hand-raising", "reading", "writing"}

type cameraStateReport struct {
	CameraKey   string `json:"camera_key"`
	CameraState string `json:"camera_state"`
	StreamState string `json:"stream_state"`
}

type algorithmStateReport struct {
	AlgorithmKey       string          `json:"algorithm_key"`
	AlgorithmVersion   string          `json:"algorithm_version"`
	RuntimeState       string          `json:"runtime_state"`
	Capabilities       json.RawMessage `json:"capabilities"`
	ReportedParameters json.RawMessage `json:"reported_parameters"`
	ReportedVersion    int64           `json:"reported_config_version"`
}

type stateReport struct {
	SchemaVersion int                    `json:"schema_version"`
	DeviceID      string                 `json:"device_id"`
	SentAt        string                 `json:"sent_at"`
	Cameras       []cameraStateReport    `json:"cameras"`
	Algorithms    []algorithmStateReport `json:"algorithms"`
}

func (a *App) handleMQTTMessage(topic string, payload []byte) {
	var err error
	switch {
	case strings.HasSuffix(topic, "/state"):
		err = a.processDeviceState(topic, payload)
	case strings.HasSuffix(topic, "/command-acks"):
		err = a.processCommandAck(topic, payload)
	default:
		err = errors.New("unhandled MQTT topic")
	}
	if err != nil {
		slog.Warn("device_mqtt_message_rejected", "topic", topic, "error", err.Error())
	}
}

func (a *App) processDeviceState(topic string, payload []byte) error {
	deviceID, err := parseDeviceTopic(topic, "state")
	if err != nil {
		return err
	}
	var report stateReport
	if err := mqttJSON(payload, &report); err != nil {
		return fmt.Errorf("invalid state payload: %w", err)
	}
	if report.SchemaVersion != 1 || report.DeviceID != deviceID.String() {
		return errors.New("state schema version or device identity does not match topic")
	}
	sentAt, err := time.Parse(time.RFC3339Nano, report.SentAt)
	if err != nil {
		return errors.New("state sent_at is invalid")
	}
	now := time.Now().UTC()
	if sentAt.After(now.Add(5*time.Minute)) || now.Sub(sentAt) > heartbeatTimeout {
		return errors.New("stale or future device state report")
	}
	for _, camera := range report.Cameras {
		if strings.TrimSpace(camera.CameraKey) == "" || len(camera.CameraKey) > 120 || len(camera.CameraState) > 32 || len(camera.StreamState) > 32 {
			return errors.New("invalid camera state report")
		}
	}
	for _, algorithm := range report.Algorithms {
		if strings.TrimSpace(algorithm.AlgorithmKey) == "" || len(algorithm.AlgorithmKey) > 120 || strings.TrimSpace(algorithm.AlgorithmVersion) == "" || len(algorithm.AlgorithmVersion) > 120 || len(algorithm.RuntimeState) > 32 || algorithm.ReportedVersion < 0 {
			return errors.New("invalid algorithm state report")
		}
		if !isJSONObject(algorithm.Capabilities) || !isJSONObject(algorithm.ReportedParameters) {
			return errors.New("algorithm capabilities and reported parameters must be JSON objects")
		}
	}
	stateJSON := append(json.RawMessage(nil), payload...)
	return a.db.Transaction(func(tx *gorm.DB) error {
		var device Device
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND active = ? AND activation_state = ?", deviceID, true, "active").First(&device).Error; err != nil {
			return errors.New("state report is not from an active device")
		}
		if device.ReportedAt != nil && sentAt.Before(*device.ReportedAt) {
			return nil // A queued older status must not overwrite a newer report.
		}
		if err := tx.Model(&Device{}).Where("id = ? AND active = ?", device.ID, true).Updates(map[string]any{
			"last_seen_at": now, "reported_at": sentAt, "reported_state": stateJSON, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		for _, camera := range report.Cameras {
			if err := tx.Model(&Camera{}).Where("account_id = ? AND device_id = ? AND camera_key = ? AND active = ?", device.AccountID, device.ID, camera.CameraKey, true).
				Updates(map[string]any{"camera_state": camera.CameraState, "stream_state": camera.StreamState, "last_reported_at": sentAt, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		for _, algorithm := range report.Algorithms {
			if err := upsertAlgorithmReport(tx, device, algorithm, sentAt, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func upsertAlgorithmReport(tx *gorm.DB, device Device, report algorithmStateReport, sentAt, now time.Time) error {
	var assignment AlgorithmAssignment
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id = ? AND algorithm_key = ?", device.ID, report.AlgorithmKey).First(&assignment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		parameters := json.RawMessage(`{}`)
		if report.AlgorithmKey == classroomAlgorithmKey {
			parameters = defaultClassroomParameters()
		}
		assignment = AlgorithmAssignment{
			ID: uuid.New(), AccountID: device.AccountID, DeviceID: device.ID, AlgorithmKey: report.AlgorithmKey,
			AlgorithmVersion: report.AlgorithmVersion, RuntimeState: report.RuntimeState, ConfigState: "pending",
			Capabilities: normalizedObject(report.Capabilities), ReportedParameters: normalizedObject(report.ReportedParameters),
			DesiredParameters: parameters, DesiredVersion: 0, ReportedVersion: report.ReportedVersion, ReportedAt: &sentAt,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&assignment).Error; err != nil {
			return err
		}
		if report.AlgorithmKey == classroomAlgorithmKey {
			snapshot := ConfigSnapshot{ID: uuid.New(), AssignmentID: assignment.ID, DeviceID: device.ID, Version: 0, AlgorithmVersion: report.AlgorithmVersion, Parameters: parameters, CreatedAt: now}
			if err := tx.Create(&snapshot).Error; err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	} else {
		if err := tx.Model(&assignment).Updates(map[string]any{
			"algorithm_version": report.AlgorithmVersion, "runtime_state": report.RuntimeState,
			"capabilities": normalizedObject(report.Capabilities), "reported_parameters": normalizedObject(report.ReportedParameters),
			"reported_version": report.ReportedVersion, "reported_at": sentAt, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		assignment.AlgorithmVersion = report.AlgorithmVersion
		assignment.ReportedParameters = normalizedObject(report.ReportedParameters)
		assignment.ReportedVersion = report.ReportedVersion
	}
	var snapshot ConfigSnapshot
	if err := tx.Where("assignment_id = ? AND version = ?", assignment.ID, report.ReportedVersion).First(&snapshot).Error; err == nil {
		if snapshot.AppliedAt == nil && jsonEqual(snapshot.Parameters, report.ReportedParameters) {
			if err := tx.Model(&ConfigSnapshot{}).Where("id = ? AND applied_at IS NULL", snapshot.ID).Update("applied_at", sentAt).Error; err != nil {
				return err
			}
			if report.ReportedVersion == assignment.DesiredVersion {
				if err := tx.Model(&AlgorithmAssignment{}).Where("id = ?", assignment.ID).Update("config_state", "applied").Error; err != nil {
					return err
				}
			}
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

func (a *App) processCommandAck(topic string, payload []byte) error {
	deviceID, err := parseDeviceTopic(topic, "command-acks")
	if err != nil {
		return err
	}
	var ack struct {
		SchemaVersion   int    `json:"schema_version"`
		DeviceID        string `json:"device_id"`
		CommandID       string `json:"command_id"`
		SentAt          string `json:"sent_at"`
		Status          string `json:"status"`
		ReportedVersion int64  `json:"reported_config_version"`
		FailureReason   string `json:"failure_reason"`
	}
	if err := mqttJSON(payload, &ack); err != nil {
		return err
	}
	if ack.SchemaVersion != 1 || ack.DeviceID != deviceID.String() || ack.Status != "applied" && ack.Status != "rejected" || len(ack.FailureReason) > 500 {
		return errors.New("command acknowledgement is invalid")
	}
	commandID, err := uuid.Parse(ack.CommandID)
	if err != nil {
		return errors.New("command acknowledgement id is invalid")
	}
	ackAt, err := time.Parse(time.RFC3339Nano, ack.SentAt)
	if err != nil || ackAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return errors.New("command acknowledgement sent_at is invalid")
	}
	return a.db.Transaction(func(tx *gorm.DB) error {
		var command CommandRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND device_id = ?", commandID, deviceID).First(&command).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // Unknown/late QoS-1 acknowledgements are harmless.
			}
			return err
		}
		if command.Status == "applied" || command.Status == "failed" {
			return nil
		}
		var assignment AlgorithmAssignment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND device_id = ?", command.AssignmentID, deviceID).First(&assignment).Error; err != nil {
			return err
		}
		if ack.Status != "applied" {
			if ack.FailureReason == "" {
				ack.FailureReason = "device_rejected"
			}
			if err := tx.Model(&CommandRecord{}).Where("id = ?", command.ID).Updates(map[string]any{"status": "failed", "failure_reason": ack.FailureReason, "reported_version": ack.ReportedVersion, "acknowledged_at": ackAt}).Error; err != nil {
				return err
			}
			if assignment.DesiredVersion == command.ExpectedVersion {
				return tx.Model(&AlgorithmAssignment{}).Where("id = ?", assignment.ID).Update("config_state", "failed").Error
			}
			return nil
		}
		if ack.ReportedVersion != command.ExpectedVersion || assignment.DesiredVersion != command.ExpectedVersion {
			if err := tx.Model(&CommandRecord{}).Where("id = ?", command.ID).Updates(map[string]any{"status": "failed", "failure_reason": "reported_version_mismatch", "reported_version": ack.ReportedVersion, "acknowledged_at": ackAt}).Error; err != nil {
				return err
			}
			if assignment.DesiredVersion == command.ExpectedVersion {
				return tx.Model(&AlgorithmAssignment{}).Where("id = ?", assignment.ID).Update("config_state", "failed").Error
			}
			return nil
		}
		if err := tx.Model(&CommandRecord{}).Where("id = ?", command.ID).Updates(map[string]any{"status": "applied", "failure_reason": "", "reported_version": ack.ReportedVersion, "acknowledged_at": ackAt}).Error; err != nil {
			return err
		}
		if err := tx.Model(&AlgorithmAssignment{}).Where("id = ?", assignment.ID).Updates(map[string]any{
			"config_state": "applied", "reported_version": ack.ReportedVersion, "reported_parameters": command.Parameters, "reported_at": ackAt,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&ConfigSnapshot{}).Where("assignment_id = ? AND version = ? AND applied_at IS NULL", command.AssignmentID, command.ExpectedVersion).Update("applied_at", ackAt).Error
	})
}

func isJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var value map[string]json.RawMessage
	return json.Unmarshal(raw, &value) == nil && value != nil
}

func normalizedObject(raw json.RawMessage) json.RawMessage {
	if !isJSONObject(raw) {
		return json.RawMessage(`{}`)
	}
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func jsonEqual(left, right json.RawMessage) bool {
	if !isJSONObject(left) || !isJSONObject(right) {
		return false
	}
	var l, r any
	return json.Unmarshal(left, &l) == nil && json.Unmarshal(right, &r) == nil && reflect.DeepEqual(l, r)
}

func defaultClassroomParameters() json.RawMessage {
	parameters := map[string]any{"classroom_rules": map[string]any{}}
	for _, behavior := range classroomBehaviorClasses {
		parameters["classroom_rules"].(map[string]any)[behavior] = map[string]any{
			"enabled": false, "confidence_threshold": 0.0, "minimum_duration_ms": 1, "cooldown_ms": 0, "severity": "low",
		}
	}
	encoded, _ := json.Marshal(parameters)
	return encoded
}

func (a *App) pendingCommandLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.publishPendingCommands(ctx)
		}
	}
}

func (a *App) publishPendingCommands(ctx context.Context) {
	if a.messages == nil || a.messages.Check(ctx) != nil {
		return
	}
	var commands []CommandRecord
	cutoff := time.Now().UTC().Add(-5 * time.Second)
	if err := a.db.Where("status = ? AND (last_attempt_at IS NULL OR last_attempt_at < ?)", "pending", cutoff).Order("created_at ASC").Limit(50).Find(&commands).Error; err != nil {
		slog.Error("device_command_queue_read_failed", "error", err.Error())
		return
	}
	for _, command := range commands {
		var assignment AlgorithmAssignment
		if err := a.db.Where("id = ?", command.AssignmentID).First(&assignment).Error; err != nil {
			continue
		}
		sentAt := time.Now().UTC()
		payload, err := json.Marshal(map[string]any{
			"schema_version": 1, "device_id": command.DeviceID.String(), "command_id": command.ID.String(),
			"sent_at": sentAt, "algorithm_key": assignment.AlgorithmKey, "algorithm_version": assignment.AlgorithmVersion,
			"expected_version": command.ExpectedVersion, "parameters": command.Parameters,
		})
		if err == nil {
			publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = a.messages.Publish(publishCtx, "devices/"+command.DeviceID.String()+"/commands", payload)
			cancel()
		}
		updates := map[string]any{"attempts": gorm.Expr("attempts + 1"), "last_attempt_at": sentAt}
		if err != nil {
			updates["failure_reason"] = safeReason(err.Error(), 500)
			slog.Warn("device_command_publish_failed", "command_id", command.ID, "device_id", command.DeviceID, "error", err.Error())
		} else {
			updates["failure_reason"] = ""
		}
		_ = a.db.Model(&CommandRecord{}).Where("id = ? AND status = ?", command.ID, "pending").Updates(updates).Error
	}
}

func safeReason(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}
