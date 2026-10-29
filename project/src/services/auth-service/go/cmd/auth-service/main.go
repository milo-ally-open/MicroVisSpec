package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const sessionLifetime = 7 * 24 * time.Hour
const invitationLifetime = 24 * time.Hour

var allowedDeviceCapabilities = map[string]struct{}{
	"device.read": {}, "event.read": {}, "event.handle": {}, "stream.view": {}, "alert.receive": {},
}

type Config struct {
	DatabaseURL         string
	ListenAddress       string
	PublicOrigin        string
	CookieName          string
	CookieSecure        bool
	InternalToken       string
	DeviceServiceURL    string
	EventServiceURL     string
	DeviceServiceToken  string
	MediaServiceURL     string
	MediaServiceToken   string
	PushServiceURL      string
	PushServiceToken    string
	AdminBootstrapToken string
	RateLimitPerMinute  int
}

func LoadConfig() (Config, error) {
	var configErr error
	read := func(name string) string {
		if configErr != nil {
			return ""
		}
		value, err := configValue(name)
		if err != nil {
			configErr = err
			return ""
		}
		return value
	}
	c := Config{
		DatabaseURL:         read("DATABASE_URL"),
		ListenAddress:       env("AUTH_LISTEN_ADDRESS", ":8080"),
		PublicOrigin:        strings.TrimRight(os.Getenv("PUBLIC_ORIGIN"), "/"),
		CookieName:          env("AUTH_COOKIE_NAME", "yunshao_session"),
		CookieSecure:        envBool("AUTH_COOKIE_SECURE", true),
		InternalToken:       read("AUTH_INTERNAL_SERVICE_TOKEN"),
		DeviceServiceURL:    strings.TrimRight(os.Getenv("AUTH_DEVICE_SERVICE_URL"), "/"),
		EventServiceURL:     strings.TrimRight(os.Getenv("AUTH_EVENT_SERVICE_URL"), "/"),
		DeviceServiceToken:  read("AUTH_DEVICE_SERVICE_TOKEN"),
		MediaServiceURL:     strings.TrimRight(os.Getenv("AUTH_MEDIA_SERVICE_URL"), "/"),
		MediaServiceToken:   read("AUTH_MEDIA_SERVICE_TOKEN"),
		PushServiceURL:      strings.TrimRight(os.Getenv("AUTH_PUSH_SERVICE_URL"), "/"),
		PushServiceToken:    read("AUTH_PUSH_SERVICE_TOKEN"),
		AdminBootstrapToken: read("AUTH_ADMIN_BOOTSTRAP_TOKEN"),
		RateLimitPerMinute:  envInt("AUTH_RATE_LIMIT_PER_MINUTE", 20),
	}
	if configErr != nil {
		return c, configErr
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if c.PublicOrigin == "" {
		return c, errors.New("PUBLIC_ORIGIN is required")
	}
	if c.InternalToken == "" {
		return c, errors.New("AUTH_INTERNAL_SERVICE_TOKEN is required")
	}
	if c.DeviceServiceToken == "" || c.MediaServiceToken == "" || c.PushServiceToken == "" {
		return c, errors.New("AUTH_DEVICE_SERVICE_TOKEN, AUTH_MEDIA_SERVICE_TOKEN, and AUTH_PUSH_SERVICE_TOKEN are required")
	}
	if err := validateServiceURL("AUTH_DEVICE_SERVICE_URL", c.DeviceServiceURL); err != nil {
		return c, err
	}
	if err := validateServiceURL("AUTH_EVENT_SERVICE_URL", c.EventServiceURL); err != nil {
		return c, err
	}
	if err := validateServiceURL("AUTH_MEDIA_SERVICE_URL", c.MediaServiceURL); err != nil {
		return c, err
	}
	if err := validateServiceURL("AUTH_PUSH_SERVICE_URL", c.PushServiceURL); err != nil {
		return c, err
	}
	return c, nil
}

func validateServiceURL(name, value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL without user information", name)
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	parsed, err := strconv.Atoi(os.Getenv(key))
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

type Account struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"account_id"`
	Username       string    `gorm:"size:32;not null" json:"username"`
	NormalizedUser string    `gorm:"column:normalized_username;size:32;not null;uniqueIndex" json:"-"`
	PasswordHash   string    `gorm:"size:255;not null" json:"-"`
	UserType       string    `gorm:"column:user_type;size:16;not null;default:pc" json:"user_type"`
	Status         string    `gorm:"size:16;not null;default:active" json:"status"`
	CreatedAt      time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time `gorm:"not null" json:"updated_at"`
}

func (Account) TableName() string { return "auth.accounts" }

type Workspace struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"workspace_id"`
	OwnerAccountID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"owner_account_id"`
	Name           string    `gorm:"size:100;not null" json:"name"`
	Status         string    `gorm:"size:16;not null;default:active" json:"status"`
	CreatedAt      time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time `gorm:"not null" json:"updated_at"`
}

func (Workspace) TableName() string { return "auth.workspaces" }

type WorkspaceMember struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey" json:"member_id"`
	WorkspaceID uuid.UUID  `gorm:"type:uuid;not null;index" json:"workspace_id"`
	AccountID   uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex" json:"account_id"`
	Status      string     `gorm:"size:16;not null;default:active" json:"status"`
	InvitedBy   uuid.UUID  `gorm:"type:uuid;not null" json:"invited_by"`
	JoinedAt    time.Time  `gorm:"not null" json:"joined_at"`
	RevokedAt   *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"not null" json:"updated_at"`
}

func (WorkspaceMember) TableName() string { return "auth.workspace_members" }

type WorkspaceInvitation struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primaryKey" json:"invitation_id"`
	WorkspaceID        uuid.UUID  `gorm:"type:uuid;not null;index" json:"workspace_id"`
	CreatedBy          uuid.UUID  `gorm:"type:uuid;not null" json:"created_by"`
	TokenHash          string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	AuthorizationDraft []byte     `gorm:"type:jsonb;not null" json:"-"`
	ExpiresAt          time.Time  `gorm:"not null;index" json:"expires_at"`
	UsedAt             *time.Time `gorm:"index" json:"used_at,omitempty"`
	RevokedAt          *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	CreatedAt          time.Time  `gorm:"not null" json:"created_at"`
}

func (WorkspaceInvitation) TableName() string { return "auth.workspace_invitations" }

type MemberDeviceGrant struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey" json:"grant_id"`
	MemberID   uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_member_device_capability,priority:1" json:"member_id"`
	DeviceID   uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_member_device_capability,priority:2" json:"device_id"`
	Capability string    `gorm:"size:32;not null;uniqueIndex:idx_member_device_capability,priority:3" json:"capability"`
	CreatedAt  time.Time `gorm:"not null" json:"created_at"`
}

