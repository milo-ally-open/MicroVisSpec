package main

import (
	"context"
	"crypto/elliptic"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type pushSender interface {
	Send(context.Context, webpush.Subscription, []byte, string) (*http.Response, error)
}

type App struct {
	config Config
	db     *gorm.DB
	box    *secretBox
	sender pushSender
}

func openDatabase(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, err
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS push").Error; err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Subscription{}, &Inbox{}, &SessionRevocation{}, &Delivery{}); err != nil {
		return nil, err
	}
	return db, nil
}

func NewApp(config Config, db *gorm.DB, sender pushSender) (*App, error) {
	box, err := newSecretBox(config.EncryptionKey)
	if err != nil {
		return nil, err
	}
	return &App{config: config, db: db, box: box, sender: sender}, nil
}

func (a *App) Handler() http.Handler {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(gin.Recovery(), a.requestContext(), a.accessLog())
	r.GET("/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "live"}) })
	r.GET("/health/ready", a.ready)
	r.GET("/api/v1/push/vapid-public-key", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"vapid_public_key": a.config.VAPIDPublicKey}) })

	api := r.Group("/api/v1/push", a.requireAccount())
	api.GET("/subscriptions", a.listSubscriptions)
	api.PUT("/subscriptions", a.upsertSubscription)
	api.DELETE("/subscriptions/:subscription_id", a.disableSubscription)

	internal := r.Group("/internal/v1")
	internal.POST("/inbox", a.requireInboxProducer(), a.receiveInbox)
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
		started := time.Now()
		c.Next()
		slog.Info("http_request", "request_id", c.GetString("request_id"), "method", c.Request.Method,
			"path", c.Request.URL.Path, "status", c.Writer.Status(), "duration_ms", time.Since(started).Milliseconds())
	}
}

func (a *App) ready(c *gin.Context) {
	sqlDB, err := a.db.DB()
	if err == nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		err = sqlDB.PingContext(ctx)
		cancel()
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "database is not ready")
		return
	}
	if a.sender == nil || a.config.VAPIDPublicKey == "" || a.config.VAPIDPrivateKey == "" {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "web push delivery is not configured")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "dependencies": gin.H{"postgres": "ready", "web_push": "ready"}})
}

func (a *App) requireAccount() gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, errAccount := uuid.Parse(c.GetHeader("X-Account-ID"))
		sessionID, errSession := uuid.Parse(c.GetHeader("X-Session-ID"))
		if errAccount != nil || errSession != nil || accountID == uuid.Nil || sessionID == uuid.Nil {
			writeError(c, http.StatusUnauthorized, "unauthorized", "a valid account session is required")
			c.Abort()
			return
		}
		c.Set("account_id", accountID)
		c.Set("session_id", sessionID)
		c.Next()
	}
}

func (a *App) requireInboxProducer() gin.HandlerFunc {
	return func(c *gin.Context) {
		producer := strings.TrimSpace(c.GetHeader("X-Internal-Service"))
		token := c.GetHeader("X-Internal-Service-Token")
		valid := producer == "auth-service" && constantTimeEqual(token, a.config.AuthServiceToken)
		valid = valid || producer == "event-service" && constantTimeEqual(token, a.config.EventServiceToken)
		if !valid {
			writeError(c, http.StatusUnauthorized, "unauthorized", "service authentication required")
			c.Abort()
			return
		}
		c.Set("inbox_producer", producer)
		c.Next()
	}
}

type subscriptionInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (a *App) upsertSubscription(c *gin.Context) {
	var input subscriptionInput
	if decodeJSON(c, &input, 16<<10) != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "invalid subscription request")
		return
	}
	if validatePushEndpoint(input.Endpoint) != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_endpoint", "push endpoint must be a valid HTTPS endpoint")
		return
	}
	p256dh, errP256 := base64.RawURLEncoding.DecodeString(input.Keys.P256dh)
	auth, errAuth := base64.RawURLEncoding.DecodeString(input.Keys.Auth)
	if errP256 != nil || len(p256dh) != 65 || p256dh[0] != 4 || errAuth != nil || len(auth) < 16 || len(auth) > 64 || !validP256dh(p256dh) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_subscription_keys", "subscription keys are invalid")
		return
	}
	p256dhEncrypted, err := a.box.seal(p256dh)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "subscription could not be saved")
		return
	}
	authEncrypted, err := a.box.seal(auth)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "subscription could not be saved")
		return
	}
	accountID, sessionID := accountID(c), sessionID(c)
	var subscription Subscription
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := lockSessionIdentity(tx, accountID, sessionID); err != nil {
			return err
		}
		var revoked SessionRevocation
		if err := tx.Table(SessionRevocation{}.TableName()).Where("account_id = ? AND session_id = ?", accountID, sessionID).Take(&revoked).Error; err == nil {
			return errSessionRevoked
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		err := tx.Table(Subscription{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).Where("endpoint = ?", input.Endpoint).Take(&subscription).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			now := time.Now().UTC()
			subscription = Subscription{ID: uuid.New(), AccountID: accountID, SourceSessionID: sessionID, Endpoint: input.Endpoint,
				P256dhEncrypted: p256dhEncrypted, AuthEncrypted: authEncrypted, CreatedAt: now, UpdatedAt: now}
			return tx.Table(Subscription{}.TableName()).Create(&subscription).Error
		}
		if err != nil {
			return err
		}
		if subscription.AccountID != accountID || subscription.SourceSessionID != sessionID {
			if err := tx.Table(Delivery{}.TableName()).Where("subscription_id = ? AND status = ?", subscription.ID, "pending").
				Updates(map[string]any{"status": "cancelled", "last_class": "subscription_rebound", "lease_id": nil, "lease_until": nil}).Error; err != nil {
				return err
			}
		}
		subscription.AccountID, subscription.SourceSessionID = accountID, sessionID
		subscription.P256dhEncrypted, subscription.AuthEncrypted = p256dhEncrypted, authEncrypted
		subscription.DisabledAt = nil
		subscription.UpdatedAt = time.Now().UTC()
		return tx.Table(Subscription{}.TableName()).Save(&subscription).Error
	})
	if err != nil {
		if errors.Is(err, errSessionRevoked) {
			writeError(c, http.StatusUnauthorized, "unauthorized", "a valid account session is required")
			return
		}
		if isUniqueViolation(err) {
			writeError(c, http.StatusConflict, "subscription_conflict", "subscription could not be rebound; retry the request")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "subscription could not be saved")
		return
	}
	slog.Info("push_subscription_saved", "request_id", c.GetString("request_id"), "account_id", accountID,
		"subscription_id", subscription.ID, "rebound", subscription.CreatedAt.Before(subscription.UpdatedAt))
	c.JSON(http.StatusOK, gin.H{"subscription_id": subscription.ID, "active": true})
}

func (a *App) listSubscriptions(c *gin.Context) {
	var subscriptions []Subscription
	err := a.db.WithContext(c.Request.Context()).Table(Subscription{}.TableName()).
		Where("account_id = ? AND source_session_id = ? AND disabled_at IS NULL", accountID(c), sessionID(c)).
		Order("created_at DESC").Find(&subscriptions).Error
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "subscriptions could not be loaded")
		return
	}
	items := make([]gin.H, 0, len(subscriptions))
	for _, sub := range subscriptions {
		items = append(items, gin.H{"subscription_id": sub.ID, "active": true, "created_at": sub.CreatedAt})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *App) disableSubscription(c *gin.Context) {
	id, err := uuid.Parse(c.Param("subscription_id"))
	if err != nil || id == uuid.Nil {
		c.Status(http.StatusNoContent)
		return
	}
	now := time.Now().UTC()
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Table(Subscription{}.TableName()).Where("id = ? AND account_id = ? AND disabled_at IS NULL", id, accountID(c)).
			Updates(map[string]any{"disabled_at": now, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		if err := tx.Table(Delivery{}.TableName()).Where("subscription_id = ? AND status = ?", id, "pending").
			Updates(map[string]any{"status": "cancelled", "last_class": "subscription_disabled", "lease_id": nil, "lease_until": nil}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "subscription could not be disabled")
		return
	}
	slog.Info("push_subscription_disabled", "request_id", c.GetString("request_id"), "account_id", accountID(c), "subscription_id", id)
	c.Status(http.StatusNoContent)
}

func accountID(c *gin.Context) uuid.UUID { return c.MustGet("account_id").(uuid.UUID) }
func sessionID(c *gin.Context) uuid.UUID { return c.MustGet("session_id").(uuid.UUID) }

func decodeJSON(c *gin.Context, target any, limit int64) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain a single JSON value")
	}
	return nil
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "request_id": c.GetString("request_id")})
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func validP256dh(key []byte) bool {
	if len(key) != 65 || key[0] != 4 {
		return false
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), key)
	return x != nil && y != nil
}

func isUniqueViolation(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "duplicate key")
}

var errSessionRevoked = errors.New("push session has been revoked")

func lockSessionIdentity(tx *gorm.DB, accountID, sessionID uuid.UUID) error {
	hash := fnv.New64a()
	_, _ = hash.Write(accountID[:])
	_, _ = hash.Write(sessionID[:])
	return tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(hash.Sum64())).Error
}
