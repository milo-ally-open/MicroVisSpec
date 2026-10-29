package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AuthenticatedSession struct {
	AccountID uuid.UUID
	SessionID string
}

type AuthSessionValidator interface {
	ValidateSession(context.Context, string, string, string, string, string) (AuthenticatedSession, error)
}

type AuthSessionClient struct {
	baseURL string
	token   string
	client  *http.Client
}

type DeviceCapabilityAuthorizer interface {
	CheckDeviceCapability(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string) error
}

type AuthAuthorizationClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewAuthAuthorizationClient(config Config) *AuthAuthorizationClient {
	return &AuthAuthorizationClient{baseURL: strings.TrimRight(config.AuthServiceInternalURL, "/"), token: config.InternalAuthToken,
		client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *AuthAuthorizationClient) CheckDeviceCapability(ctx context.Context, accountID, sessionID, deviceID uuid.UUID, capability, requestID string) error {
	if accountID == uuid.Nil || sessionID == uuid.Nil || deviceID == uuid.Nil || c.baseURL == "" || c.token == "" {
		return errors.New("authorization client is not configured")
	}
	body, err := json.Marshal(map[string]string{"account_id": accountID.String(), "session_id": sessionID.String(),
		"device_id": deviceID.String(), "capability": capability})
	if err != nil {
		return errors.New("authorization request could not be encoded")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/authorization/check", bytes.NewReader(body))
	if err != nil {
		return errors.New("authorization request could not be created")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-Service-Token", c.token)
	request.Header.Set("X-Internal-Service", "media-service")
	request.Header.Set("X-Request-ID", requestID)
	response, err := c.client.Do(request)
	if err != nil {
		return errors.New("authorization service is unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	switch response.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusForbidden, http.StatusNotFound:
		return errDeviceCapabilityDenied
	default:
		return fmt.Errorf("authorization service returned HTTP %d", response.StatusCode)
	}
}

var errDeviceCapabilityDenied = errors.New("device capability is not granted")

func NewAuthSessionClient(config Config) *AuthSessionClient {
	return &AuthSessionClient{
		baseURL: strings.TrimRight(config.AuthServiceInternalURL, "/"),
		token:   config.InternalAuthToken,
		client:  &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (c *AuthSessionClient) ValidateSession(ctx context.Context, cookie, csrfToken, originalMethod, originalOrigin, requestID string) (AuthenticatedSession, error) {
	var identity AuthenticatedSession
	if c.baseURL == "" || c.token == "" {
		return identity, errors.New("auth service client is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/v1/sessions/validate", nil)
	if err != nil {
		return identity, errors.New("auth service request could not be created")
	}
	request.Header.Set("X-Internal-Service-Token", c.token)
	request.Header.Set("X-Internal-Service", "media-service")
	request.Header.Set("Cookie", cookie)
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.Header.Set("X-Original-Method", originalMethod)
	request.Header.Set("X-Original-Origin", originalOrigin)
	request.Header.Set("X-Request-ID", requestID)
	response, err := c.client.Do(request)
	if err != nil {
		return identity, errors.New("auth service is unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	switch response.StatusCode {
	case http.StatusNoContent:
		accountID, parseErr := uuid.Parse(response.Header.Get("X-Account-ID"))
		sessionID := strings.TrimSpace(response.Header.Get("X-Session-ID"))
		if parseErr != nil || accountID == uuid.Nil || sessionID == "" || len(sessionID) > 128 {
			return identity, errors.New("auth service returned an invalid session identity")
		}
		return AuthenticatedSession{AccountID: accountID, SessionID: sessionID}, nil
	case http.StatusUnauthorized:
		return identity, errAuthSessionUnauthorized
	case http.StatusForbidden:
		return identity, errAuthSessionForbidden
	default:
		return identity, fmt.Errorf("auth service returned HTTP %d", response.StatusCode)
	}
}

var (
	errAuthSessionUnauthorized = errors.New("auth session is not authorized")
	errAuthSessionForbidden    = errors.New("auth session request was rejected")
)

type CameraOwnership struct {
	DeviceID       string `json:"device_id"`
	CameraID       string `json:"camera_id"`
	AccountID      string `json:"account_id"`
	DeviceName     string `json:"device_name"`
	DeviceLocation string `json:"device_location"`
	CameraName     string `json:"camera_name"`
}

type CameraOwnershipResolver interface {
	ResolveCamera(context.Context, string) (CameraOwnership, error)
}

type DeviceServiceClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewDeviceServiceClient(config Config) *DeviceServiceClient {
	return &DeviceServiceClient{baseURL: strings.TrimRight(config.DeviceServiceURL, "/"), token: config.DeviceServiceToken, client: &http.Client{Timeout: 3 * time.Second}}
}

func (c *DeviceServiceClient) ResolveCamera(ctx context.Context, cameraID string) (CameraOwnership, error) {
	var ownership CameraOwnership
	endpoint := c.baseURL + "/internal/v1/cameras/" + url.PathEscape(cameraID) + "/ownership"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ownership, err
	}
	request.Header.Set("X-Internal-Service-Token", c.token)
	request.Header.Set("X-Internal-Service", "media-service")
	response, err := c.client.Do(request)
	if err != nil {
		return ownership, errors.New("device ownership service is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return ownership, errOwnershipNotFound
	}
	if response.StatusCode != http.StatusOK {
		return ownership, fmt.Errorf("device ownership service returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&ownership); err != nil {
		return ownership, errors.New("device ownership response is invalid")
	}
	return ownership, nil
}

type MediaPathState struct {
	Online  bool             `json:"online"`
	Tracks2 []MediaTrackInfo `json:"tracks2"`
}

type MediaTrackInfo struct {
	Codec      string `json:"codec"`
	CodecProps struct {
		Profile string `json:"profile"`
	} `json:"codecProps"`
}

type MediaPathControl interface {
	Check(context.Context) error
	ApplyPath(context.Context, string, string) error
	DeletePath(context.Context, string) error
	PathState(context.Context, string) (MediaPathState, error)
	KickLease(context.Context, string, string) error
}

type MediaMTXClient struct {
	baseURL string
	user    string
	pass    string
	client  *http.Client
}

func NewMediaMTXClient(config Config) *MediaMTXClient {
	return &MediaMTXClient{baseURL: strings.TrimRight(config.MediaMTXAPIURL, "/"), user: config.MediaMTXUsername, pass: config.MediaMTXPassword, client: &http.Client{Timeout: 4 * time.Second}}
}

func (c *MediaMTXClient) Check(ctx context.Context) error {
	response, err := c.do(ctx, http.MethodGet, "/v3/config/global/get", nil)
	if err != nil {
		return errors.New("MediaMTX control API is unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("MediaMTX control API returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (c *MediaMTXClient) ApplyPath(ctx context.Context, name, source string) error {
	body, err := json.Marshal(map[string]any{"source": source, "sourceOnDemand": true, "sourceOnDemandCloseAfter": "10s"})
	if err != nil {
		return errors.New("MediaMTX path configuration could not be encoded")
	}
	path := "/v3/config/paths/get/" + url.PathEscape(name)
	response, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return errors.New("MediaMTX control API is unavailable")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	response.Body.Close()
	method := http.MethodPost
	endpoint := "/v3/config/paths/add/" + url.PathEscape(name)
	if response.StatusCode == http.StatusOK {
		endpoint = "/v3/config/paths/replace/" + url.PathEscape(name)
	} else if response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("MediaMTX path lookup returned HTTP %d", response.StatusCode)
	}
	response, err = c.do(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("MediaMTX path configuration request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("MediaMTX path configuration returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (c *MediaMTXClient) DeletePath(ctx context.Context, name string) error {
	response, err := c.do(ctx, http.MethodDelete, "/v3/config/paths/delete/"+url.PathEscape(name), nil)
	if err != nil {
		return errors.New("MediaMTX path deletion request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode == http.StatusNotFound || (response.StatusCode >= 200 && response.StatusCode < 300) {
		return nil
	}
	return fmt.Errorf("MediaMTX path deletion returned HTTP %d", response.StatusCode)
}

func (c *MediaMTXClient) PathState(ctx context.Context, name string) (MediaPathState, error) {
	var state MediaPathState
	response, err := c.do(ctx, http.MethodGet, "/v3/paths/get/"+url.PathEscape(name), nil)
	if err != nil {
		return state, errors.New("MediaMTX path status request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return state, nil
	}
	if response.StatusCode != http.StatusOK {
		return state, fmt.Errorf("MediaMTX path status returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&state); err != nil {
		return state, errors.New("MediaMTX path status response is invalid")
	}
	return state, nil
}

func (c *MediaMTXClient) KickLease(ctx context.Context, streamID, leaseID string) error {
	for _, protocol := range []struct{ list, kick string }{
		{list: "/v3/webrtc/sessions/list?page=0&itemsPerPage=100", kick: "/v3/webrtc/sessions/kick/"},
		{list: "/v3/hls/sessions/list?page=0&itemsPerPage=100", kick: "/v3/hls/sessions/kick/"},
	} {
		response, err := c.do(ctx, http.MethodGet, protocol.list, nil)
		if err != nil {
			return errors.New("MediaMTX session list request failed")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return fmt.Errorf("MediaMTX session list returned HTTP %d", response.StatusCode)
		}
		var list struct {
			Items []struct {
				ID    string `json:"id"`
				Path  string `json:"path"`
				Query string `json:"query"`
			} `json:"items"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&list)
		response.Body.Close()
		if decodeErr != nil {
			return errors.New("MediaMTX session list response is invalid")
		}
		for _, session := range list.Items {
			query, _ := url.ParseQuery(strings.TrimPrefix(session.Query, "?"))
			if session.Path != streamID || query.Get("lease_id") != leaseID {
				continue
			}
			kickResponse, err := c.do(ctx, http.MethodPost, protocol.kick+url.PathEscape(session.ID), nil)
			if err != nil {
				return errors.New("MediaMTX session kick request failed")
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(kickResponse.Body, 64<<10))
			kickResponse.Body.Close()
			if kickResponse.StatusCode != http.StatusNotFound && (kickResponse.StatusCode < 200 || kickResponse.StatusCode >= 300) {
				return fmt.Errorf("MediaMTX session kick returned HTTP %d", kickResponse.StatusCode)
			}
		}
	}
	return nil
}

func (c *MediaMTXClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(c.user, c.pass)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return c.client.Do(request)
}
