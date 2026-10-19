package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	activationTicketLifetime = 10 * time.Minute
	activationClaimLifetime  = 2 * time.Minute
)

type windowLimiter struct {
	mu      sync.Mutex
	limit   int
	period  time.Duration
	buckets map[string]windowBucket
}

type windowBucket struct {
	start time.Time
	count int
}

func newWindowLimiter(limit int) *windowLimiter {
	if limit < 1 {
		limit = 10
	}
	return &windowLimiter{limit: limit, period: time.Minute, buckets: make(map[string]windowBucket)}
}

func (l *windowLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.buckets[key]
	if !ok || now.Sub(current.start) >= l.period {
		l.buckets[key] = windowBucket{start: now, count: 1}
		return true
	}
	if current.count >= l.limit {
		return false
	}
	current.count++
	l.buckets[key] = current
	return true
}

func randomOpaque(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashOpaque(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func (a *App) createActivationTicket(c *gin.Context) {
	if a.broker == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MQTT management is not ready")
		return
	}
	device, ok := a.findOwnedDevice(c, c.Param("device_id"), true)
	if !ok {
		return
	}
	now := time.Now().UTC()
	if !a.activationLimiter.allow("ticket:"+accountID(c).String()+":"+device.ID.String(), now) {
		writeError(c, http.StatusTooManyRequests, "rate_limited", "too many activation tickets were requested")
		return
	}
	token, err := randomOpaque(32)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "activation ticket could not be issued")
		return
	}
	ticket := ActivationTicket{ID: uuid.New(), DeviceID: device.ID, TokenHash: hashOpaque(token), ExpiresAt: now.Add(activationTicketLifetime), CreatedAt: now}
	err = a.db.Transaction(func(tx *gorm.DB) error {
		var inFlight ActivationTicket
		inFlightErr := tx.Where("device_id = ? AND consumed_at IS NULL AND revoked_at IS NULL AND claim_expires_at > ?", device.ID, now).First(&inFlight).Error
		if inFlightErr == nil {
			return errActivationInProgress
		}
		if !errors.Is(inFlightErr, gorm.ErrRecordNotFound) {
			return inFlightErr
		}
		if err := tx.Model(&ActivationTicket{}).Where("device_id = ? AND consumed_at IS NULL AND revoked_at IS NULL", device.ID).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return tx.Create(&ticket).Error
	})
	if err != nil {
		if errors.Is(err, errActivationInProgress) {
			writeError(c, http.StatusConflict, "activation_in_progress", "device activation is already being processed")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "activation ticket could not be issued")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"device_id": device.ID, "activation_ticket": token, "expires_at": ticket.ExpiresAt})
}