func (MemberDeviceGrant) TableName() string { return "auth.workspace_member_device_grants" }

type AuditLog struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"audit_id"`
	ActorID     uuid.UUID `gorm:"type:uuid;not null;index" json:"actor_account_id"`
	WorkspaceID uuid.UUID `gorm:"type:uuid;not null;index" json:"workspace_id"`
	Action      string    `gorm:"size:64;not null;index" json:"action"`
	ResourceID  string    `gorm:"size:64;not null;default:''" json:"resource_id"`
	Result      string    `gorm:"size:16;not null" json:"result"`
	Reason      string    `gorm:"size:240;not null;default:''" json:"reason,omitempty"`
	RequestID   string    `gorm:"size:64;not null;index" json:"request_id"`
	CreatedAt   time.Time `gorm:"not null;index" json:"created_at"`
}

func (AuditLog) TableName() string { return "auth.audit_logs" }

type Session struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey"`
	AccountID      uuid.UUID  `gorm:"type:uuid;not null;index"`
	TokenHash      string     `gorm:"size:64;not null;uniqueIndex"`
	CSRFHash       string     `gorm:"column:csrf_token_hash;size:64;not null"`
	LastActivityAt time.Time  `gorm:"not null"`
	ExpiresAt      time.Time  `gorm:"not null;index"`
	RevokedAt      *time.Time `gorm:"index"`
	CreatedAt      time.Time  `gorm:"not null"`
}

func (Session) TableName() string { return "auth.sessions" }

type Outbox struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey"`
	EventType     string     `gorm:"size:100;not null;index"`
	Payload       []byte     `gorm:"type:jsonb;not null"`
	CreatedAt     time.Time  `gorm:"not null;index"`
	DeliveredAt   *time.Time `gorm:"index"`
	Attempts      int        `gorm:"not null;default:0"`
	NextAttemptAt time.Time  `gorm:"not null;default:CURRENT_TIMESTAMP;index"`
	LeaseID       *uuid.UUID `gorm:"type:uuid;index"`
	LeaseUntil    *time.Time `gorm:"index"`
	LastError     string     `gorm:"size:500"`
}

func (Outbox) TableName() string { return "auth.outbox" }

