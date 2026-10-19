package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (a *App) startMaintenance(ctx context.Context) {
	go func() {
		if err := a.cleanupEvidence(ctx); err != nil {
			slog.Error("event_evidence_cleanup_failed", "error", err.Error())
		}
		if err := a.cleanupResolvedEvents(ctx); err != nil {
			slog.Error("event_retention_cleanup_failed", "error", err.Error())
		}
		if err := a.cleanupOrphanFiles(ctx); err != nil {
			slog.Error("event_orphan_cleanup_failed", "error", err.Error())
		}
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := a.cleanupEvidence(ctx); err != nil {
					slog.Error("event_evidence_cleanup_failed", "error", err.Error())
				}
				if err := a.cleanupResolvedEvents(ctx); err != nil {
					slog.Error("event_retention_cleanup_failed", "error", err.Error())
				}
				if err := a.cleanupOrphanFiles(ctx); err != nil {
					slog.Error("event_orphan_cleanup_failed", "error", err.Error())
				}
			}
		}
	}()
}

func (a *App) cleanupEvidence(ctx context.Context) error {
	cutoff := time.Now().UTC()
	for batch := 0; batch < 20; batch++ {
		var items []Evidence
		if err := a.db.WithContext(ctx).Table(Evidence{}.TableName()).Where("expires_at <= ? AND deleted_at IS NULL", cutoff).
			Order("expires_at ASC").Limit(100).Find(&items).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		for _, item := range items {
			path, err := evidenceAbsolutePath(a.config.EvidenceDir, item.RelativePath)
			if err == nil {
				err = os.Remove(path)
				if errors.Is(err, os.ErrNotExist) {
					err = nil
				}
			}
			if err != nil {
				slog.Error("event_evidence_file_delete_failed", "event_id", item.EventID, "error", err.Error())
				continue
			}
			now := time.Now().UTC()
			if err := a.db.WithContext(ctx).Table(Evidence{}.TableName()).Where("id = ? AND deleted_at IS NULL", item.ID).Update("deleted_at", now).Error; err != nil {
				return err
			}
		}
		if len(items) < 100 {
			return nil
		}
	}
	return nil
}

func (a *App) cleanupResolvedEvents(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-90 * 24 * time.Hour)
	for batch := 0; batch < 20; batch++ {
		var events []Event
		if err := a.db.WithContext(ctx).Table(Event{}.TableName()).Where("status IN ? AND resolved_at <= ?", []string{"resolved", "false_positive"}, cutoff).
			Select("id").Order("resolved_at ASC").Limit(100).Find(&events).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(events))
		for _, event := range events {
			ids = append(ids, event.ID)
		}
		var evidence []Evidence
		if err := a.db.WithContext(ctx).Table(Evidence{}.TableName()).Where("event_id IN ?", ids).Find(&evidence).Error; err != nil {
			return err
		}
		if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Table(Evidence{}.TableName()).Where("event_id IN ?", ids).Delete(&Evidence{}).Error; err != nil {
				return err
			}
			if err := tx.Table(Disposition{}.TableName()).Where("event_id IN ?", ids).Delete(&Disposition{}).Error; err != nil {
				return err
			}
			if err := tx.Table(Outbox{}.TableName()).Where("id IN ?", ids).Delete(&Outbox{}).Error; err != nil {
				return err
			}
			return tx.Table(Event{}.TableName()).Where("id IN ? AND status IN ? AND resolved_at <= ?", ids, []string{"resolved", "false_positive"}, cutoff).Delete(&Event{}).Error
		}); err != nil {
			return err
		}
		for _, item := range evidence {
			path, err := evidenceAbsolutePath(a.config.EvidenceDir, item.RelativePath)
			if err == nil {
				if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					slog.Error("event_retained_evidence_delete_failed", "event_id", item.EventID, "error", removeErr.Error())
				}
			}
		}
		if len(events) < 100 {
			return nil
		}
	}
	return nil
}

func (a *App) cleanupOrphanFiles(ctx context.Context) error {
	entries, err := os.ReadDir(a.config.EvidenceDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var referenced []string
	if err := a.db.WithContext(ctx).Table(Evidence{}.TableName()).Pluck("relative_path", &referenced).Error; err != nil {
		return err
	}
	known := make(map[string]struct{}, len(referenced))
	for _, path := range referenced {
		known[path] = struct{}{}
	}
	cutoff := time.Now().Add(-time.Hour)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := known[name]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if filepath.Ext(name) != ".jpg" && filepath.Ext(name) != ".png" && filepath.Ext(name) != "" {
			continue
		}
		if err := os.Remove(filepath.Join(a.config.EvidenceDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Error("event_orphan_file_delete_failed", "error", err.Error())
		}
	}
	return nil
}
