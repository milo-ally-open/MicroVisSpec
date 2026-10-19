package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type internalInboxEnvelope struct {
	Producer      string          `json:"producer"`
	EventType     string          `json:"event_type"`
	EventID       uuid.UUID       `json:"event_id"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

type createdEventPayload struct {
	AccountID  uuid.UUID `json:"account_id"`
	EventID    uuid.UUID `json:"event_id"`
	Location   string    `json:"location"`
	Severity   string    `json:"severity"`
	OccurredAt time.Time `json:"occurred_at"`
	DetailPath string    `json:"detail_path"`
}

type revokedSessionPayload struct {
	AccountID uuid.UUID `json:"account_id"`
	SessionID uuid.UUID `json:"session_id"`
}

type notificationPayload struct {
	EventID    uuid.UUID `json:"event_id"`
	Location   string    `json:"location"`
	Severity   string    `json:"severity"`
	OccurredAt time.Time `json:"occurred_at"`
	DetailPath string    `json:"detail_path"`
}

func (a *App) receiveInbox(c *gin.Context) {
	var envelope internalInboxEnvelope
	if decodeJSON(c, &envelope, 16<<10) != nil || envelope.EventID == uuid.Nil || envelope.SchemaVersion != 1 || envelope.Payload == nil || envelope.OccurredAt.IsZero() {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "internal event envelope is invalid")
		return
	}
	producer := c.GetString("inbox_producer")
	if envelope.Producer != producer || !validProducerEvent(producer, envelope.EventType) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "internal event producer or type is invalid")
		return
	}
	var eventPayload createdEventPayload
	var sessionPayload revokedSessionPayload
	switch envelope.EventType {
	case "event.created":
		if decodeStrict(envelope.Payload, &eventPayload) != nil || eventPayload.AccountID == uuid.Nil || eventPayload.EventID != envelope.EventID ||
			eventPayload.OccurredAt.IsZero() || !eventPayload.OccurredAt.Equal(envelope.OccurredAt) || len(eventPayload.Location) > 256 ||
			!validEventSeverity(eventPayload.Severity) || eventPayload.DetailPath != "/events/"+envelope.EventID.String() {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "event.created payload is invalid")
			return
		}
	case "session.revoked":
		if decodeStrict(envelope.Payload, &sessionPayload) != nil || sessionPayload.AccountID == uuid.Nil || sessionPayload.SessionID == uuid.Nil {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "session.revoked payload is invalid")
			return
		}
	}

	duplicate := false
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		inbox := Inbox{ID: uuid.New(), Producer: producer, EventID: envelope.EventID, EventType: envelope.EventType,
			SchemaVersion: envelope.SchemaVersion, OccurredAt: envelope.OccurredAt.UTC(), Payload: envelope.Payload,
			ReceivedAt: now}
		result := tx.Table(Inbox{}.TableName()).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "producer"}, {Name: "event_id"}, {Name: "event_type"}}, DoNothing: true,
		}).Create(&inbox)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			duplicate = true
			return nil
		}
		switch envelope.EventType {
		case "event.created":
			if err := a.createDeliveries(tx, envelope.EventID, eventPayload, now); err != nil {
				return err
			}
		case "session.revoked":
			if err := a.revokeSessionSubscriptions(tx, sessionPayload, now); err != nil {
				return err
			}
		}
		return tx.Table(Inbox{}.TableName()).Where("producer = ? AND event_id = ? AND event_type = ?", producer, envelope.EventID, envelope.EventType).
			Update("processed_at", now).Error
	})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "internal event could not be persisted")
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted", "duplicate": duplicate})
}

func (a *App) createDeliveries(tx *gorm.DB, eventID uuid.UUID, event createdEventPayload, now time.Time) error {
	var subscriptions []Subscription
	if err := tx.Table(Subscription{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("account_id = ? AND disabled_at IS NULL", event.AccountID).Order("created_at ASC").Find(&subscriptions).Error; err != nil {
		return err
	}
	notice, err := json.Marshal(notificationPayload{EventID: eventID, Location: event.Location, Severity: event.Severity,
		OccurredAt: event.OccurredAt.UTC(), DetailPath: event.DetailPath})
	if err != nil {
		return err
	}
	expiresAt := event.OccurredAt.Add(a.config.MaxEventAge)
	for _, subscription := range subscriptions {
		status := "pending"
		lastClass := ""
		if !expiresAt.After(now) {
			status = "expired"
			lastClass = "event_too_old"
		}
		delivery := Delivery{ID: uuid.New(), EventID: eventID, SubscriptionID: subscription.ID, AccountID: event.AccountID,
			Payload: notice, Status: status, NextAttemptAt: now, ExpiresAt: expiresAt, LastClass: lastClass,
			CreatedAt: now, UpdatedAt: now}
		if err := tx.Table(Delivery{}.TableName()).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "event_id"}, {Name: "subscription_id"}}, DoNothing: true,
		}).Create(&delivery).Error; err != nil {
			return err
		}
	}
	return nil
}

func (a *App) revokeSessionSubscriptions(tx *gorm.DB, payload revokedSessionPayload, now time.Time) error {
	if err := lockSessionIdentity(tx, payload.AccountID, payload.SessionID); err != nil {
		return err
	}
	revocation := SessionRevocation{AccountID: payload.AccountID, SessionID: payload.SessionID, RevokedAt: now}
	if err := tx.Table(SessionRevocation{}.TableName()).Clauses(clause.OnConflict{DoNothing: true}).Create(&revocation).Error; err != nil {
		return err
	}
	var subscriptions []Subscription
	if err := tx.Table(Subscription{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("account_id = ? AND source_session_id = ? AND disabled_at IS NULL", payload.AccountID, payload.SessionID).Find(&subscriptions).Error; err != nil {
		return err
	}
	if len(subscriptions) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		ids = append(ids, subscription.ID)
	}
	if err := tx.Table(Subscription{}.TableName()).Where("id IN ? AND disabled_at IS NULL", ids).
		Updates(map[string]any{"disabled_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	return tx.Table(Delivery{}.TableName()).Where("subscription_id IN ? AND status = ?", ids, "pending").
		Updates(map[string]any{"status": "cancelled", "last_class": "session_revoked", "lease_id": nil, "lease_until": nil}).Error
}

func validProducerEvent(producer, eventType string) bool {
	return producer == "event-service" && eventType == "event.created" || producer == "auth-service" && eventType == "session.revoked"
}

func validEventSeverity(value string) bool {
	switch value {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("payload must contain a single JSON value")
	}
	return nil
}

func (a *App) startOutboxWorker(ctx context.Context) {
	if a.sender == nil {
		slog.Error("push_delivery_worker_disabled", "reason", "web push sender unavailable")
		return
	}
	go a.deliveryLoop(ctx)
}
