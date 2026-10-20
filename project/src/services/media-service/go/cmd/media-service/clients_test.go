package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestAuthSessionClientForwardsBrowserContextAndUsesTrustedIdentity(t *testing.T) {
	wantAccountID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/internal/v1/sessions/validate" {
			t.Errorf("unexpected auth request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Internal-Service-Token") != "auth-token" || r.Header.Get("X-Internal-Service") != "media-service" || r.Header.Get("Cookie") != "yunshao_session=browser-cookie" {
			t.Errorf("auth token or browser cookie missing")
		}
		if r.Header.Get("X-CSRF-Token") != "csrf-token" || r.Header.Get("X-Original-Method") != http.MethodPost || r.Header.Get("X-Original-Origin") != "https://app.example.test" {
			t.Errorf("original request security metadata missing")
		}
		if r.Header.Get("X-Account-ID") != "" || r.Header.Get("X-Session-ID") != "" {
			t.Errorf("client identity headers must not be sent to auth-service")
		}
		w.Header().Set("X-Account-ID", wantAccountID.String())
		w.Header().Set("X-Session-ID", "trusted-session")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewAuthSessionClient(Config{AuthServiceInternalURL: server.URL, InternalAuthToken: "auth-token"})
	identity, err := client.ValidateSession(context.Background(), "yunshao_session=browser-cookie", "csrf-token", http.MethodPost,
		"https://app.example.test", "request-123")
	if err != nil || identity.AccountID != wantAccountID || identity.SessionID != "trusted-session" {
		t.Fatalf("unexpected validated identity: %+v err=%v", identity, err)
	}
}

func TestAuthSessionClientRejectsInvalidSessionsAndMalformedIdentity(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		accountID  string
		sessionID  string
		wantErr    error
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized, wantErr: errAuthSessionUnauthorized},
		{name: "csrf rejected", statusCode: http.StatusForbidden, wantErr: errAuthSessionForbidden},
		{name: "identity missing", statusCode: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Account-ID", test.accountID)
				w.Header().Set("X-Session-ID", test.sessionID)
				w.WriteHeader(test.statusCode)
			}))
			defer server.Close()
			client := NewAuthSessionClient(Config{AuthServiceInternalURL: server.URL, InternalAuthToken: "auth-token"})
			_, err := client.ValidateSession(context.Background(), "yunshao_session=cookie", "", http.MethodGet, "", "request-456")
			if test.wantErr != nil && err != test.wantErr {
				t.Fatalf("got error %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && err == nil {
				t.Fatal("an empty auth-service identity must fail closed")
			}
		})
	}
}

func TestMediaMTXPathConfigurationUsesOnDemandSource(t *testing.T) {
	var added map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/config/paths/get/stream-id":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v3/config/paths/add/stream-id":
			if user, password, ok := r.BasicAuth(); !ok || user != "control-user" || password != "control-password" {
				t.Errorf("MediaMTX control credentials were not sent")
			}
			if err := json.NewDecoder(r.Body).Decode(&added); err != nil {
				t.Errorf("decode path configuration: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected MediaMTX API request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &MediaMTXClient{baseURL: server.URL, user: "control-user", pass: "control-password", client: server.Client()}
	if err := client.ApplyPath(context.Background(), "stream-id", "rtsp://10.0.0.8/stream"); err != nil {
		t.Fatal(err)
	}
	if added["source"] != "rtsp://10.0.0.8/stream" || added["sourceOnDemand"] != true || added["sourceOnDemandCloseAfter"] != "10s" {
		t.Fatalf("unexpected MediaMTX path configuration: %v", added)
	}
}
