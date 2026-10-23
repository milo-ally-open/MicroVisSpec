package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
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
	activationTicketLifetime           = 10 * time.Minute
	activationClaimLifetime            = 2 * time.Minute
	maxActivationDurationSeconds int64 = 9_223_372_036
	activationExpiryInterval           = 15 * time.Second
	activationExpiryBatchSize          = 100
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

func activationPassword(key string, deviceID, ticketID uuid.UUID) string {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte("yunshao-edge-activation-v1\x00" + deviceID.String() + "\x00" + ticketID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func writeActivationCredentialResponse(c *gin.Context, deviceID uuid.UUID, brokerURL, clientID, username, password string, activatedAt time.Time, expiresAt *time.Time) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(http.StatusOK, gin.H{
		"device_id": deviceID,
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
		"activated_at":          activatedAt,
		"activation_expires_at": expiresAt,
	})
}

func (a *App) createActivationTicket(c *gin.Context) {
	a.createActivationJob(c, true)
}

func (a *App) createActivationRequest(c *gin.Context) {
	a.createActivationJob(c, false)
}

func (a *App) createActivationJob(c *gin.Context, exposeTicket bool) {
	if a.broker == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MQTT management is not ready")
		return
	}
	device, ok := a.findOwnedActivatableDevice(c, c.Param("device_id"))
	if !ok {
		return
	}
	if !exposeTicket {
		var credential BrokerCredential
		if err := a.db.WithContext(c.Request.Context()).Where("device_id = ?", device.ID).Take(&credential).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeError(c, http.StatusConflict, "device_not_provisioned", "device has no trusted activation identity; an administrator must provision it once")
			} else {
				writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device activation identity could not be checked")
			}
			return
		}
		now := time.Now().UTC()
		deviceUsable := device.Active && device.ActivationState == "active" && (device.ActivationExpiresAt == nil || device.ActivationExpiresAt.After(now))
		if deviceUsable && credential.PreviousPasswordHash != "" && credential.RecoveryTicketID != nil {
			writeError(c, http.StatusConflict, "activation_sync_pending", "device is still synchronizing the previous activation")
			return
		}
	}
	var input struct {
		DurationSeconds *int64 `json:"duration_seconds"`
	}
	if decodeJSON(c, &input) != nil || input.DurationSeconds != nil && (*input.DurationSeconds < 1 || *input.DurationSeconds > maxActivationDurationSeconds) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "duration_seconds must be null or a positive value within the supported range")
		return
	}
	ticket, token, err := a.issueActivationTicket(c.Request.Context(), accountID(c), device.ID, input.DurationSeconds)
	if err != nil {
		if errors.Is(err, errActivationInProgress) {
			writeError(c, http.StatusConflict, "activation_in_progress", "device activation is already being processed")
			return
		}
		if errors.Is(err, errActivationRateLimited) {
			writeError(c, http.StatusTooManyRequests, "rate_limited", "too many activation tickets were requested")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "activation ticket could not be issued")
		return
	}
	if exposeTicket {
		c.JSON(http.StatusCreated, gin.H{"device_id": device.ID, "activation_ticket": token, "expires_at": ticket.ExpiresAt, "duration_seconds": ticket.UsageDurationSeconds})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"device_id": device.ID, "request_id": ticket.ID, "status": "pending", "expires_at": ticket.ExpiresAt, "duration_seconds": ticket.UsageDurationSeconds})
}

var errActivationRateLimited = errors.New("activation rate limited")

func (a *App) issueActivationTicket(ctx context.Context, account, deviceID uuid.UUID, duration *int64) (ActivationTicket, string, error) {
	if a.broker == nil {
		return ActivationTicket{}, "", errors.New("MQTT management is not ready")
	}
	now := time.Now().UTC()
	if !a.activationLimiter.allow("ticket:"+account.String()+":"+deviceID.String(), now) {
		return ActivationTicket{}, "", errActivationRateLimited
	}
	token, err := randomOpaque(32)
	if err != nil {
		return ActivationTicket{}, "", err
	}
	ticket := ActivationTicket{ID: uuid.New(), DeviceID: deviceID, TokenHash: hashOpaque(token), ExpiresAt: now.Add(activationTicketLifetime), UsageDurationSeconds: duration, CreatedAt: now}
	err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inFlight ActivationTicket
		inFlightErr := tx.Where("device_id = ? AND consumed_at IS NULL AND revoked_at IS NULL AND claim_expires_at > ?", deviceID, now).First(&inFlight).Error
		if inFlightErr == nil {
			return errActivationInProgress
		}
		if !errors.Is(inFlightErr, gorm.ErrRecordNotFound) {
			return inFlightErr
		}
		if err := tx.Model(&ActivationTicket{}).Where("device_id = ? AND consumed_at IS NULL AND revoked_at IS NULL", deviceID).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return tx.Create(&ticket).Error
	})
	if err != nil {
		return ActivationTicket{}, "", err
	}
	return ticket, token, nil
}

