package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type classroomRule struct {
	Enabled             *bool    `json:"enabled"`
	ConfidenceThreshold *float64 `json:"confidence_threshold,omitempty"`
	MinimumDurationMS   *int64   `json:"minimum_duration_ms,omitempty"`
	CooldownMS          *int64   `json:"cooldown_ms,omitempty"`
	Severity            *string  `json:"severity,omitempty"`
}

type parameterCapability struct {
	Type    string            `json:"type"`
	Minimum *float64          `json:"minimum,omitempty"`
	Maximum *float64          `json:"maximum,omitempty"`
	Enum    []json.RawMessage `json:"enum,omitempty"`
}

func validateParameters(assignment AlgorithmAssignment, raw json.RawMessage) error {
	var values map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil || values == nil {
		return errors.New("parameters must be a JSON object")
	}
	if len(values) == 0 {
		return errors.New("at least one supported parameter is required")
	}
	var capabilityDoc struct {
		Parameters map[string]parameterCapability `json:"parameters"`
	}
	_ = json.Unmarshal(assignment.Capabilities, &capabilityDoc)
	for key, value := range values {
		if assignment.AlgorithmKey == classroomAlgorithmKey && key == "classroom_rules" {
			if err := validateClassroomRules(value); err != nil {
				return err
			}
			continue
		}
		capability, ok := capabilityDoc.Parameters[key]
		if !ok {
			return errors.New("parameter is not declared as supported")
		}
		if err := validateCapabilityValue(value, capability); err != nil {
			return err
		}
	}
	if assignment.AlgorithmKey == classroomAlgorithmKey {
		if _, ok := values["classroom_rules"]; !ok {
			return errors.New("classroom_rules is required for classroom_behavior_detection")
		}
	}
	return nil
}

func validateClassroomRules(raw json.RawMessage) error {
	var rules map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rules); err != nil || rules == nil {
		return errors.New("classroom_rules must be an object")
	}
	if len(rules) != len(classroomBehaviorClasses) {
		return errors.New("all supported classroom behavior categories are required")
	}
	allowedSeverity := map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
	for key, value := range rules {
		if !containsString(classroomBehaviorClasses, key) {
			return errors.New("classroom behavior category is not supported")
		}
		var rule classroomRule
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&rule); err != nil || rule.Enabled == nil {
			return errors.New("each classroom rule must explicitly set enabled")
		}
		if rule.ConfidenceThreshold != nil && (math.IsNaN(*rule.ConfidenceThreshold) || math.IsInf(*rule.ConfidenceThreshold, 0) || *rule.ConfidenceThreshold < 0 || *rule.ConfidenceThreshold > 1) {
			return errors.New("confidence_threshold must be between 0 and 1")
		}
		if rule.MinimumDurationMS != nil && *rule.MinimumDurationMS <= 0 {
			return errors.New("minimum_duration_ms must be positive")
		}
		if rule.CooldownMS != nil && *rule.CooldownMS < 0 {
			return errors.New("cooldown_ms must not be negative")
		}
		if rule.Severity != nil && !allowedSeverity[*rule.Severity] {
			return errors.New("severity must be low, medium, high, or critical")
		}
		if *rule.Enabled && (rule.ConfidenceThreshold == nil || rule.MinimumDurationMS == nil || rule.CooldownMS == nil || rule.Severity == nil) {
			return errors.New("enabled classroom rules require confidence, duration, cooldown, and severity")
		}
	}
	for _, category := range classroomBehaviorClasses {
		if _, ok := rules[category]; !ok {
			return errors.New("all supported classroom behavior categories are required")
		}
	}
	return nil
}

