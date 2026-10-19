package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type classroomEventValidation struct {
	CameraID           string  `json:"camera_id"`
	AlgorithmVersion   string  `json:"algorithm_version"`
	RuleConfigVersion  int64   `json:"rule_config_version"`
	BehaviorClass      string  `json:"behavior_class"`
	Confidence         float64 `json:"confidence"`
	ObservedDurationMS int64   `json:"observed_duration_ms"`
	Severity           string  `json:"severity"`
	OccurredAt         string  `json:"occurred_at"`
}

type ownershipResult struct {
	Allowed           bool      `json:"allowed"`
	ReasonCode        string    `json:"reason_code"`
	AccountID         uuid.UUID `json:"account_id"`
	DeviceID          uuid.UUID `json:"device_id"`
	DeviceName        string    `json:"device_name"`
	CameraID          uuid.UUID `json:"camera_id"`
	CameraName        string    `json:"camera_name"`
	Location          string    `json:"location"`
	AlgorithmVersion  string    `json:"algorithm_version"`
	RuleConfigVersion int64     `json:"rule_config_version"`
}

type permanentRuleRejection struct {
	ReasonCode string `json:"reason_code"`
}

func (a *App) validateClassroomEvent(ctx context.Context, event EdgeEvent, attributes ClassroomAttributes) (ownershipResult, string, error) {
	requestBody, err := json.Marshal(classroomEventValidation{
		CameraID: event.CameraID.String(), AlgorithmVersion: event.AlgorithmVersion,
		RuleConfigVersion: attributes.RuleConfigVersion, BehaviorClass: attributes.BehaviorClass,
		Confidence: attributes.Confidence, ObservedDurationMS: attributes.ObservedDurationMS,
		Severity: event.Severity, OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return ownershipResult{}, "", fmt.Errorf("encode device rule validation: %w", err)
	}
	endpoint := strings.TrimRight(a.config.DeviceServiceURL, "/") + "/internal/v1/devices/" + event.DeviceID.String() + "/classroom-events/validate"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return ownershipResult{}, "", fmt.Errorf("create device rule validation request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-Service-Token", a.config.DeviceServiceToken)
	request.Header.Set("X-Request-ID", event.EventID.String())
	response, err := a.deviceHTTP.Do(request)
	if err != nil {
		return ownershipResult{}, "", fmt.Errorf("device rule validation request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 32<<10))
	if err != nil {
		return ownershipResult{}, "", fmt.Errorf("read device rule validation response: %w", err)
	}
	switch response.StatusCode {
	case http.StatusOK:
		var result ownershipResult
		if err := json.Unmarshal(responseBody, &result); err != nil || !result.Allowed || result.AccountID == uuid.Nil || result.DeviceID != event.DeviceID || result.CameraID != event.CameraID || result.RuleConfigVersion != attributes.RuleConfigVersion {
			return ownershipResult{}, "", fmt.Errorf("device rule validation returned an invalid success response")
		}
		return result, "", nil
	case http.StatusNotFound:
		return ownershipResult{}, "device_or_camera_not_found", nil
	case http.StatusUnprocessableEntity:
		var rejection permanentRuleRejection
		if json.Unmarshal(responseBody, &rejection) != nil || strings.TrimSpace(rejection.ReasonCode) == "" {
			return ownershipResult{}, "", fmt.Errorf("device rule validation returned an invalid rejection")
		}
		return ownershipResult{}, rejection.ReasonCode, nil
	default:
		return ownershipResult{}, "", fmt.Errorf("device rule validation returned HTTP %d", response.StatusCode)
	}
}
