package main

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Stream struct {
	ID                   uuid.UUID  `gorm:"type:uuid;primaryKey" json:"stream_id"`
	AccountID            uuid.UUID  `gorm:"type:uuid;not null;index" json:"account_id"`
	DeviceID             uuid.UUID  `gorm:"type:uuid;not null;index" json:"device_id"`
	CameraID             uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex" json:"camera_id"`
	EncryptedSource      []byte     `gorm:"type:bytea;not null" json:"-"`
	ConfigVersion        int64      `gorm:"not null;default:1" json:"config_version"`
	AppliedConfigVersion int64      `gorm:"not null;default:0" json:"applied_config_version"`
	ConfigStatus         string     `gorm:"size:24;not null;index" json:"config_status"`
	SourceState          string     `gorm:"size:24;not null;default:unknown" json:"source_state"`
	MediaState           string     `gorm:"size:24;not null;default:unknown" json:"media_state"`
	LastError            string     `gorm:"size:120" json:"last_error,omitempty"`
	Active               bool       `gorm:"not null;default:true;index" json:"active"`
	CreatedAt            time.Time  `gorm:"not null;index" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"not null" json:"updated_at"`
	DisabledAt           *time.Time `json:"disabled_at,omitempty"`
}

func (Stream) TableName() string { return "media.streams" }

type ViewerLease struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey" json:"lease_id"`
	AccountID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"account_id"`
	SessionID   string     `gorm:"size:128;not null;index" json:"session_id"`
	StreamID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"stream_id"`
	ExpiresAt   time.Time  `gorm:"not null;index" json:"expires_at"`
	ReleasedAt  *time.Time `gorm:"index" json:"released_at,omitempty"`
	KickPending bool       `gorm:"not null;default:false;index" json:"-"`
	PlayerID    string     `gorm:"size:128" json:"player_id,omitempty"`
	CreatedAt   time.Time  `gorm:"not null;index" json:"created_at"`
}

func (ViewerLease) TableName() string { return "media.viewer_leases" }

type Inbox struct {
	ID            uuid.UUID       `gorm:"type:uuid;primaryKey"`
	Producer      string          `gorm:"size:80;not null;uniqueIndex:idx_media_inbox_dedupe,priority:1"`
	EventID       uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_media_inbox_dedupe,priority:2"`
	EventType     string          `gorm:"size:100;not null;uniqueIndex:idx_media_inbox_dedupe,priority:3"`
	SchemaVersion int             `gorm:"not null"`
	OccurredAt    time.Time       `gorm:"not null;index"`
	Payload       json.RawMessage `gorm:"type:jsonb;not null"`
	ReceivedAt    time.Time       `gorm:"not null;index"`
	ProcessedAt   *time.Time      `gorm:"index"`
}

func (Inbox) TableName() string { return "media.inbox" }
