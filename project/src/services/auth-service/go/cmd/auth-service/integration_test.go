//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAuthSessionLifecycleIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL must point at an isolated PostgreSQL test database")
	}
	db, err := openDatabase(dsn)
	if err != nil {
		t.Fatalf("open isolated test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	config := Config{PublicOrigin: "https://yunshao.test", CookieName: "test_session", InternalToken: "integration-internal-token", RateLimitPerMinute: 1000}
	handler := NewHandler(config, db)
	username := "itest_" + uuid.NewString()[:8]
	register := request(t, handler, http.MethodPost, "/api/v1/auth/register", `{"username":"`+username+`","password":"correct horse battery staple"}`, nil, config)
	if register.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", register.Code, register.Body.String())
	}
	duplicate := request(t, handler, http.MethodPost, "/api/v1/auth/register", `{"username":"`+strings.ToUpper(username)+`","password":"correct horse battery staple"}`, nil, config)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate registration status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	weak := request(t, handler, http.MethodPost, "/api/v1/auth/register", `{"username":"weak_`+username+`","password":"short"}`, nil, config)
	if weak.Code != http.StatusUnprocessableEntity {
		t.Fatalf("weak registration status=%d body=%s", weak.Code, weak.Body.String())
	}

	login := request(t, handler, http.MethodPost, "/api/v1/auth/login", `{"username":"`+username+`","password":"correct horse battery staple"}`, nil, config)
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	var loginBody struct {
		AccountID string `json:"account_id"`
		CSRF      string `json:"csrf_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &loginBody); err != nil {
		t.Fatal(err)
	}
	var cookie *http.Cookie
	for _, candidate := range login.Result().Cookies() {
		if candidate.Name == config.CookieName {
			cookie = candidate
		}
	}
	if cookie == nil || cookie.Value == "" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie missing or unsafe: %#v", cookie)
	}

	me := request(t, handler, http.MethodGet, "/api/v1/auth/me", "", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}, config)
	if me.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", me.Code, me.Body.String())
	}
	var meBody struct {
		AccountID string `json:"account_id"`
		CSRF      string `json:"csrf_token"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &meBody); err != nil {
		t.Fatal(err)
	}
	if meBody.AccountID != loginBody.AccountID || meBody.CSRF == "" {
		t.Fatalf("me identity mismatch: login=%+v me=%+v", loginBody, meBody)
	}

	internalHeaders := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value, "X-Internal-Service-Token": config.InternalToken}
	validated := request(t, handler, http.MethodGet, "/internal/v1/sessions/validate", "", internalHeaders, config)
	if validated.Code != http.StatusNoContent || validated.Header().Get("X-Account-ID") != loginBody.AccountID || validated.Header().Get("X-Session-ID") == "" {
		t.Fatalf("session validation status=%d headers=%v", validated.Code, validated.Header())
	}
	proxyWriteHeaders := map[string]string{
		"Cookie": cookie.Name + "=" + cookie.Value, "X-Internal-Service-Token": config.InternalToken,
		"X-Original-Method": http.MethodPut, "X-Original-Origin": config.PublicOrigin, "X-CSRF-Token": meBody.CSRF,
	}
	proxyWrite := request(t, handler, http.MethodGet, "/internal/v1/sessions/validate", "", proxyWriteHeaders, config)
	if proxyWrite.Code != http.StatusNoContent {
		t.Fatalf("same-origin proxied write with valid CSRF should pass, status=%d body=%s", proxyWrite.Code, proxyWrite.Body.String())
	}
	proxyWriteHeaders["X-CSRF-Token"] = "wrong"
	proxyWrite = request(t, handler, http.MethodGet, "/internal/v1/sessions/validate", "", proxyWriteHeaders, config)
	if proxyWrite.Code != http.StatusForbidden {
		t.Fatalf("proxied write with invalid CSRF should fail, status=%d body=%s", proxyWrite.Code, proxyWrite.Body.String())
	}
	proxyWriteHeaders["X-CSRF-Token"] = meBody.CSRF
	proxyWriteHeaders["X-Original-Origin"] = "https://attacker.example"
	proxyWrite = request(t, handler, http.MethodGet, "/internal/v1/sessions/validate", "", proxyWriteHeaders, config)
	if proxyWrite.Code != http.StatusForbidden {
		t.Fatalf("cross-origin proxied write should fail, status=%d body=%s", proxyWrite.Code, proxyWrite.Body.String())
	}
	forgedIdentity := request(t, handler, http.MethodGet, "/internal/v1/sessions/validate", "", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value, "X-Internal-Service-Token": "wrong", "X-Account-ID": loginBody.AccountID}, config)
	if forgedIdentity.Code != http.StatusUnauthorized {
		t.Fatalf("forged identity status=%d body=%s", forgedIdentity.Code, forgedIdentity.Body.String())
	}

	badCSRF := request(t, handler, http.MethodPost, "/api/v1/auth/logout", "{}", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value, "X-CSRF-Token": "wrong"}, config)
	if badCSRF.Code != http.StatusForbidden {
		t.Fatalf("bad CSRF status=%d body=%s", badCSRF.Code, badCSRF.Body.String())
	}
	logout := request(t, handler, http.MethodPost, "/api/v1/auth/logout", "{}", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value, "X-CSRF-Token": meBody.CSRF}, config)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
	invalidated := request(t, handler, http.MethodGet, "/internal/v1/sessions/validate", "", internalHeaders, config)
	if invalidated.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status=%d body=%s", invalidated.Code, invalidated.Body.String())
	}
	duplicateLogout := request(t, handler, http.MethodPost, "/api/v1/auth/logout", "{}", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}, config)
	if duplicateLogout.Code != http.StatusNoContent {
		t.Fatalf("repeated logout status=%d body=%s", duplicateLogout.Code, duplicateLogout.Body.String())
	}
	var outboxCount int64
	if err := db.Table(Outbox{}.TableName()).Where("event_type = ? AND payload->>'account_id' = ?", "session.revoked", loginBody.AccountID).Count(&outboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected one transactional revocation event; got %d", outboxCount)
	}
}

func request(t *testing.T, handler http.Handler, method, path, body string, headers map[string]string, config Config) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Origin", config.PublicOrigin)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