func validateCapabilityValue(raw json.RawMessage, capability parameterCapability) error {
	if len(capability.Enum) > 0 {
		for _, allowed := range capability.Enum {
			if jsonValuesEqual(raw, allowed) {
				return nil
			}
		}
		return errors.New("parameter value is not in the supported enum")
	}
	switch capability.Type {
	case "number":
		var value float64
		if err := json.Unmarshal(raw, &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("parameter must be a finite number")
		}
		if capability.Minimum != nil && value < *capability.Minimum || capability.Maximum != nil && value > *capability.Maximum {
			return errors.New("parameter value is outside the supported range")
		}
	case "integer":
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return errors.New("parameter must be an integer")
		}
		if capability.Minimum != nil && float64(value) < *capability.Minimum || capability.Maximum != nil && float64(value) > *capability.Maximum {
			return errors.New("parameter value is outside the supported range")
		}
	case "boolean":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return errors.New("parameter must be a boolean")
		}
	case "string":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return errors.New("parameter must be a string")
		}
	default:
		return errors.New("parameter capability has an unsupported type")
	}
	return nil
}

func jsonValuesEqual(left, right json.RawMessage) bool {
	var l, r any
	if json.Unmarshal(left, &l) != nil || json.Unmarshal(right, &r) != nil {
		return false
	}
	return reflect.DeepEqual(l, r)
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (a *App) updateAlgorithmParameters(c *gin.Context) {
	device, ok := a.findOwnedDevice(c, c.Param("device_id"), true)
	if !ok {
		return
	}
	assignmentID, err := uuid.Parse(c.Param("assignment_id"))
	if err != nil {
		writeError(c, http.StatusNotFound, "not_found", "algorithm assignment was not found")
		return
	}
	var request struct {
		ExpectedVersion *int64          `json:"expected_version"`
		Parameters      json.RawMessage `json:"parameters"`
	}
	if decodeJSON(c, &request) != nil || request.ExpectedVersion == nil || *request.ExpectedVersion < 0 || len(request.Parameters) == 0 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "expected_version and parameters are required")
		return
	}
	now := time.Now().UTC()
	var command CommandRecord
	var desiredVersion int64
	err = a.db.Transaction(func(tx *gorm.DB) error {
		var assignment AlgorithmAssignment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND account_id = ? AND device_id = ?", assignmentID, accountID(c), device.ID).First(&assignment).Error; err != nil {
			return err
		}
		if assignment.DesiredVersion != *request.ExpectedVersion {
			return errVersionConflict
		}
		if err := validateParameters(assignment, request.Parameters); err != nil {
			return &invalidParametersError{reason: err.Error()}
		}
		desiredVersion = assignment.DesiredVersion + 1
		command = CommandRecord{
			ID: uuid.New(), WorkspaceID: assignment.WorkspaceID, AssignmentID: assignment.ID, DeviceID: device.ID, ExpectedVersion: desiredVersion,
			Parameters: append(json.RawMessage(nil), request.Parameters...), Status: "pending", CreatedAt: now,
		}
		snapshot := ConfigSnapshot{ID: uuid.New(), WorkspaceID: assignment.WorkspaceID, AssignmentID: assignment.ID, DeviceID: device.ID, Version: desiredVersion, AlgorithmVersion: assignment.AlgorithmVersion, Parameters: append(json.RawMessage(nil), request.Parameters...), CreatedAt: now}
		if err := tx.Create(&snapshot).Error; err != nil {
			return err
		}
		if err := tx.Create(&command).Error; err != nil {
			return err
		}
		return tx.Model(&AlgorithmAssignment{}).Where("id = ?", assignment.ID).Updates(map[string]any{
			"desired_parameters": request.Parameters, "desired_version": desiredVersion, "config_state": "pending", "updated_at": now,
		}).Error
	})
	if err != nil {
		if errors.Is(err, errVersionConflict) {
			writeError(c, http.StatusConflict, "version_conflict", "algorithm configuration changed; reload before retrying")
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "algorithm assignment was not found")
			return
		}
		var validationErr *invalidParametersError
		if errors.As(err, &validationErr) {
			writeError(c, http.StatusUnprocessableEntity, "unsupported_parameters", validationErr.reason)
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "algorithm configuration could not be saved")
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"command_id": command.ID, "desired_version": desiredVersion, "status": "pending"})
}

var errVersionConflict = errors.New("configuration version conflict")

type invalidParametersError struct{ reason string }

func (e *invalidParametersError) Error() string {
	return fmt.Sprintf("invalid parameters: %s", e.reason)
}