func (a *App) getActivationRequest(c *gin.Context) {
	device, ok := a.findOwnedActivatableDevice(c, c.Param("device_id"))
	if !ok {
		return
	}
	requestID, err := uuid.Parse(c.Param("request_id"))
	if err != nil || requestID == uuid.Nil {
		writeError(c, http.StatusNotFound, "activation_request_not_found", "activation request was not found")
		return
	}
	var request ActivationTicket
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND device_id = ?", requestID, device.ID).Take(&request).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "activation_request_not_found", "activation request was not found")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "activation request could not be read")
		}
		return
	}
	status := "pending"
	switch {
	case request.ConsumedAt != nil:
		status = "activated"
		var credential BrokerCredential
		credentialErr := a.db.WithContext(c.Request.Context()).Where("device_id = ?", device.ID).Take(&credential).Error
		if credentialErr != nil && !errors.Is(credentialErr, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device credential status could not be checked")
			return
		}
		if credentialErr == nil && credential.RecoveryTicketID != nil && *credential.RecoveryTicketID == request.ID && credential.PreviousPasswordHash != "" {
			status = "syncing"
		}
	case request.RevokedAt != nil:
		status = "superseded"
	case !request.ExpiresAt.After(time.Now().UTC()):
		status = "expired"
	}
	c.JSON(http.StatusOK, gin.H{"request_id": request.ID, "status": status, "expires_at": request.ExpiresAt,
		"activation_expires_at": device.ActivationExpiresAt})
}

func (a *App) activateDevice(c *gin.Context) {
	if a.broker == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MQTT management is not ready")
		return
	}
	if validateMQTTBrokerURL(a.config.AdvertisedBrokerURL) != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "edge MQTT endpoint is not configured")
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
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND (active = ? OR activation_state = ?)", deviceID, true, "expired").First(&device).Error; err != nil {
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

	a.completeActivation(c, ticket, claimID, device)
}

