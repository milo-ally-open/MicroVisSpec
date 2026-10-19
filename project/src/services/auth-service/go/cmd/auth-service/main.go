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
	"gorm.io/gorm/logger"
)

const sessionLifetime = 7 * 24 * time.Hour

type Config struct {
	DatabaseURL        string
	ListenAddress      string
	PublicOrigin       string
	CookieName         string
	CookieSecure       bool
	InternalToken      string
	MediaServiceURL    string
	MediaServiceToken  string
	PushServiceURL     string
	PushServiceToken   string
	RateLimitPerMinute int
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
		DatabaseURL:        read("DATABASE_URL"),
		ListenAddress:      env("AUTH_LISTEN_ADDRESS", ":8080"),
		PublicOrigin:       strings.TrimRight(os.Getenv("PUBLIC_ORIGIN"), "/"),
		CookieName:         env("AUTH_COOKIE_NAME", "yunshao_session"),
		CookieSecure:       envBool("AUTH_COOKIE_SECURE", true),
		InternalToken:      read("AUTH_INTERNAL_SERVICE_TOKEN"),
		MediaServiceURL:    strings.TrimRight(os.Getenv("AUTH_MEDIA_SERVICE_URL"), "/"),
		MediaServiceToken:  read("AUTH_MEDIA_SERVICE_TOKEN"),
		PushServiceURL:     strings.TrimRight(os.Getenv("AUTH_PUSH_SERVICE_URL"), "/"),
		PushServiceToken:   read("AUTH_PUSH_SERVICE_TOKEN"),
		RateLimitPerMinute: envInt("AUTH_RATE_LIMIT_PER_MINUTE", 20),
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
	if c.MediaServiceToken == "" || c.PushServiceToken == "" {
		return c, errors.New("AUTH_MEDIA_SERVICE_TOKEN and AUTH_PUSH_SERVICE_TOKEN are required")
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
	CreatedAt      time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time `gorm:"not null" json:"updated_at"`
}

func (Account) TableName() string { return "auth.accounts" }

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
	if err := db.AutoMigrate(&Account{}, &Session{}, &Outbox{}); err != nil {
		return nil, err
	}
	return db, nil
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

	internal := r.Group("/internal/v1")
	internal.Use(app.requireInternalToken())
	internal.GET("/sessions/validate", app.validateSession)
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

func (a *App) requireInternalToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !constantTimeEqual(c.GetHeader("X-Internal-Service-Token"), a.config.InternalToken) {
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
	account := Account{ID: uuid.New(), Username: username, NormalizedUser: normalized, PasswordHash: encoded, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := a.db.Table(Account{}.TableName()).Create(&account).Error; err != nil {
		if isUniqueViolation(err) {
			writeError(c, http.StatusConflict, "username_unavailable", "username is already in use")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "account could not be created")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"account_id": account.ID, "username": account.Username})
}

func (a *App) login(c *gin.Context) {
	var body credentials
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "invalid request body")
		return
	}
	var account Account
	err := a.db.Table(Account{}.TableName()).Where("normalized_username = ?", strings.ToLower(strings.TrimSpace(body.Username))).First(&account).Error
	if err != nil || !verifyPassword(account.PasswordHash, body.Password) {
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
	c.JSON(http.StatusOK, gin.H{"account_id": account.ID, "username": account.Username, "csrf_token": csrf})
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
	c.JSON(http.StatusOK, gin.H{"account_id": account.ID, "username": account.Username, "csrf_token": csrf})
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
	if err := a.db.Table(Account{}.TableName()).Where("id = ?", session.AccountID).First(&account).Error; err != nil {
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
