package main

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (a *App) validateClassroomEvent(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("device_id"))
	if err != nil || deviceID == uuid.Nil {
		writeError(c, http.StatusNotFound, "not_found", "device was not found")
		return
	}
	var input struct {
		CameraID           string   `json:"camera_id"`
		AlgorithmVersion   string   `json:"algorithm_version"`
		RuleConfigVersion  *int64   `json:"rule_config_version"`
		BehaviorClass      string   `json:"behavior_class"`
		Confidence         *float64 `json:"confidence"`
		ObservedDurationMS *int64   `json:"observed_duration_ms"`
		Severity           string   `json:"severity"`
		OccurredAt         string   `json:"occurred_at"`
	}
	if decodeJSON(c, &input) != nil || strings.TrimSpace(input.AlgorithmVersion) == "" || input.RuleConfigVersion == nil || *input.RuleConfigVersion < 0 || input.ObservedDurationMS == nil || *input.ObservedDurationMS <= 0 || input.Confidence == nil || math.IsNaN(*input.Confidence) || math.IsInf(*input.Confidence, 0) || *input.Confidence < 0 || *input.Confidence > 1 || strings.TrimSpace(input.Severity) == "" {
		writeRuleRejection(c, "invalid_event_metadata")
		return
	}
	cameraID, err := uuid.Parse(input.CameraID)
	if err != nil || cameraID == uuid.Nil {
		writeRuleRejection(c, "camera_not_found")
		return
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, input.OccurredAt)
	if err != nil || occurredAt.After(time.Now().UTC().Add(5*time.Minute)) {
		writeRuleRejection(c, "invalid_occurred_at")
		return
	}
	if !containsString(classroomBehaviorClasses, input.BehaviorClass) {
		writeRuleRejection(c, "unsupported_behavior_class")
		return
	}
	var device Device
	if err := a.db.Where("id = ? AND active = ? AND activation_state = ?", deviceID, true, "active").First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "device was not found")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device ownership could not be verified")
		}
		return
	}
	var camera Camera
	if err := a.db.Where("id = ? AND device_id = ? AND account_id = ? AND active = ?", cameraID, device.ID, device.AccountID, true).First(&camera).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "camera was not found")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "camera ownership could not be verified")
		}
		return
	}
	var assignment AlgorithmAssignment
	if err := a.db.Where("device_id = ? AND algorithm_key = ?", device.ID, classroomAlgorithmKey).First(&assignment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeRuleRejection(c, "algorithm_not_reported")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "algorithm configuration could not be loaded")
		}
		return
	}
	if assignment.AlgorithmVersion != input.AlgorithmVersion {
		writeRuleRejection(c, "algorithm_version_mismatch")
		return
	}
	var snapshot ConfigSnapshot
	if err := a.db.Where("assignment_id = ? AND version = ?", assignment.ID, *input.RuleConfigVersion).First(&snapshot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeRuleRejection(c, "unknown_rule_config_version")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "historical rule configuration is unavailable")
		}
		return
	}
	if snapshot.AppliedAt == nil {
		writeRuleRejection(c, "rule_config_not_applied")
		return
	}
	if occurredAt.Before(*snapshot.AppliedAt) {
		writeRuleRejection(c, "rule_config_not_active_at_event_time")
		return
	}
	var laterSnapshot ConfigSnapshot
	err = a.db.Where("assignment_id = ? AND version > ? AND applied_at IS NOT NULL AND applied_at <= ?", assignment.ID, snapshot.Version, occurredAt).
		Order("version DESC").First(&laterSnapshot).Error
	if err == nil {
		writeRuleRejection(c, "rule_config_not_active_at_event_time")
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "rule configuration history is unavailable")
		return
	}
	var parameters struct {
		ClassroomRules map[string]classroomRule `json:"classroom_rules"`
	}
	if err := json.Unmarshal(snapshot.Parameters, &parameters); err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "historical rule configuration is invalid")
		return
	}
	rule, exists := parameters.ClassroomRules[input.BehaviorClass]
	if !exists || rule.Enabled == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "historical rule configuration is incomplete")
		return
	}
	if !*rule.Enabled {
		writeRuleRejection(c, "rule_disabled")
		return
	}
	if rule.ConfidenceThreshold == nil || rule.MinimumDurationMS == nil || rule.CooldownMS == nil || rule.Severity == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "historical rule configuration is incomplete")
		return
	}
	if *input.Confidence < *rule.ConfidenceThreshold {
		writeRuleRejection(c, "confidence_below_threshold")
		return
	}
	if *input.ObservedDurationMS < *rule.MinimumDurationMS {
		writeRuleRejection(c, "duration_below_minimum")
		return
	}
	if input.Severity != *rule.Severity {
		writeRuleRejection(c, "severity_mismatch")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"allowed": true, "account_id": device.AccountID, "device_id": device.ID, "device_name": device.Name,
		"camera_id": camera.ID, "camera_name": camera.Name, "location": device.Location,
		"algorithm_version": input.AlgorithmVersion, "rule_config_version": snapshot.Version,
	})
}

func writeRuleRejection(c *gin.Context, reason string) {
	c.JSON(http.StatusUnprocessableEntity, gin.H{
		"allowed": false, "reason_code": reason,
		"error":      gin.H{"code": "rule_rejected", "message": "classroom event does not match an active rule"},
		"request_id": c.GetString("request_id"),
	})
}