func (a *App) completeActivation(c *gin.Context, ticket ActivationTicket, claimID uuid.UUID, device Device) {
	if a.broker == nil || validateMQTTBrokerURL(a.config.AdvertisedBrokerURL) != nil {
		a.releaseTicketClaim(ticket.ID, claimID)
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "edge MQTT endpoint is not configured")
		return
	}
	username := brokerUsername(device.ID)
	clientID := brokerClientID(device.ID)
	// Derive the new password from the server-only broker admin secret and the
	// one-time request ID. A retry after a lost HTTP response can therefore
	// provision and return the same credential without storing it in the DB.
	password := activationPassword(a.config.BrokerAdminPassword, device.ID, ticket.ID)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	err := a.broker.Provision(ctx, device.ID, username, clientID, password)
	cancel()
	if err != nil {
		a.releaseTicketClaim(ticket.ID, claimID)
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "broker credentials could not be provisioned")
		return
	}
	completedAt := time.Now().UTC()
	var activationExpiresAt *time.Time
	if ticket.UsageDurationSeconds != nil {
		if *ticket.UsageDurationSeconds < 1 || *ticket.UsageDurationSeconds > maxActivationDurationSeconds {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = a.broker.Disable(ctx, device.ID, username)
			cancel()
			a.releaseTicketClaim(ticket.ID, claimID)
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device activation duration is invalid")
			return
		}
		expiresAt := completedAt.Add(time.Duration(*ticket.UsageDurationSeconds) * time.Second)
		activationExpiresAt = &expiresAt
	}
	credential := BrokerCredential{ID: uuid.New(), DeviceID: device.ID, Username: username, ClientID: clientID, PasswordHash: hashOpaque(password), CreatedAt: completedAt}
	err = a.db.Transaction(func(tx *gorm.DB) error {
		var current Device
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND (active = ? OR activation_state = ?)", device.ID, true, "expired").Take(&current).Error; err != nil {
			return errActivationInvalid
		}
		var previousCredential BrokerCredential
		previousErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id = ?", device.ID).Take(&previousCredential).Error
		if previousErr != nil && !errors.Is(previousErr, gorm.ErrRecordNotFound) {
			return previousErr
		}
		if previousErr == nil {
			credential.PreviousPasswordHash = previousCredential.PasswordHash
			// Preserve the device's last acknowledged password across multiple
			// renewals while it is offline. It can then recover the newest one.
			if previousCredential.PreviousPasswordHash != "" && previousCredential.RecoveryTicketID != nil {
				credential.PreviousPasswordHash = previousCredential.PreviousPasswordHash
			}
		}
		recoveryTicketID := ticket.ID
		credential.RecoveryTicketID = &recoveryTicketID
		result := tx.Model(&ActivationTicket{}).Where("id = ? AND claim_id = ? AND consumed_at IS NULL AND revoked_at IS NULL", ticket.ID, claimID).
			Updates(map[string]any{"consumed_at": completedAt, "claim_id": nil, "claim_expires_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errActivationInvalid
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "device_id"}}, DoUpdates: clause.AssignmentColumns([]string{"username", "client_id", "password_hash", "previous_password_hash", "recovery_ticket_id", "created_at", "revoked_at"})}).Create(&credential).Error; err != nil {
			return err
		}
		deviceUpdate := tx.Model(&Device{}).Where("id = ?", device.ID).Updates(map[string]any{
			"active": true, "activation_state": "active", "activation_expires_at": activationExpiresAt, "disabled_at": nil, "updated_at": completedAt,
		})
		if deviceUpdate.Error != nil {
			return deviceUpdate.Error
		}
		if deviceUpdate.RowsAffected != 1 {
			return errActivationInvalid
		}
		if current.ActivationState == "expired" || current.ActivationState == "pending" {
			payload, err := json.Marshal(map[string]string{"account_id": current.AccountID.String(), "device_id": current.ID.String()})
			if err != nil {
				return err
			}
			if err := tx.Create(&Outbox{ID: uuid.New(), EventType: "device.activated", Payload: payload, CreatedAt: completedAt, NextAttemptAt: completedAt}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = a.broker.Disable(ctx, device.ID, username)
		cancel()
		a.releaseTicketClaim(ticket.ID, claimID)
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device activation could not be committed")
		return
	}
	writeActivationCredentialResponse(c, device.ID, a.config.AdvertisedBrokerURL, clientID, username, password, completedAt, activationExpiresAt)
}