func openDatabase(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, err
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS auth").Error; err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Account{}, &Workspace{}, &WorkspaceMember{}, &WorkspaceInvitation{}, &MemberDeviceGrant{}, &AuditLog{}, &Session{}, &Outbox{}); err != nil {
		return nil, err
	}
	if err := backfillOwnerWorkspaces(db); err != nil {
		return nil, err
	}
	return db, nil
}

func backfillOwnerWorkspaces(db *gorm.DB) error {
	return db.Exec(`INSERT INTO auth.workspaces (id, owner_account_id, name, status, created_at, updated_at)
		SELECT id, id, username, 'active', created_at, updated_at
		FROM auth.accounts WHERE user_type = 'pc'
		ON CONFLICT (owner_account_id) DO NOTHING`).Error
}

type App struct {
	config Config
	db     *gorm.DB
	limit  *rateLimiter
}

func NewHandler(config Config, db *gorm.DB) http.Handler {
	app := &App{config: config, db: db, limit: newRateLimiter(config.RateLimitPerMinute)}
	r := gin.New()
	r.Use(gin.Recovery(), app.requestContext(), app.accessLog())
	r.GET("/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "live"}) })
	r.GET("/health/ready", app.ready)

	api := r.Group("/api/v1/auth")
	api.POST("/register", app.sameOrigin, app.limitByIP("register"), app.register)
	api.POST("/login", app.sameOrigin, app.limitByIP("login"), app.login)
	api.GET("/me", app.me)
	api.POST("/logout", app.sameOrigin, app.logout)
	api.PUT("/password", app.sameOrigin, app.changePassword)
	api.GET("/workspace", app.getWorkspace)
	api.GET("/workspace/members", app.listWorkspaceMembers)
	api.PUT("/workspace/members/:member_id/grants", app.sameOrigin, app.replaceMemberGrants)
	api.DELETE("/workspace/members/:member_id", app.sameOrigin, app.revokeWorkspaceMember)
	api.GET("/workspace/invitations", app.listWorkspaceInvitations)
	api.POST("/workspace/invitations", app.sameOrigin, app.createInvitation)
	api.DELETE("/workspace/invitations/:invitation_id", app.sameOrigin, app.revokeWorkspaceInvitation)
	r.POST("/api/v1/invitations/redeem", app.sameOrigin, app.limitByIP("invitation-redeem"), app.redeemInvitation)
	r.POST("/internal/v1/admin/bootstrap", app.bootstrapAdmin)
	admin := r.Group("/api/v1/admin", app.requireAdmin())
	admin.GET("/accounts", app.listAdminAccounts)
	admin.GET("/health", app.adminHealth)
	admin.POST("/accounts", app.sameOrigin, app.requireAdminCSRF(), app.createAdminManagedPC)
	admin.PATCH("/accounts/:account_id/status", app.sameOrigin, app.requireAdminCSRF(), app.setAdminAccountStatus)
	admin.GET("/audit", app.listAdminAudit)

	r.GET("/internal/v1/sessions/validate", app.requireSessionValidationToken(), app.validateSession)
	r.POST("/internal/v1/authorization/check", app.requireAuthorizationService(), app.checkAuthorization)
	r.POST("/internal/v1/authorization/devices", app.requireAuthorizationService(), app.listAuthorizedDevices)
	r.POST("/internal/v1/authorization/recipients", app.requireAuthorizationService(), app.listAlertRecipients)
	return r
}

func (a *App) requestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if len(id) > 64 || strings.ContainsAny(id, "\r\n") {
			id = ""
		}
		if id == "" {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func (a *App) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http_request", "request_id", c.GetString("request_id"), "method", c.Request.Method,
			"path", c.Request.URL.Path, "status", c.Writer.Status(), "duration_ms", time.Since(start).Milliseconds())
	}
}

func (a *App) ready(c *gin.Context) {
	sqlDB, err := a.db.DB()
	if err == nil {
		err = sqlDB.PingContext(c.Request.Context())
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "database is not ready")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "dependencies": gin.H{"postgres": "ready"}})
}

func (a *App) sameOrigin(c *gin.Context) {
	if c.GetHeader("Origin") != a.config.PublicOrigin {
		writeError(c, http.StatusForbidden, "origin_rejected", "request origin is not allowed")
		c.Abort()
		return
	}
	c.Next()
}

