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
	authOutboxLeaseDuration = 30 * time.Second
	authOutboxPollInterval  = time.Second
	authOutboxBatchSize     = 10
)

type authInboxEnvelope struct {
	Producer      string          `json:"producer"`
	EventType     string          `json:"event_type"`
	EventID       uuid.UUID       `json:"event_id"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

type authInboxTarget struct {
	service string
	url     string
	token   string
}

type authOutboxHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func runOutboxWorker(ctx context.Context, db *gorm.DB, config Config, client authOutboxHTTPDoer) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	ticker := time.NewTicker(authOutboxPollInterval)
	defer ticker.Stop()
	for {
		processed := false
		for count := 0; count < authOutboxBatchSize; count++ {
			if ctx.Err() != nil {
				return
			}
			item, err := claimAuthOutbox(ctx, db)
			if err != nil {
				slog.Error("auth_outbox_claim_failed", "error", err.Error())
				break
			}
			if item == nil {
				break
			}
			processed = true
			if err := deliverAuthOutbox(ctx, *item, config, client); err != nil {
				if updateErr := retryAuthOutbox(ctx, db, *item, err); updateErr != nil {
					slog.Error("auth_outbox_retry_schedule_failed", "event_id", item.ID, "error", updateErr.Error())
				}
				slog.Error("auth_outbox_delivery_failed", "event_id", item.ID, "event_type", item.EventType,
					"attempt", item.Attempts, "error", err.Error())
				continue
			}
			if err := acknowledgeAuthOutbox(ctx, db, *item); err != nil {
				slog.Error("auth_outbox_acknowledgement_failed", "event_id", item.ID, "error", err.Error())
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

func claimAuthOutbox(ctx context.Context, db *gorm.DB) (*Outbox, error) {
	var item Outbox
	found := false
	now := time.Now().UTC()
	leaseID := uuid.New()
	leaseUntil := now.Add(authOutboxLeaseDuration)
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Table(Outbox{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("delivered_at IS NULL AND next_attempt_at <= ? AND (lease_until IS NULL OR lease_until <= ?)", now, now).
			Order("created_at ASC").Limit(1).Take(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		result := tx.Table(Outbox{}.TableName()).Where("id = ? AND delivered_at IS NULL", item.ID).Updates(map[string]any{
			"lease_id": leaseID, "lease_until": leaseUntil, "attempts": gorm.Expr("attempts + 1"),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("outbox item lease was not acquired")
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

func deliverAuthOutbox(ctx context.Context, item Outbox, config Config, client authOutboxHTTPDoer) error {
	if item.EventType != "session.revoked" {
		return fmt.Errorf("unsupported auth outbox event type %q", item.EventType)
	}
	envelope, err := authEnvelope(item)
	if err != nil {
		return err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode internal event: %w", err)
	}
	targets := []authInboxTarget{
		{service: "media-service", url: config.MediaServiceURL, token: config.MediaServiceToken},
		{service: "push-service", url: config.PushServiceURL, token: config.PushServiceToken},
	}
	for _, target := range targets {
		if target.url == "" || target.token == "" {
			return fmt.Errorf("%s inbox is not configured", target.service)
		}
		endpoint := strings.TrimRight(target.url, "/") + "/internal/v1/inbox"
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create %s inbox request: %w", target.service, err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Internal-Service-Token", target.token)
		request.Header.Set("X-Internal-Service", "auth-service")
		request.Header.Set("X-Request-ID", item.ID.String())
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("send session.revoked to %s: %w", target.service, err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("%s inbox returned HTTP %d", target.service, response.StatusCode)
		}
	}
	return nil
}

func authEnvelope(item Outbox) (authInboxEnvelope, error) {
	var stored struct {
		AccountID  string `json:"account_id"`
		SessionID  string `json:"session_id"`
		OccurredAt string `json:"occurred_at"`
	}
	if err := json.Unmarshal(item.Payload, &stored); err != nil {
		return authInboxEnvelope{}, fmt.Errorf("decode session.revoked payload: %w", err)
	}
	if _, err := uuid.Parse(stored.AccountID); err != nil {
		return authInboxEnvelope{}, errors.New("session.revoked account_id is invalid")
	}
	if _, err := uuid.Parse(stored.SessionID); err != nil {
		return authInboxEnvelope{}, errors.New("session.revoked session_id is invalid")
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, stored.OccurredAt)
	if err != nil || occurredAt.IsZero() {
		return authInboxEnvelope{}, errors.New("session.revoked occurred_at is invalid")
	}
	payload, err := json.Marshal(struct {
		AccountID string `json:"account_id"`
		SessionID string `json:"session_id"`
	}{AccountID: stored.AccountID, SessionID: stored.SessionID})
	if err != nil {
		return authInboxEnvelope{}, fmt.Errorf("encode session.revoked payload: %w", err)
	}
	return authInboxEnvelope{Producer: "auth-service", EventType: item.EventType, EventID: item.ID,
		SchemaVersion: 1, OccurredAt: occurredAt.UTC(), Payload: payload}, nil
}

func acknowledgeAuthOutbox(ctx context.Context, db *gorm.DB, item Outbox) error {
	now := time.Now().UTC()
	result := db.WithContext(ctx).Table(Outbox{}.TableName()).
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

func retryAuthOutbox(ctx context.Context, db *gorm.DB, item Outbox, deliveryErr error) error {
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
	result := db.WithContext(ctx).Table(Outbox{}.TableName()).
		Where("id = ? AND lease_id = ? AND delivered_at IS NULL", item.ID, item.LeaseID).
		Updates(map[string]any{"next_attempt_at": time.Now().UTC().Add(delay), "last_error": message, "lease_id": nil, "lease_until": nil})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("outbox retry scheduling lost its lease")
	}
	return nil
}
