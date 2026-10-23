package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	outboxLeaseDuration = 30 * time.Second
	outboxPollInterval  = time.Second
	outboxBatchSize     = 10
)

type internalInboxEnvelope struct {
	Producer      string          `json:"producer"`
	EventType     string          `json:"event_type"`
	EventID       uuid.UUID       `json:"event_id"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

func (a *App) outboxLoop(ctx context.Context) {
	if a.config.MediaServiceURL == "" || a.config.MediaServiceToken == "" {
		slog.Warn("device_outbox_delivery_disabled", "reason", "media service internal endpoint is not configured")
		return
	}
	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()
	for {
		processed := false
		for count := 0; count < outboxBatchSize; count++ {
			if ctx.Err() != nil {
				return
			}
			item, err := a.claimOutbox(ctx)
			if err != nil {
				slog.Error("device_outbox_claim_failed", "error", err.Error())
				break
			}
			if item == nil {
				break
			}
			processed = true
			if err := a.deliverOutbox(ctx, *item); err != nil {
				if updateErr := a.retryOutbox(ctx, *item, err); updateErr != nil {
					slog.Error("device_outbox_retry_schedule_failed", "event_id", item.ID, "error", updateErr.Error())
				}
				slog.Error("device_outbox_delivery_failed", "event_id", item.ID, "event_type", item.EventType, "attempt", item.Attempts, "error", err.Error())
				continue
			}
			if err := a.acknowledgeOutbox(ctx, *item); err != nil {
				slog.Error("device_outbox_acknowledgement_failed", "event_id", item.ID, "error", err.Error())
			}
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) claimOutbox(ctx context.Context) (*Outbox, error) {
	var item Outbox
	found := false
	now := time.Now().UTC()
	leaseID := uuid.New()
	leaseUntil := now.Add(outboxLeaseDuration)
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("delivered_at IS NULL AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until <= ?)", now, now).
			Order("created_at ASC").Limit(1).Take(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		result := tx.Model(&Outbox{}).Where("id = ? AND delivered_at IS NULL", item.ID).Updates(map[string]any{
			"lease_id": leaseID, "lease_until": leaseUntil, "attempts": gorm.Expr("attempts + 1"),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("outbox item lease was not acquired")
		}
		item.LeaseID = &leaseID
		item.LeaseUntil = &leaseUntil
		item.Attempts++
		return nil
	})
	if err != nil || !found {
		return nil, err
	}
	return &item, nil
}

func (a *App) deliverOutbox(ctx context.Context, item Outbox) error {
	envelope := internalInboxEnvelope{
		Producer: "device-service", EventType: item.EventType, EventID: item.ID,
		SchemaVersion: 1, OccurredAt: item.CreatedAt.UTC(), Payload: item.Payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode internal event: %w", err)
	}
	endpoint := strings.TrimRight(a.config.MediaServiceURL, "/") + "/internal/v1/inbox"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create internal delivery request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-Service-Token", a.config.MediaServiceToken)
	request.Header.Set("X-Internal-Service", "device-service")
	request.Header.Set("X-Request-ID", item.ID.String())
	client := a.outboxHTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send internal delivery request: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("internal inbox returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (a *App) acknowledgeOutbox(ctx context.Context, item Outbox) error {
	now := time.Now().UTC()
	result := a.db.WithContext(ctx).Model(&Outbox{}).
		Where("id = ? AND lease_id = ? AND delivered_at IS NULL", item.ID, item.LeaseID).
		Updates(map[string]any{"delivered_at": now, "lease_id": nil, "lease_until": nil, "last_error": ""})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("outbox acknowledgement lost its lease")
	}
	return nil
}

func (a *App) retryOutbox(ctx context.Context, item Outbox, deliveryErr error) error {
	attempt := item.Attempts
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	delay := time.Second * time.Duration(1<<shift)
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	message := deliveryErr.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	result := a.db.WithContext(ctx).Model(&Outbox{}).
		Where("id = ? AND lease_id = ? AND delivered_at IS NULL", item.ID, item.LeaseID).
		Updates(map[string]any{"next_attempt_at": time.Now().UTC().Add(delay), "last_error": message, "lease_id": nil, "lease_until": nil})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("outbox retry schedule lost its lease")
	}
	return nil
}

func (a *App) revokeDeviceCredentials(ctx context.Context, deviceID uuid.UUID) error {
	if a.broker == nil {
		return nil
	}
	var credential BrokerCredential
	err := a.db.WithContext(ctx).Where("device_id = ? AND revoked_at IS NULL", deviceID).Take(&credential).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := a.broker.Disable(ctx, deviceID, credential.Username); err != nil {
		return err
	}
	now := time.Now().UTC()
	return a.db.WithContext(ctx).Model(&BrokerCredential{}).
		Where("id = ? AND revoked_at IS NULL", credential.ID).Update("revoked_at", now).Error
}

func (a *App) brokerRevocationLoop(ctx context.Context) {
	if a.broker == nil {
		return
	}
	a.retryBrokerRevocations(ctx)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.retryBrokerRevocations(ctx)
		}
	}
}

func (a *App) retryBrokerRevocations(ctx context.Context) {
	var credentials []BrokerCredential
	err := a.db.WithContext(ctx).Table("device.broker_credentials AS credentials").
		Select("credentials.*").
		Joins("JOIN device.devices AS devices ON devices.id = credentials.device_id").
		Where("credentials.revoked_at IS NULL AND (devices.active = ? OR (devices.activation_expires_at IS NOT NULL AND devices.activation_expires_at <= ?))", false, time.Now().UTC()).
		Order("credentials.created_at ASC").Limit(100).Scan(&credentials).Error
	if err != nil {
		slog.Error("broker_revocation_reconciliation_failed", "error", err.Error())
		return
	}
	for _, credential := range credentials {
		if ctx.Err() != nil {
			return
		}
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := a.revokeDeviceCredentials(callCtx, credential.DeviceID)
		cancel()
		if err != nil {
			slog.Error("broker_credential_revocation_retry_failed", "device_id", credential.DeviceID, "error", err.Error())
		}
	}
}
