package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

var errDeviceCapabilityDenied = errors.New("device capability is not granted")

type DeviceAuthorizationClient interface {
	ListAuthorizedDevices(context.Context, uuid.UUID, string, string, string, int, string) ([]uuid.UUID, string, error)
	CheckDeviceCapability(context.Context, uuid.UUID, string, uuid.UUID, string, string) error
}

type AuthDeviceAuthorizationClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewDeviceAuthorizationClient(config Config) *AuthDeviceAuthorizationClient {
	return &AuthDeviceAuthorizationClient{baseURL: strings.TrimRight(config.AuthServiceURL, "/"), token: config.InternalToken,
		client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *AuthDeviceAuthorizationClient) ListAuthorizedDevices(ctx context.Context, accountID uuid.UUID, sessionID, capability, cursor string, limit int, requestID string) ([]uuid.UUID, string, error) {
	var result struct {
		DeviceIDs  []uuid.UUID `json:"device_ids"`
		NextCursor string      `json:"next_cursor"`
	}
	body := map[string]any{"account_id": accountID.String(), "session_id": sessionID, "capability": capability, "cursor": cursor, "limit": limit}
	status, err := c.post(ctx, "/internal/v1/authorization/devices", body, requestID, &result)
	if err != nil {
		return nil, "", err
	}
	if status == http.StatusForbidden {
		return nil, "", errDeviceCapabilityDenied
	}
	if status != http.StatusOK {
		return nil, "", fmt.Errorf("authorization service returned HTTP %d", status)
	}
	return result.DeviceIDs, result.NextCursor, nil
}

func (c *AuthDeviceAuthorizationClient) CheckDeviceCapability(ctx context.Context, accountID uuid.UUID, sessionID string, deviceID uuid.UUID, capability, requestID string) error {
	body := map[string]string{"account_id": accountID.String(), "session_id": sessionID,
		"device_id": deviceID.String(), "capability": capability}
	status, err := c.post(ctx, "/internal/v1/authorization/check", body, requestID, nil)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusNoContent:
		return nil
	case http.StatusForbidden, http.StatusNotFound:
		return errDeviceCapabilityDenied
	default:
		return fmt.Errorf("authorization service returned HTTP %d", status)
	}
}

func (c *AuthDeviceAuthorizationClient) post(ctx context.Context, path string, body any, requestID string, responseBody any) (int, error) {
	if c.baseURL == "" || c.token == "" {
		return 0, errors.New("authorization client is not configured")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, errors.New("authorization request could not be encoded")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return 0, errors.New("authorization request could not be created")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-Service-Token", c.token)
	request.Header.Set("X-Internal-Service", "device-service")
	request.Header.Set("X-Request-ID", requestID)
	response, err := c.client.Do(request)
	if err != nil {
		return 0, errors.New("authorization service is unavailable")
	}
	defer response.Body.Close()
	if responseBody != nil && response.StatusCode == http.StatusOK {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(responseBody); err != nil {
			return 0, errors.New("authorization response is invalid")
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	}
	return response.StatusCode, nil
}