func (a *App) limitByIP(bucket string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.limit.allow(bucket + ":" + c.ClientIP()) {
			writeError(c, http.StatusTooManyRequests, "rate_limited", "too many requests")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (a *App) requireSessionValidationToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := c.GetHeader("X-Internal-Service-Token")
		validToken := provided != "" && ((a.config.InternalToken != "" && constantTimeEqual(provided, a.config.InternalToken)) ||
			(a.config.MediaServiceToken != "" && constantTimeEqual(provided, a.config.MediaServiceToken)))
		if !validToken {
			writeError(c, http.StatusUnauthorized, "unauthorized", "service authentication required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (a *App) requireAuthorizationService() gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := c.GetHeader("X-Internal-Service-Token")
		valid := provided != "" && ((a.config.InternalToken != "" && constantTimeEqual(provided, a.config.InternalToken)) ||
			(a.config.DeviceServiceToken != "" && constantTimeEqual(provided, a.config.DeviceServiceToken)) ||
			(a.config.MediaServiceToken != "" && constantTimeEqual(provided, a.config.MediaServiceToken)) ||
			(a.config.PushServiceToken != "" && constantTimeEqual(provided, a.config.PushServiceToken)))
		if !valid {
			writeError(c, http.StatusUnauthorized, "unauthorized", "service authentication required")
			c.Abort()
			return
		}
		c.Next()
	}
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *App) register(c *gin.Context) {
	var body credentials
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "invalid request body")
		return
	}
	username := strings.TrimSpace(body.Username)
	normalized := strings.ToLower(username)
	if !validUsername(normalized) || len(body.Password) < 12 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_credentials", "username or password does not meet requirements")
		return
	}
	encoded, err := hashPassword(body.Password)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	account := Account{ID: uuid.New(), Username: username, NormalizedUser: normalized, PasswordHash: encoded,
		UserType: "pc", Status: "active", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	workspace := Workspace{ID: account.ID, OwnerAccountID: account.ID, Name: account.Username,
		Status: "active", CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt}
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(Account{}.TableName()).Create(&account).Error; err != nil {
			return err
		}
		return tx.Table(Workspace{}.TableName()).Create(&workspace).Error
	}); err != nil {
		if isUniqueViolation(err) {
			writeError(c, http.StatusConflict, "username_unavailable", "username is already in use")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "account could not be created")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"account_id": account.ID, "username": account.Username, "user_type": account.UserType})
}

type invitationGrantDraft struct {
	DeviceID     string   `json:"device_id"`
	Capabilities []string `json:"capabilities"`
}

type createInvitationRequest struct {
	DeviceGrants []invitationGrantDraft `json:"device_grants"`
}

func (a *App) createInvitation(c *gin.Context) {
	_, session, account, ok := a.loadSession(c, false)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	if !a.validCSRF(c, session) {
		writeError(c, http.StatusForbidden, "csrf_rejected", "valid CSRF token required")
		return
	}
	if account.UserType != "pc" {
		writeError(c, http.StatusForbidden, "forbidden", "only a PC workspace owner can create invitations")
		return
	}
	var body createInvitationRequest
	if err := decodeJSON(c, &body); err != nil || len(body.DeviceGrants) == 0 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "at least one device grant is required")
		return
	}
	grants, err := normalizeGrantDrafts(body.DeviceGrants)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_grants", "device grants are invalid")
		return
	}
	var workspace Workspace
	if err := a.db.Table(Workspace{}.TableName()).Where("owner_account_id = ? AND status = ?", account.ID, "active").First(&workspace).Error; err != nil {
		writeError(c, http.StatusForbidden, "workspace_unavailable", "workspace is not available")
		return
	}
	owned, err := a.verifyDeviceOwnership(c.Request.Context(), account.ID, grants)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device ownership could not be verified")
		return
	}
	if !owned {
		writeError(c, http.StatusUnprocessableEntity, "invalid_grants", "every granted device must be active and owned by this workspace")
		return
	}
	plainToken, err := randomToken(32)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	now := time.Now().UTC()
	draftBytes, err := jsonBytes(grants)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	invitation := WorkspaceInvitation{ID: uuid.New(), WorkspaceID: workspace.ID, CreatedBy: account.ID,
		TokenHash: hashToken(plainToken), AuthorizationDraft: draftBytes, ExpiresAt: now.Add(invitationLifetime), CreatedAt: now}
	audit := AuditLog{ID: uuid.New(), ActorID: account.ID, WorkspaceID: workspace.ID, Action: "invitation.created",
		ResourceID: invitation.ID.String(), Result: "success", RequestID: c.GetString("request_id"), CreatedAt: now}
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(WorkspaceInvitation{}.TableName()).Create(&invitation).Error; err != nil {
			return err
		}
		return tx.Table(AuditLog{}.TableName()).Create(&audit).Error
	}); err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "invitation could not be created")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"invitation_id": invitation.ID, "invite_token": plainToken,
		"expires_at": invitation.ExpiresAt, "uses_remaining": 1})
}

