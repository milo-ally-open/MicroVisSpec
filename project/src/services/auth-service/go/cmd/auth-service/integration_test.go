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
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
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

	config := Config{PublicOrigin: "https://yunshao.test", CookieName: "test_session", InternalToken: "integration-internal-token",
		MediaServiceToken: "integration-media-token", RateLimitPerMinute: 1000}
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
	mediaServiceHeaders := map[string]string{
		"Cookie": cookie.Name + "=" + cookie.Value, "X-Internal-Service-Token": config.MediaServiceToken,
		"X-Original-Method": http.MethodPost, "X-Original-Origin": config.PublicOrigin, "X-CSRF-Token": meBody.CSRF,
	}
	mediaValidated := request(t, handler, http.MethodGet, "/internal/v1/sessions/validate", "", mediaServiceHeaders, config)
	if mediaValidated.Code != http.StatusNoContent || mediaValidated.Header().Get("X-Account-ID") != loginBody.AccountID || mediaValidated.Header().Get("X-Session-ID") == "" {
		t.Fatalf("media-service session validation status=%d headers=%v", mediaValidated.Code, mediaValidated.Header())
	}
	mediaServiceHeaders["X-Internal-Service-Token"] = "wrong-media-token"
	mediaValidated = request(t, handler, http.MethodGet, "/internal/v1/sessions/validate", "", mediaServiceHeaders, config)
	if mediaValidated.Code != http.StatusUnauthorized {
		t.Fatalf("invalid media-service token status=%d body=%s", mediaValidated.Code, mediaValidated.Body.String())
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

func TestWorkspaceMemberRevocationRevokesSessionsAndQueuesMediaEvents(t *testing.T) {
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

	now := time.Now().UTC()
	ownerName := "owner_" + uuid.NewString()[:8]
	memberName := "member_" + uuid.NewString()[:8]
	owner := Account{ID: uuid.New(), Username: ownerName, NormalizedUser: ownerName,
		PasswordHash: "unused", UserType: "pc", Status: "active", CreatedAt: now, UpdatedAt: now}
	memberAccount := Account{ID: uuid.New(), Username: memberName, NormalizedUser: memberName,
		PasswordHash: "unused", UserType: "mobile", Status: "active", CreatedAt: now, UpdatedAt: now}
	workspace := Workspace{ID: owner.ID, OwnerAccountID: owner.ID, Name: owner.Username, Status: "active", CreatedAt: now, UpdatedAt: now}
	member := WorkspaceMember{ID: uuid.New(), WorkspaceID: workspace.ID, AccountID: memberAccount.ID, Status: "active", InvitedBy: owner.ID,
		JoinedAt: now, CreatedAt: now, UpdatedAt: now}
	sessions := []Session{
		{ID: uuid.New(), AccountID: memberAccount.ID, TokenHash: uuid.NewString(), CSRFHash: uuid.NewString(), LastActivityAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now},
		{ID: uuid.New(), AccountID: memberAccount.ID, TokenHash: uuid.NewString(), CSRFHash: uuid.NewString(), LastActivityAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now},
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&memberAccount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&workspace).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions {
		if err := db.Create(&session).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = db.Table(Outbox{}.TableName()).Where("payload->>'account_id' = ?", memberAccount.ID.String()).Delete(&Outbox{}).Error
		_ = db.Table(AuditLog{}.TableName()).Where("workspace_id = ? AND resource_id = ?", workspace.ID, member.ID.String()).Delete(&AuditLog{}).Error
		_ = db.Delete(&Session{}, "account_id = ?", memberAccount.ID).Error
		_ = db.Delete(&WorkspaceMember{}, "id = ?", member.ID).Error
		_ = db.Delete(&Workspace{}, "id = ?", workspace.ID).Error
		_ = db.Delete(&Account{}, "id IN ?", []uuid.UUID{owner.ID, memberAccount.ID}).Error
	})

	if err := db.Transaction(func(tx *gorm.DB) error {
		return revokeWorkspaceMemberTx(tx, member, owner, workspace, "member-revoke-test", now)
	}); err != nil {
		t.Fatalf("revoke member sessions: %v", err)
	}
	var storedMember WorkspaceMember
	if err := db.Table(WorkspaceMember{}.TableName()).Where("id = ?", member.ID).Take(&storedMember).Error; err != nil {
		t.Fatal(err)
	}
	if storedMember.Status != "revoked" || storedMember.RevokedAt == nil {
		t.Fatalf("member status was not revoked transactionally: %+v", storedMember)
	}
	var revoked int64
	if err := db.Table(Session{}.TableName()).Where("account_id = ? AND revoked_at = ?", memberAccount.ID, now).Count(&revoked).Error; err != nil {
		t.Fatal(err)
	}
	if revoked != int64(len(sessions)) {
		t.Fatalf("expected %d revoked sessions, got %d", len(sessions), revoked)
	}
	var queued int64
	if err := db.Table(Outbox{}.TableName()).Where("event_type = ? AND payload->>'account_id' = ?", "session.revoked", memberAccount.ID.String()).Count(&queued).Error; err != nil {
		t.Fatal(err)
	}
	if queued != int64(len(sessions)) {
		t.Fatalf("expected %d media/push revocation events, got %d", len(sessions), queued)
	}
	var audits int64
	if err := db.Table(AuditLog{}.TableName()).Where("action = ? AND workspace_id = ? AND resource_id = ? AND request_id = ?",
		"member.revoked", workspace.ID, member.ID.String(), "member-revoke-test").Count(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("expected one member revocation audit, got %d", audits)
	}
}

