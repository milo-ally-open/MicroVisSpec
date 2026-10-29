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

type PushAuthorizationClient interface {
	ListAlertRecipients(context.Context, uuid.UUID, string) ([]uuid.UUID, error)
	CheckDeviceCapability(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string) error
}

type AuthPushAuthorizationClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewPushAuthorizationClient(config Config) *AuthPushAuthorizationClient {
	return &AuthPushAuthorizationClient{baseURL: strings.TrimRight(config.AuthServiceURL, "/"), token: config.AuthServiceToken,
		client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *AuthPushAuthorizationClient) ListAlertRecipients(ctx context.Context, deviceID uuid.UUID, requestID string) ([]uuid.UUID, error) {
	var all []uuid.UUID
	cursor := ""
	for {
		var response struct {
			AccountIDs []uuid.UUID `json:"account_ids"`
			NextCursor string      `json:"next_cursor"`
		}
		status, err := c.post(ctx, "/internal/v1/authorization/recipients", map[string]any{
			"device_id": deviceID.String(), "cursor": cursor, "limit": 100,
		}, requestID, &response)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("authorization service returned HTTP %d", status)
		}
		all = append(all, response.AccountIDs...)
		if response.NextCursor == "" {
			return all, nil
		}
		if response.NextCursor == cursor {
			return nil, errors.New("authorization service returned a non-advancing recipient cursor")
		}
		cursor = response.NextCursor
	}
}

func (c *AuthPushAuthorizationClient) CheckDeviceCapability(ctx context.Context, accountID, sessionID, deviceID uuid.UUID, capability, requestID string) error {
	status, err := c.post(ctx, "/internal/v1/authorization/check", map[string]string{
		"account_id": accountID.String(), "session_id": sessionID.String(), "device_id": deviceID.String(), "capability": capability,
	}, requestID, nil)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusNoContent:
		return nil
	case http.StatusForbidden, http.StatusNotFound:
		return errPushCapabilityDenied
	default:
		return fmt.Errorf("authorization service returned HTTP %d", status)
	}
}

func (c *AuthPushAuthorizationClient) post(ctx context.Context, path string, body any, requestID string, responseBody any) (int, error) {
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
	request.Header.Set("X-Internal-Service", "push-service")
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

var errPushCapabilityDenied = errors.New("alert capability is not granted")