type redeemInvitationRequest struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *App) redeemInvitation(c *gin.Context) {
	var body redeemInvitationRequest
	if err := decodeJSON(c, &body); err != nil || len(body.Token) < 32 || len(body.Token) > 256 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "invitation token and account credentials are required")
		return
	}
	username := strings.TrimSpace(body.Username)
	normalized := strings.ToLower(username)
	if !validUsername(normalized) || len(body.Password) < 12 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_credentials", "username or password does not meet requirements")
		return
	}
	now := time.Now().UTC()
	var createdAccount Account
	var member WorkspaceMember
	var invitation WorkspaceInvitation
	var drafts []invitationGrantDraft
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(WorkspaceInvitation{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ?", hashToken(body.Token)).First(&invitation).Error; err != nil {
			return errors.New("invitation is invalid or unavailable")
		}
		if invitation.UsedAt != nil || invitation.RevokedAt != nil || !now.Before(invitation.ExpiresAt) {
			return errors.New("invitation is invalid or unavailable")
		}
		var workspace Workspace
		if err := tx.Table(Workspace{}.TableName()+" AS workspaces").
			Joins("JOIN auth.accounts AS owners ON owners.id = workspaces.owner_account_id").
			Where("workspaces.id = ? AND workspaces.status = ? AND owners.status = ?", invitation.WorkspaceID, "active", "active").First(&workspace).Error; err != nil {
			return errors.New("invitation is invalid or unavailable")
		}
		if err := json.Unmarshal(invitation.AuthorizationDraft, &drafts); err != nil {
			return errors.New("invitation authorization is invalid")
		}
		if len(drafts) == 0 {
			return errors.New("invitation authorization is invalid")
		}
		var existing Account
		lookupErr := tx.Table(Account{}.TableName()).Where("normalized_username = ?", normalized).First(&existing).Error
		switch {
		case errors.Is(lookupErr, gorm.ErrRecordNotFound):
			encoded, hashErr := hashPassword(body.Password)
			if hashErr != nil {
				return hashErr
			}
			createdAccount = Account{ID: uuid.New(), Username: username, NormalizedUser: normalized,
				PasswordHash: encoded, UserType: "mobile", Status: "active", CreatedAt: now, UpdatedAt: now}
			if err := tx.Table(Account{}.TableName()).Create(&createdAccount).Error; err != nil {
				return err
			}
		case lookupErr != nil:
			return lookupErr
		default:
			if existing.UserType != "mobile" || existing.Status != "active" || !verifyPassword(existing.PasswordHash, body.Password) {
				return errors.New("invitation is invalid or unavailable")
			}
			createdAccount = existing
		}
		memberErr := tx.Table(WorkspaceMember{}.TableName()).Where("account_id = ?", createdAccount.ID).First(&member).Error
		if memberErr == nil {
			if member.Status == "active" {
				return errors.New("mobile account already has an active membership")
			}
			member.WorkspaceID, member.Status, member.InvitedBy = invitation.WorkspaceID, "active", invitation.CreatedBy
			member.JoinedAt, member.RevokedAt, member.UpdatedAt = now, nil, now
			if err := tx.Table(WorkspaceMember{}.TableName()).Save(&member).Error; err != nil {
				return err
			}
		} else if errors.Is(memberErr, gorm.ErrRecordNotFound) {
			member = WorkspaceMember{ID: uuid.New(), WorkspaceID: invitation.WorkspaceID, AccountID: createdAccount.ID,
				Status: "active", InvitedBy: invitation.CreatedBy, JoinedAt: now, CreatedAt: now, UpdatedAt: now}
			if err := tx.Table(WorkspaceMember{}.TableName()).Create(&member).Error; err != nil {
				return err
			}
		} else {
			return memberErr
		}
		if err := tx.Table(MemberDeviceGrant{}.TableName()).Where("member_id = ?", member.ID).Delete(&MemberDeviceGrant{}).Error; err != nil {
			return err
		}
		for _, draft := range drafts {
			deviceID, parseErr := uuid.Parse(draft.DeviceID)
			if parseErr != nil {
				return errors.New("invitation authorization is invalid")
			}
			for _, capability := range draft.Capabilities {
				grant := MemberDeviceGrant{ID: uuid.New(), MemberID: member.ID, DeviceID: deviceID, Capability: capability, CreatedAt: now}
				if err := tx.Table(MemberDeviceGrant{}.TableName()).Create(&grant).Error; err != nil {
					return err
				}
			}
		}
		invitation.UsedAt = &now
		consumeResult := tx.Table(WorkspaceInvitation{}.TableName()).Where("id = ? AND used_at IS NULL AND revoked_at IS NULL", invitation.ID).
			Update("used_at", now)
		if consumeResult.Error != nil {
			return consumeResult.Error
		}
		if consumeResult.RowsAffected != 1 {
			return errors.New("invitation is invalid or unavailable")
		}
		audit := AuditLog{ID: uuid.New(), ActorID: createdAccount.ID, WorkspaceID: invitation.WorkspaceID,
			Action: "invitation.redeemed", ResourceID: invitation.ID.String(), Result: "success",
			RequestID: c.GetString("request_id"), CreatedAt: now}
		return tx.Table(AuditLog{}.TableName()).Create(&audit).Error
	})
	if err != nil {
		if strings.Contains(err.Error(), "mobile account already has an active membership") {
			writeError(c, http.StatusConflict, "membership_exists", "mobile account already has an active membership")
			return
		}
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			writeError(c, http.StatusConflict, "username_unavailable", "username is already in use")
			return
		}
		if strings.Contains(err.Error(), "invitation is invalid") || strings.Contains(err.Error(), "invitation authorization is invalid") {
			writeError(c, http.StatusUnprocessableEntity, "invitation_invalid", "invitation is expired, revoked, used, or invalid")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "invitation could not be redeemed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"account_id": createdAccount.ID, "username": createdAccount.Username,
		"user_type": createdAccount.UserType, "workspace_id": invitation.WorkspaceID, "member_id": member.ID})
}

