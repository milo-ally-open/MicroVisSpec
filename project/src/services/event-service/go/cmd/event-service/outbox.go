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
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	eventOutboxLeaseDuration = 30 * time.Second
	eventOutboxPollInterval  = time.Second
)

type pushInboxEnvelope struct {
	Producer      string          `json:"producer"`
	EventType     string          `json:"event_type"`
	EventID       uuid.UUID       `json:"event_id"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

type pushOutboxPayload struct {
	EventID    uuid.UUID `json:"event_id"`
	DeviceID   uuid.UUID `json:"device_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

func (a *App) startOutboxWorker(ctx context.Context) {
	if a.config.PushServiceURL == "" || a.config.PushServiceToken == "" {
		slog.Error("event_outbox_worker_disabled", "reason", "push service delivery is not configured")
		return
	}
	go a.outboxLoop(ctx)
}

func (a *App) outboxLoop(ctx context.Context) {
	ticker := time.NewTicker(eventOutboxPollInterval)
	defer ticker.Stop()
	for {
		item, err := a.claimOutbox(ctx)
		if err != nil {
			slog.Error("event_outbox_claim_failed", "error", err.Error())
		} else if item != nil {
			err = a.deliverOutbox(ctx, *item)
			if err != nil {
				if updateErr := a.retryOutbox(ctx, *item, err); updateErr != nil {
					slog.Error("event_outbox_retry_schedule_failed", "event_id", item.ID, "error", updateErr.Error())
				}
				if item.Attempts >= 10 {
					slog.Error("event_outbox_delivery_repeated_failure", "event_id", item.ID, "attempts", item.Attempts, "error", err.Error())
				} else {
					slog.Warn("event_outbox_delivery_failed", "event_id", item.ID, "attempts", item.Attempts, "error", err.Error())
				}
			} else if ackErr := a.ackOutbox(ctx, *item); ackErr != nil {
				slog.Error("event_outbox_ack_failed", "event_id", item.ID, "error", ackErr.Error())
			} else {
				slog.Info("event_outbox_delivered", "event_id", item.ID, "event_type", item.EventType)
			}
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
	now := time.Now().UTC()
	leaseID := uuid.New()
	leaseUntil := now.Add(eventOutboxLeaseDuration)
	var item Outbox
	found := false
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(Outbox{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("delivered_at IS NULL AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until <= ?)", now, now).
			Order("next_attempt_at ASC, created_at ASC").Limit(1).Take(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		found = true
		result := tx.Table(Outbox{}.TableName()).Where("id = ? AND delivered_at IS NULL", item.ID).Updates(map[string]any{
			"lease_id": leaseID, "lease_until": leaseUntil, "attempts": gorm.Expr("attempts + 1"), "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("event outbox lease was not acquired")
		}
		item.LeaseID, item.LeaseUntil = &leaseID, &leaseUntil
		item.Attempts++
		return nil
	})
	if err != nil || !found {
		return nil, err
	}
	return &item, nil
}

func (a *App) deliverOutbox(ctx context.Context, item Outbox) error {
	var payload pushOutboxPayload
	if err := json.Unmarshal(item.Payload, &payload); err != nil || payload.EventID != item.ID || payload.DeviceID == uuid.Nil || payload.OccurredAt.IsZero() {
		return errors.New("event.created outbox payload is invalid")
	}
	envelope := pushInboxEnvelope{Producer: "event-service", EventType: item.EventType, EventID: item.ID,
		SchemaVersion: 1, OccurredAt: payload.OccurredAt.UTC(), Payload: item.Payload}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode event.created envelope: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.PushServiceURL+"/internal/v1/inbox", bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create push inbox request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-Service", "event-service")
	request.Header.Set("X-Internal-Service-Token", a.config.PushServiceToken)
	request.Header.Set("X-Request-ID", item.ID.String())
	response, err := a.pushHTTP.Do(request)
	if err != nil {
		return fmt.Errorf("send event to push-service: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("push-service inbox returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (a *App) ackOutbox(ctx context.Context, item Outbox) error {
	now := time.Now().UTC()
	return a.db.WithContext(ctx).Table(Outbox{}.TableName()).Where("id = ? AND lease_id = ? AND delivered_at IS NULL", item.ID, item.LeaseID).
		Updates(map[string]any{"delivered_at": now, "last_error": "", "lease_id": nil, "lease_until": nil, "updated_at": now}).Error
}

func (a *App) retryOutbox(ctx context.Context, item Outbox, deliveryErr error) error {
	shift := item.Attempts - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 8 {
		shift = 8
	}
	delay := time.Second * time.Duration(1<<shift)
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	now := time.Now().UTC()
	return a.db.WithContext(ctx).Table(Outbox{}.TableName()).Where("id = ? AND lease_id = ? AND delivered_at IS NULL", item.ID, item.LeaseID).
		Updates(map[string]any{"next_attempt_at": now.Add(delay), "last_error": truncateString(deliveryErr.Error(), 500),
			"lease_id": nil, "lease_until": nil, "updated_at": now}).Error
}

func truncateString(value string, max int) string {
	if len(value) > max {
		return value[:max]
	}
	return value
}
