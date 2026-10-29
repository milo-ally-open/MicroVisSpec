package main

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Subscription struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey" json:"subscription_id"`
	AccountID       uuid.UUID  `gorm:"type:uuid;not null;index:idx_push_subs_account_active,priority:1" json:"account_id"`
	SourceSessionID uuid.UUID  `gorm:"type:uuid;not null;index" json:"source_session_id"`
	Endpoint        string     `gorm:"type:text;not null;uniqueIndex" json:"-"`
	P256dhEncrypted []byte     `gorm:"type:bytea;not null" json:"-"`
	AuthEncrypted   []byte     `gorm:"type:bytea;not null" json:"-"`
	CreatedAt       time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"not null" json:"updated_at"`
	DisabledAt      *time.Time `gorm:"index:idx_push_subs_account_active,priority:2" json:"disabled_at,omitempty"`
}

func (Subscription) TableName() string { return "push.subscriptions" }

type Inbox struct {
	ID            uuid.UUID       `gorm:"type:uuid;primaryKey"`
	Producer      string          `gorm:"size:80;not null;uniqueIndex:idx_push_inbox_dedupe,priority:1"`
	EventID       uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_push_inbox_dedupe,priority:2"`
	EventType     string          `gorm:"size:100;not null;uniqueIndex:idx_push_inbox_dedupe,priority:3"`
	SchemaVersion int             `gorm:"not null"`
	OccurredAt    time.Time       `gorm:"not null;index"`
	Payload       json.RawMessage `gorm:"type:jsonb;not null"`
	ReceivedAt    time.Time       `gorm:"not null;index"`
	ProcessedAt   *time.Time      `gorm:"index"`
}

func (Inbox) TableName() string { return "push.inbox" }

type SessionRevocation struct {
	AccountID uuid.UUID `gorm:"type:uuid;primaryKey"`
	SessionID uuid.UUID `gorm:"type:uuid;primaryKey"`
	RevokedAt time.Time `gorm:"not null;index"`
}

func (SessionRevocation) TableName() string { return "push.session_revocations" }

type Delivery struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey"`
	EventID        uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:idx_push_delivery_dedupe,priority:1;index"`
	DeviceID       *uuid.UUID `gorm:"type:uuid;index"`
	SubscriptionID uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:idx_push_delivery_dedupe,priority:2;index"`
	AccountID      uuid.UUID  `gorm:"type:uuid;not null;index"`
	Payload        []byte     `gorm:"type:bytea;not null"`
	Status         string     `gorm:"size:24;not null;default:pending;index"`
	Attempts       int        `gorm:"not null;default:0"`
	NextAttemptAt  time.Time  `gorm:"not null;default:CURRENT_TIMESTAMP;index"`
	ExpiresAt      time.Time  `gorm:"not null;index"`
	AcceptedAt     *time.Time `gorm:"index"`
	LastClass      string     `gorm:"size:32"`
	LastError      string     `gorm:"size:500"`
	LeaseID        *uuid.UUID `gorm:"type:uuid;index"`
	LeaseUntil     *time.Time `gorm:"index"`
	CreatedAt      time.Time  `gorm:"not null;index"`
	UpdatedAt      time.Time  `gorm:"not null"`
}

func (Delivery) TableName() string { return "push.deliveries" }
