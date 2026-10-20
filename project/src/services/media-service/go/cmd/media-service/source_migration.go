package main

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type sourceMigration struct {
	ID        uuid.UUID
	Previous  []byte
	Encrypted []byte
}

// migrateLegacyMediaSources removes the old separately stored RTSP
// username/password fields before the service starts reconciling MediaMTX.
// All ciphertexts are checked before the transaction updates any row.
func migrateLegacyMediaSources(ctx context.Context, db *gorm.DB, key []byte) error {
	if db == nil || len(key) != 32 {
		return errors.New("media source migration dependencies are incomplete")
	}
	var streams []Stream
	if err := db.WithContext(ctx).Order("id ASC").Find(&streams).Error; err != nil {
		return errors.New("media source migration query failed")
	}

	updates := make([]sourceMigration, 0)
	for _, stream := range streams {
		payload, err := decryptMediaSourcePayload(key, stream.EncryptedSource)
		if err != nil {
			return errors.New("media source migration could not decrypt a stored source")
		}
		parsed, err := url.Parse(strings.TrimSpace(payload.URL))
		if err != nil || (parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return errors.New("media source migration found an invalid RTSP URL")
		}
		if payload.Version == currentMediaSourceVersion && payload.Username == "" && payload.Password == "" {
			continue
		}
		encrypted, err := encryptMediaSource(key, mediaSourceSecret{URL: parsed.String()})
		if err != nil {
			return errors.New("media source migration could not re-encrypt a stored URL")
		}
		updates = append(updates, sourceMigration{ID: stream.ID, Previous: stream.EncryptedSource, Encrypted: encrypted})
	}
	if len(updates) == 0 {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, update := range updates {
			result := tx.Model(&Stream{}).Where("id = ? AND encrypted_source = ?", update.ID, update.Previous).
				Update("encrypted_source", update.Encrypted)
			if result.Error != nil {
				return errors.New("media source migration update failed")
			}
			if result.RowsAffected != 1 {
				return errors.New("media source changed during migration")
			}
		}
		return nil
	})
}