func normalizeGrantDrafts(input []invitationGrantDraft) ([]invitationGrantDraft, error) {
	seen := make(map[string]struct{})
	result := make([]invitationGrantDraft, 0, len(input))
	for _, item := range input {
		deviceID, err := uuid.Parse(strings.TrimSpace(item.DeviceID))
		if err != nil || len(item.Capabilities) == 0 {
			return nil, errors.New("invalid device grant")
		}
		grant := invitationGrantDraft{DeviceID: deviceID.String(), Capabilities: make([]string, 0, len(item.Capabilities))}
		for _, capability := range item.Capabilities {
			capability = strings.TrimSpace(capability)
			if _, ok := allowedDeviceCapabilities[capability]; !ok {
				return nil, errors.New("unsupported device capability")
			}
			key := grant.DeviceID + ":" + capability
			if _, ok := seen[key]; ok {
				return nil, errors.New("duplicate device capability")
			}
			seen[key] = struct{}{}
			grant.Capabilities = append(grant.Capabilities, capability)
		}
		result = append(result, grant)
	}
	return result, nil
}

type deviceOwnerSnapshot struct {
	DeviceID  uuid.UUID `json:"device_id"`
	AccountID uuid.UUID `json:"account_id"`
	Active    bool      `json:"active"`
}

func (a *App) verifyDeviceOwnership(ctx context.Context, ownerID uuid.UUID, grants []invitationGrantDraft) (bool, error) {
	seen := make(map[uuid.UUID]struct{}, len(grants))
	for _, grant := range grants {
		deviceID, err := uuid.Parse(grant.DeviceID)
		if err != nil {
			return false, nil
		}
		if _, exists := seen[deviceID]; exists {
			continue
		}
		seen[deviceID] = struct{}{}
		snapshot, found, err := a.getDeviceOwnership(ctx, deviceID)
		if err != nil {
			return false, err
		}
		if !found {
			return false, nil
		}
		if snapshot.DeviceID != deviceID || snapshot.AccountID != ownerID || !snapshot.Active {
			return false, nil
		}
	}
	return true, nil
}

func (a *App) getDeviceOwnership(ctx context.Context, deviceID uuid.UUID) (deviceOwnerSnapshot, bool, error) {
	var snapshot deviceOwnerSnapshot
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		a.config.DeviceServiceURL+"/internal/v1/devices/"+deviceID.String()+"/ownership", nil)
	if err != nil {
		return snapshot, false, err
	}
	req.Header.Set("X-Internal-Service-Token", a.config.DeviceServiceToken)
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return snapshot, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return snapshot, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return snapshot, false, fmt.Errorf("device ownership service returned status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&snapshot); err != nil {
		return snapshot, false, err
	}
	return snapshot, true, nil
}

