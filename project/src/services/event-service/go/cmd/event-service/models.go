package main

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Event struct {
	ID                uuid.UUID       `gorm:"type:uuid;primaryKey" json:"event_id"`
	AccountID         uuid.UUID       `gorm:"type:uuid;not null;index:idx_events_account_occurred,priority:1" json:"account_id"`
	WorkspaceID       *uuid.UUID      `gorm:"type:uuid;index" json:"-"`
	DeviceID          uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_events_device_edge,priority:1;index" json:"device_id"`
	CameraID          uuid.UUID       `gorm:"type:uuid;not null;index" json:"camera_id"`
	EdgeEventID       uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_events_device_edge,priority:2" json:"edge_event_id"`
	EventType         string          `gorm:"size:120;not null;index" json:"event_type"`
	Severity          string          `gorm:"size:16;not null;index" json:"severity"`
	OccurredAt        time.Time       `gorm:"not null;index:idx_events_account_occurred,priority:2,sort:desc" json:"occurred_at"`
	ReceivedAt        time.Time       `gorm:"not null;index" json:"received_at"`
	DeviceName        string          `gorm:"size:120;not null" json:"device_name"`
	CameraName        string          `gorm:"size:120;not null" json:"camera_name"`
	Location          string          `gorm:"size:256;not null;index" json:"location"`
	AlgorithmKey      string          `gorm:"size:120;not null" json:"algorithm_key"`
	AlgorithmVersion  string          `gorm:"size:120;not null" json:"algorithm_version"`
	RuleConfigVersion int64           `gorm:"not null" json:"rule_config_version"`
	RegionKey         string          `gorm:"size:64" json:"region_key,omitempty"`
	Attributes        json.RawMessage `gorm:"type:jsonb;not null" json:"attributes"`
	Status            string          `gorm:"size:24;not null;default:pending;index" json:"status"`
	Version           int64           `gorm:"not null;default:1" json:"version"`
	ResolvedAt        *time.Time      `gorm:"index" json:"resolved_at,omitempty"`
	CreatedAt         time.Time       `gorm:"not null" json:"created_at"`
}

func (Event) TableName() string { return "event.events" }

func (event *Event) BeforeCreate(*gorm.DB) error {
	if event.WorkspaceID == nil && event.AccountID != uuid.Nil {
		workspaceID := event.AccountID
		event.WorkspaceID = &workspaceID
	}
	return nil
}

type Evidence struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey"`
	EventID      uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex"`
	RelativePath string     `gorm:"size:512;not null"`
	MIME         string     `gorm:"size:64;not null"`
	SHA256       string     `gorm:"size:64;not null"`
	UploadedAt   time.Time  `gorm:"not null;index"`
	ExpiresAt    time.Time  `gorm:"not null;index"`
	DeletedAt    *time.Time `gorm:"index"`
}

func (Evidence) TableName() string { return "event.evidence" }

type Disposition struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"disposition_id"`
	EventID        uuid.UUID `gorm:"type:uuid;not null;index" json:"event_id"`
	ActorAccountID uuid.UUID `gorm:"type:uuid;not null;index" json:"actor_account_id"`
	FromStatus     string    `gorm:"size:24;not null" json:"from_status"`
	ToStatus       string    `gorm:"size:24;not null" json:"to_status"`
	Result         string    `gorm:"type:text;not null" json:"result"`
	ActedAt        time.Time `gorm:"not null;index" json:"acted_at"`
}

func (Disposition) TableName() string { return "event.dispositions" }

type Outbox struct {
	ID            uuid.UUID       `gorm:"type:uuid;primaryKey" json:"event_id"`
	WorkspaceID   *uuid.UUID      `gorm:"type:uuid;index" json:"-"`
	EventType     string          `gorm:"size:100;not null;index" json:"event_type"`
	Payload       json.RawMessage `gorm:"type:jsonb;not null" json:"payload"`
	CreatedAt     time.Time       `gorm:"not null;index" json:"created_at"`
	UpdatedAt     time.Time       `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
	DeliveredAt   *time.Time      `gorm:"index" json:"delivered_at,omitempty"`
	Attempts      int             `gorm:"not null;default:0" json:"attempts"`
	NextAttemptAt time.Time       `gorm:"not null;default:CURRENT_TIMESTAMP;index" json:"next_attempt_at"`
	LastError     string          `gorm:"size:500" json:"last_error,omitempty"`
	LeaseID       *uuid.UUID      `gorm:"type:uuid;index" json:"-"`
	LeaseUntil    *time.Time      `gorm:"index" json:"-"`
}

func (Outbox) TableName() string { return "event.outbox" }