func (a *App) activateDevice(c *gin.Context) {
	if a.broker == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MQTT management is not ready")
		return
	}
	var input struct {
		DeviceID string `json:"device_id"`
		Ticket   string `json:"ticket"`
	}
	if decodeJSON(c, &input) != nil || len(input.Ticket) < 32 || len(input.Ticket) > 128 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "device_id and activation ticket are required")
		return
	}
	deviceID, err := uuid.Parse(input.DeviceID)
	if err != nil || deviceID == uuid.Nil {
		writeError(c, http.StatusUnauthorized, "activation_rejected", "activation ticket is invalid")
		return
	}
	now := time.Now().UTC()
	if !a.activationLimiter.allow("activate:"+c.ClientIP()+":"+deviceID.String(), now) {
		writeError(c, http.StatusTooManyRequests, "rate_limited", "too many activation attempts")
		return
	}
	claimID := uuid.New()
	var ticket ActivationTicket
	var device Device
	claimUntil := now.Add(activationClaimLifetime)
	err = a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id = ? AND token_hash = ?", deviceID, hashOpaque(input.Ticket)).First(&ticket).Error; err != nil {
			return errActivationInvalid
		}
		if ticket.RevokedAt != nil || ticket.ConsumedAt != nil {
			return errActivationInvalid
		}
		if !ticket.ExpiresAt.After(now) {
			return errActivationExpired
		}
		if ticket.ClaimID != nil && ticket.ClaimExpiresAt != nil && ticket.ClaimExpiresAt.After(now) {
			return errActivationInProgress
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND active = ?", deviceID, true).First(&device).Error; err != nil {
			return errActivationInvalid
		}
		return tx.Model(&ActivationTicket{}).Where("id = ?", ticket.ID).Updates(map[string]any{"claim_id": claimID, "claim_expires_at": claimUntil}).Error
	})
	if err != nil {
		switch {
		case errors.Is(err, errActivationExpired):
			writeError(c, http.StatusGone, "activation_ticket_expired", "activation ticket has expired")
		case errors.Is(err, errActivationInProgress):
			writeError(c, http.StatusConflict, "activation_in_progress", "activation is already being processed")
		default:
			writeError(c, http.StatusUnauthorized, "activation_rejected", "activation ticket is invalid or has already been used")
		}
		return
	}

	username := brokerUsername(device.ID)
	clientID := brokerClientID(device.ID)
	password, err := randomOpaque(32)
	if err == nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		err = a.broker.Provision(ctx, device.ID, username, clientID, password)
		cancel()
	}
	if err != nil {
		a.releaseTicketClaim(ticket.ID, claimID)
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "broker credentials could not be provisioned")
		return
	}
	completedAt := time.Now().UTC()
	credential := BrokerCredential{ID: uuid.New(), DeviceID: device.ID, Username: username, ClientID: clientID, PasswordHash: hashOpaque(password), CreatedAt: completedAt}
	err = a.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&ActivationTicket{}).Where("id = ? AND claim_id = ? AND consumed_at IS NULL AND revoked_at IS NULL", ticket.ID, claimID).
			Updates(map[string]any{"consumed_at": completedAt, "claim_id": nil, "claim_expires_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errActivationInvalid
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}}, DoUpdates: clause.AssignmentColumns([]string{"username", "client_id", "password_hash", "created_at", "revoked_at"})}).Create(&credential).Error; err != nil {
			return err
		}
		return tx.Model(&Device{}).Where("id = ? AND active = ?", device.ID, true).Updates(map[string]any{"activation_state": "active", "updated_at": completedAt}).Error
	})
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = a.broker.Disable(ctx, device.ID, username)
		cancel()
		a.releaseTicketClaim(ticket.ID, claimID)
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device activation could not be committed")
		return
	}
	brokerURL := a.config.BrokerURL
	if brokerURL == "" {
		// Tests inject a fake broker and can set the URL explicitly when
		// verifying the response contract.
		brokerURL = "ssl://mqtt.invalid:8883"
	}
	c.JSON(http.StatusOK, gin.H{
		"device_id": device.ID,
		"mqtt": gin.H{
			"broker_url":         brokerURL,
			"protocol_version":   "3.1.1",
			"client_id":          clientID,
			"username":           username,
			"password":           password,
			"keepalive_seconds":  30,
			"persistent_session": true,
			"tls_required":       true,
		},
		"activated_at": completedAt,
	})
}

func (a *App) releaseTicketClaim(ticketID, claimID uuid.UUID) {
	_ = a.db.Model(&ActivationTicket{}).Where("id = ? AND claim_id = ? AND consumed_at IS NULL", ticketID, claimID).
		Updates(map[string]any{"claim_id": nil, "claim_expires_at": nil}).Error
}

var (
	errActivationInvalid    = errors.New("activation ticket invalid")
	errActivationExpired    = errors.New("activation ticket expired")
	errActivationInProgress = errors.New("activation already in progress")
)

func brokerUsername(deviceID uuid.UUID) string {
	return "yunshao-device-" + strings.ReplaceAll(deviceID.String(), "-", "")
}
func brokerClientID(deviceID uuid.UUID) string {
	return "yunshao-edge-" + strings.ReplaceAll(deviceID.String(), "-", "")
}

func marshalJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