func (a *App) login(c *gin.Context) {
	var body credentials
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "invalid request body")
		return
	}
	var account Account
	err := a.db.Table(Account{}.TableName()).Where("normalized_username = ?", strings.ToLower(strings.TrimSpace(body.Username))).First(&account).Error
	if err != nil || !verifyPassword(account.PasswordHash, body.Password) || account.Status != "active" {
		slog.Warn("auth_login_failed", "request_id", c.GetString("request_id"), "client_ip", c.ClientIP())
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "username or password is incorrect")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	csrf, err := randomToken(32)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	now := time.Now().UTC()
	session := Session{ID: uuid.New(), AccountID: account.ID, TokenHash: hashToken(token), CSRFHash: hashToken(csrf), LastActivityAt: now, ExpiresAt: now.Add(sessionLifetime), CreatedAt: now}
	if err := a.db.Table(Session{}.TableName()).Create(&session).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "session could not be created")
		return
	}
	a.setCookie(c, token, session.ExpiresAt)
	slog.Info("auth_login_succeeded", "request_id", c.GetString("request_id"), "account_id", account.ID)
	c.JSON(http.StatusOK, gin.H{"account_id": account.ID, "username": account.Username,
		"user_type": account.UserType, "csrf_token": csrf})
}

func (a *App) me(c *gin.Context) {
	_, session, account, ok := a.loadSession(c, true)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	csrf, err := randomToken(32)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	if err := a.db.Table(Session{}.TableName()).Where("id = ?", session.ID).Update("csrf_token_hash", hashToken(csrf)).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "session could not be refreshed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"account_id": account.ID, "username": account.Username,
		"user_type": account.UserType, "csrf_token": csrf})
}

func (a *App) logout(c *gin.Context) {
	_, session, _, ok := a.loadSession(c, false)
	if !ok {
		a.clearCookie(c)
		c.Status(http.StatusNoContent)
		return
	}
	if !a.validCSRF(c, session) {
		writeError(c, http.StatusForbidden, "csrf_rejected", "valid CSRF token required")
		return
	}
	now := time.Now().UTC()
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(Session{}.TableName()).Where("id = ? AND revoked_at IS NULL", session.ID).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return writeRevocation(tx, session, now)
	}); err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "session could not be revoked")
		return
	}
	a.clearCookie(c)
	c.Status(http.StatusNoContent)
}

type passwordChange struct {
	OldPassword             string `json:"old_password"`
	NewPassword             string `json:"new_password"`
	NewPasswordConfirmation string `json:"new_password_confirmation"`
}

func (a *App) changePassword(c *gin.Context) {
	_, current, account, ok := a.loadSession(c, false)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	if !a.validCSRF(c, current) {
		writeError(c, http.StatusForbidden, "csrf_rejected", "valid CSRF token required")
		return
	}
	var body passwordChange
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "invalid request body")
		return
	}
	if !verifyPassword(account.PasswordHash, body.OldPassword) {
		writeError(c, http.StatusUnprocessableEntity, "old_password_invalid", "old password is incorrect")
		return
	}
	if len(body.NewPassword) < 12 || body.NewPassword != body.NewPasswordConfirmation {
		writeError(c, http.StatusUnprocessableEntity, "password_requirements_not_met", "new password must be at least 12 characters and match its confirmation")
		return
	}
	newHash, err := hashPassword(body.NewPassword)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	now := time.Now().UTC()
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(Account{}.TableName()).Where("id = ?", account.ID).Update("password_hash", newHash).Error; err != nil {
			return err
		}
		var sessions []Session
		if err := tx.Table(Session{}.TableName()).Where("account_id = ? AND revoked_at IS NULL", account.ID).Find(&sessions).Error; err != nil {
			return err
		}
		for _, session := range sessions {
			if err := tx.Table(Session{}.TableName()).Where("id = ?", session.ID).Update("revoked_at", now).Error; err != nil {
				return err
			}
			if err := writeRevocation(tx, session, now); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "password could not be changed")
		return
	}
	a.clearCookie(c)
	c.Status(http.StatusNoContent)
}

func (a *App) validateSession(c *gin.Context) {
	_, session, account, ok := a.loadSession(c, true)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	originalMethod := strings.ToUpper(strings.TrimSpace(c.GetHeader("X-Original-Method")))
	if originalMethod != "" && originalMethod != http.MethodGet && originalMethod != http.MethodHead && originalMethod != http.MethodOptions {
		if c.GetHeader("X-Original-Origin") != a.config.PublicOrigin || !a.validCSRF(c, session) {
			writeError(c, http.StatusForbidden, "csrf_rejected", "same-origin request with a valid CSRF token required")
			return
		}
	}
	c.Header("X-Account-ID", account.ID.String())
	c.Header("X-Session-ID", session.ID.String())
	c.Header("X-User-Type", account.UserType)
	c.Status(http.StatusNoContent)
}