func (a *App) pollDeviceActivation(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("device_id"))
	if err != nil || deviceID == uuid.Nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
		return
	}
	username, password, ok := c.Request.BasicAuth()
	if !ok || username == "" || len(password) < 32 || len(password) > 256 {
		writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
		return
	}
	var device Device
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND (active = ? OR activation_state = ?)", deviceID, true, "expired").Take(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device activation identity could not be checked")
		}
		return
	}
	var credential BrokerCredential
	if err := a.db.WithContext(c.Request.Context()).Where("device_id = ?", deviceID).Take(&credential).Error; err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
		return
	}
	presentedHash := hashOpaque(password)
	currentCredential := constantTimeEqual(presentedHash, credential.PasswordHash)
	recoveryCredential := credential.RecoveryTicketID != nil && credential.PreviousPasswordHash != "" && constantTimeEqual(presentedHash, credential.PreviousPasswordHash)
	if !constantTimeEqual(username, credential.Username) || (!currentCredential && !recoveryCredential) {
		writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
		return
	}
	if !a.activationLimiter.allow("activation-poll:"+deviceID.String(), time.Now().UTC()) {
		writeError(c, http.StatusTooManyRequests, "rate_limited", "activation polling is too frequent")
		return
	}

	now := time.Now().UTC()
	deviceUsable := device.Active && device.ActivationState == "active" && (device.ActivationExpiresAt == nil || device.ActivationExpiresAt.After(now))
	if recoveryCredential && deviceUsable {
		password := activationPassword(a.config.BrokerAdminPassword, device.ID, *credential.RecoveryTicketID)
		writeActivationCredentialResponse(c, device.ID, a.config.AdvertisedBrokerURL, credential.ClientID, credential.Username,
			password, credential.CreatedAt, device.ActivationExpiresAt)
		return
	}
	if currentCredential && credential.RecoveryTicketID != nil && credential.PreviousPasswordHash != "" {
		if err := a.db.WithContext(c.Request.Context()).Model(&BrokerCredential{}).
			Where("device_id = ? AND password_hash = ?", deviceID, credential.PasswordHash).
			Updates(map[string]any{"previous_password_hash": "", "recovery_ticket_id": nil}).Error; err != nil {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device credential acknowledgement could not be saved")
			return
		}
		credential.PreviousPasswordHash = ""
		credential.RecoveryTicketID = nil
	}
	claimID := uuid.New()
	claimUntil := now.Add(activationClaimLifetime)
	var ticket ActivationTicket
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id = ? AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at > ?", deviceID, now).
			Order("created_at DESC").Limit(1).Take(&ticket)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if query.Error != nil {
			return query.Error
		}
		if ticket.ClaimID != nil && ticket.ClaimExpiresAt != nil && ticket.ClaimExpiresAt.After(now) {
			return errActivationInProgress
		}
		result := tx.Model(&ActivationTicket{}).Where("id = ? AND consumed_at IS NULL AND revoked_at IS NULL", ticket.ID).
			Updates(map[string]any{"claim_id": claimID, "claim_expires_at": claimUntil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errActivationInvalid
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && ticket.ID == uuid.Nil {
		if device.Active && device.ActivationState == "active" && (device.ActivationExpiresAt == nil || device.ActivationExpiresAt.After(now)) {
			c.JSON(http.StatusOK, gin.H{"status": "active", "activation_expires_at": device.ActivationExpiresAt})
			return
		}
		c.Status(http.StatusNoContent)
		return
	}
	if errors.Is(err, errActivationInProgress) {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "activation request could not be claimed")
		return
	}
	a.completeActivation(c, ticket, claimID, device)
}

func (a *App) activationExpiryLoop(ctx context.Context) {
	a.expireDevices(ctx, time.Now().UTC())
	ticker := time.NewTicker(activationExpiryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.expireDevices(ctx, time.Now().UTC())
		}
	}
}

func (a *App) expireDevices(ctx context.Context, now time.Time) {
	for ctx.Err() == nil {
		var expiredIDs []uuid.UUID
		err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var devices []Device
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("active = ? AND activation_state = ? AND activation_expires_at IS NOT NULL AND activation_expires_at <= ?", true, "active", now).
				Order("activation_expires_at ASC").Limit(activationExpiryBatchSize).Find(&devices).Error; err != nil {
				return err
			}
			for _, device := range devices {
				result := tx.Model(&Device{}).Where("id = ? AND active = ? AND activation_state = ? AND activation_expires_at <= ?", device.ID, true, "active", now).Updates(map[string]any{
					"active": false, "activation_state": "expired", "disabled_at": now, "updated_at": now,
				})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					continue
				}
				payload, _ := json.Marshal(map[string]string{"account_id": device.AccountID.String(), "device_id": device.ID.String()})
				if err := tx.Create(&Outbox{ID: uuid.New(), EventType: "device.disabled", Payload: payload, CreatedAt: now, NextAttemptAt: now}).Error; err != nil {
					return err
				}
				expiredIDs = append(expiredIDs, device.ID)
			}
			return nil
		})
		if err != nil {
			slog.Error("device_activation_expiry_failed", "error", err.Error())
			return
		}
		for _, deviceID := range expiredIDs {
			if a.broker == nil {
				continue
			}
			callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := a.revokeDeviceCredentials(callCtx, deviceID)
			cancel()
			if err != nil {
				slog.Error("expired_device_broker_revocation_failed", "device_id", deviceID, "error", err.Error())
			}
		}
		if len(expiredIDs) < activationExpiryBatchSize {
			return
		}
	}
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