func TestWorkspaceOwnerCanRenameWorkspaceAndCreatesAuditRecord(t *testing.T) {
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
	config := Config{PublicOrigin: "https://yunshao.test", CookieName: "test_session", InternalToken: "integration-internal-token",
		MediaServiceToken: "integration-media-token", RateLimitPerMinute: 1000}
	handler := NewHandler(config, db)
	now := time.Now().UTC()
	username := "workspace_" + uuid.NewString()[:8]
	owner := Account{ID: uuid.New(), Username: username, NormalizedUser: username, PasswordHash: "unused", UserType: "pc", Status: "active", CreatedAt: now, UpdatedAt: now}
	workspace := Workspace{ID: owner.ID, OwnerAccountID: owner.ID, Name: "Original", Status: "active", CreatedAt: now, UpdatedAt: now}
	cookieValue, csrfValue := uuid.NewString(), uuid.NewString()
	session := Session{ID: uuid.New(), AccountID: owner.ID, TokenHash: hashToken(cookieValue), CSRFHash: hashToken(csrfValue),
		LastActivityAt: now, ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(Workspace{}.TableName()).Create(&workspace).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(Session{}.TableName()).Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Table(AuditLog{}.TableName()).Where("workspace_id = ? AND action = ?", workspace.ID, "workspace.updated").Delete(&AuditLog{}).Error
		_ = db.Table(Session{}.TableName()).Where("id = ?", session.ID).Delete(&Session{}).Error
		_ = db.Table(Workspace{}.TableName()).Where("id = ?", workspace.ID).Delete(&Workspace{}).Error
		_ = db.Table(Account{}.TableName()).Where("id = ?", owner.ID).Delete(&Account{}).Error
	})
	response := request(t, handler, http.MethodPut, "/api/v1/auth/workspace", `{"name":"Research Lab"}`, map[string]string{
		"Cookie": config.CookieName + "=" + cookieValue, "X-CSRF-Token": csrfValue,
	}, config)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"Research Lab"`) {
		t.Fatalf("rename workspace status=%d body=%s", response.Code, response.Body.String())
	}
	var stored Workspace
	if err := db.Table(Workspace{}.TableName()).Where("id = ?", workspace.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Research Lab" {
		t.Fatalf("workspace name was not saved: %q", stored.Name)
	}
	var auditCount int64
	if err := db.Table(AuditLog{}.TableName()).Where("workspace_id = ? AND action = ? AND resource_id = ?", workspace.ID, "workspace.updated", workspace.ID.String()).Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected one workspace update audit, got %d", auditCount)
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