func (a *App) loadSession(c *gin.Context, touch bool) (string, Session, Account, bool) {
	cookie, err := c.Cookie(a.config.CookieName)
	if err != nil || cookie == "" {
		return "", Session{}, Account{}, false
	}
	var session Session
	err = a.db.Table(Session{}.TableName()).Where("token_hash = ? AND revoked_at IS NULL", hashToken(cookie)).First(&session).Error
	if err != nil || time.Now().UTC().After(session.ExpiresAt) {
		return "", Session{}, Account{}, false
	}
	if touch {
		now := time.Now().UTC()
		if a.db.Table(Session{}.TableName()).Where("id = ?", session.ID).Updates(map[string]any{"last_activity_at": now, "expires_at": now.Add(sessionLifetime)}).Error != nil {
			return "", Session{}, Account{}, false
		}
		session.LastActivityAt, session.ExpiresAt = now, now.Add(sessionLifetime)
	}
	var account Account
	if err := a.db.Table(Account{}.TableName()).Where("id = ? AND status = ?", session.AccountID, "active").First(&account).Error; err != nil {
		return "", Session{}, Account{}, false
	}
	return cookie, session, account, true
}

func (a *App) validCSRF(c *gin.Context, session Session) bool {
	token := c.GetHeader("X-CSRF-Token")
	return token != "" && constantTimeEqual(hashToken(token), session.CSRFHash)
}

func (a *App) setCookie(c *gin.Context, token string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{Name: a.config.CookieName, Value: token, Path: "/", Expires: expires,
		MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: a.config.CookieSecure, SameSite: http.SameSiteLaxMode})
}

func (a *App) clearCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: a.config.CookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		HttpOnly: true, Secure: a.config.CookieSecure, SameSite: http.SameSiteLaxMode})
}

func writeRevocation(tx *gorm.DB, session Session, now time.Time) error {
	eventID := uuid.New()
	payload, err := jsonBytes(map[string]string{"event_id": eventID.String(), "event_type": "session.revoked", "account_id": session.AccountID.String(), "session_id": session.ID.String(), "occurred_at": now.Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	return tx.Table(Outbox{}.TableName()).Create(&Outbox{ID: eventID, EventType: "session.revoked", Payload: payload, CreatedAt: now, NextAttemptAt: now}).Error
}

func decodeJSON(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	return c.ShouldBindJSON(target)
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "request_id": c.GetString("request_id")})
}

func validUsername(value string) bool {
	if len(value) < 3 || len(value) > 32 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_') {
			return false
		}
	}
	return true
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const memory, iterations, parallelism, keyLength = 64 * 1024, 3, 2, 32
	derived := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", memory, iterations, parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(derived)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory, iterations, parallelism uint64
	for _, item := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(item, "=", 2)
		if len(pair) != 2 {
			return false
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return false
		}
		switch pair[0] {
		case "m":
			memory = value
		case "t":
			iterations = value
		case "p":
			parallelism = value
		}
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	key, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(salt) < 16 || len(key) < 16 || memory < 8 || memory > 256*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 16 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(key)))
	return constantTimeEqual(hex.EncodeToString(actual), hex.EncodeToString(key))
}

func jsonBytes(value any) ([]byte, error) { return json.Marshal(value) }

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	var diff byte
	for i := range left {
		diff |= left[i] ^ right[i]
	}
	return diff == 0
}

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "duplicate key")
}

type rateWindow struct {
	start time.Time
	count int
}

type rateLimiter struct {
	mu      sync.Mutex
	max     int
	entries map[string]rateWindow
}

func newRateLimiter(max int) *rateLimiter {
	return &rateLimiter{max: max, entries: make(map[string]rateWindow)}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	w := l.entries[key]
	if now.Sub(w.start) >= time.Minute {
		w = rateWindow{start: now}
	}
	w.count++
	l.entries[key] = w
	return w.count <= l.max
}

func run() error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	db, err := openDatabase(config.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database initialization failed: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	server := &http.Server{Addr: config.ListenAddress, Handler: NewHandler(config, db), ReadHeaderTimeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return err
	}
	slog.Info("auth_service_starting", "listen_address", config.ListenAddress)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go runOutboxWorker(workerCtx, db, config, nil)
	return server.ListenAndServe()
}

func main() {
	if err := run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("auth_service_stopped", "error", err.Error())
		os.Exit(1)
	}
}
