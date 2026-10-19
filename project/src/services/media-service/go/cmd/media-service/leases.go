package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	viewerLeaseLifetime  = 30 * time.Second
	maxActiveSourceCount = 12
	maxViewerLeaseCount  = 24
	mediaQuotaLockID     = int64(728194631)
)

var errMediaCapacityFull = errors.New("media capacity is full")

func (a *App) createPlaybackLease(c *gin.Context) {
	cameraID, err := uuid.Parse(c.Param("camera_id"))
	if err != nil || cameraID == uuid.Nil {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
		return
	}
	accountID := c.MustGet("account_id").(uuid.UUID)
	sessionID := strings.TrimSpace(c.GetHeader("X-Session-ID"))
	if sessionID == "" || len(sessionID) > 128 {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	if a.device == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "camera ownership service is unavailable")
		return
	}
	ownership, err := a.device.ResolveCamera(c.Request.Context(), cameraID.String())
	if errors.Is(err, errOwnershipNotFound) {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
		return
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "camera ownership could not be verified")
		return
	}
	ownerID, err := uuid.Parse(ownership.AccountID)
	if err != nil || ownerID != accountID || ownership.CameraID != cameraID.String() {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
		return
	}
	var stream Stream
	if err := a.db.WithContext(c.Request.Context()).Where("camera_id = ? AND account_id = ? AND active = ?", cameraID, accountID, true).Take(&stream).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusConflict, "stream_not_configured", "camera has no configured media source")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "media stream could not be loaded")
		return
	}
	if stream.ConfigStatus != "applied" || stream.AppliedConfigVersion != stream.ConfigVersion {
		writeError(c, http.StatusConflict, "stream_not_ready", "media source configuration is not ready")
		return
	}
	if a.config.PublicMediaBaseURL == "" {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "protected media base URL is not configured")
		return
	}
	now := time.Now().UTC()
	lease := ViewerLease{ID: uuid.New(), AccountID: accountID, SessionID: sessionID, StreamID: stream.ID,
		ExpiresAt: now.Add(viewerLeaseLifetime), CreatedAt: now}
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", mediaQuotaLockID).Error; err != nil {
			return err
		}
		var connections int64
		if err := tx.Model(&ViewerLease{}).Where("released_at IS NULL AND expires_at > ?", now).Count(&connections).Error; err != nil {
			return err
		}
		if connections >= maxViewerLeaseCount {
			return errMediaCapacityFull
		}
		var activeSources int64
		if err := tx.Raw("SELECT count(DISTINCT stream_id) FROM media.viewer_leases WHERE released_at IS NULL AND expires_at > ?", now).Scan(&activeSources).Error; err != nil {
			return err
		}
		var thisSourceActive int64
		if err := tx.Model(&ViewerLease{}).Where("stream_id = ? AND released_at IS NULL AND expires_at > ?", stream.ID, now).Count(&thisSourceActive).Error; err != nil {
			return err
		}
		if thisSourceActive == 0 && activeSources >= maxActiveSourceCount {
			return errMediaCapacityFull
		}
		return tx.Create(&lease).Error
	})
	if errors.Is(err, errMediaCapacityFull) {
		writeError(c, http.StatusTooManyRequests, "capacity_full", "media viewing capacity is full")
		return
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "viewer lease could not be created")
		return
	}
	base := strings.TrimRight(a.config.PublicMediaBaseURL, "/") + "/media/" + stream.ID.String()
	query := url.Values{"lease_id": []string{lease.ID.String()}}
	c.JSON(http.StatusOK, gin.H{
		"stream_id": stream.ID, "lease_id": lease.ID, "expires_at": lease.ExpiresAt,
		"status": stream.MediaState, "whep_url": base + "/whep?" + query.Encode(),
		"hls_url": base + "/index.m3u8?" + query.Encode(),
	})
}

func (a *App) renewViewerLease(c *gin.Context) {
	leaseID, err := uuid.Parse(c.Param("lease_id"))
	if err != nil || leaseID == uuid.Nil {
		writeError(c, http.StatusGone, "lease_expired", "viewer lease is no longer active")
		return
	}
	accountID := c.MustGet("account_id").(uuid.UUID)
	sessionID := strings.TrimSpace(c.GetHeader("X-Session-ID"))
	if sessionID == "" || len(sessionID) > 128 {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	now := time.Now().UTC()
	newExpiry := now.Add(viewerLeaseLifetime)
	result := a.db.WithContext(c.Request.Context()).Model(&ViewerLease{}).
		Where("id = ? AND account_id = ? AND session_id = ? AND released_at IS NULL AND expires_at > ?", leaseID, accountID, sessionID, now).
		Update("expires_at", newExpiry)
	if result.Error != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "viewer lease could not be renewed")
		return
	}
	if result.RowsAffected != 1 {
		writeError(c, http.StatusGone, "lease_expired", "viewer lease is no longer active")
		return
	}
	c.JSON(http.StatusOK, gin.H{"lease_id": leaseID, "expires_at": newExpiry})
}

func (a *App) releaseViewerLease(c *gin.Context) {
	leaseID, err := uuid.Parse(c.Param("lease_id"))
	if err != nil || leaseID == uuid.Nil {
		c.Status(http.StatusNoContent)
		return
	}
	accountID := c.MustGet("account_id").(uuid.UUID)
	sessionID := strings.TrimSpace(c.GetHeader("X-Session-ID"))
	if sessionID == "" || len(sessionID) > 128 {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	now := time.Now().UTC()
	result := a.db.WithContext(c.Request.Context()).Model(&ViewerLease{}).
		Where("id = ? AND account_id = ? AND session_id = ? AND released_at IS NULL", leaseID, accountID, sessionID).
		Updates(map[string]any{"released_at": now, "kick_pending": true})
	if result.Error != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "viewer lease could not be released")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	a.processPendingKicks(ctx)
	cancel()
	c.Status(http.StatusNoContent)
}

func (a *App) authorizePlayback(c *gin.Context) {
	requestURI := strings.TrimSpace(c.GetHeader("X-Original-URI"))
	if requestURI == "" || len(requestURI) > 4096 || strings.ContainsAny(requestURI, "\r\n") {
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	parsedURI, err := url.ParseRequestURI(requestURI)
	if err != nil || parsedURI.IsAbs() || parsedURI.Host != "" || parsedURI.Fragment != "" {
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	pathParts := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "media" {
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	streamID, streamErr := uuid.Parse(pathParts[1])
	accountID, accountErr := uuid.Parse(c.GetHeader("X-Account-ID"))
	sessionID := strings.TrimSpace(c.GetHeader("X-Session-ID"))
	if streamErr != nil || accountErr != nil || streamID == uuid.Nil || accountID == uuid.Nil || sessionID == "" {
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	query := a.db.WithContext(c.Request.Context()).Table("media.viewer_leases AS leases").
		Joins("JOIN media.streams AS streams ON streams.id = leases.stream_id").
		Where("leases.account_id = ? AND leases.session_id = ? AND leases.stream_id = ? AND leases.released_at IS NULL AND leases.expires_at > ? AND streams.active = ?", accountID, sessionID, streamID, time.Now().UTC(), true)
	values, err := url.ParseQuery(parsedURI.RawQuery)
	if err != nil || len(values) > 1 || len(values["lease_id"]) > 1 {
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	if leaseValues, hasLease := values["lease_id"]; hasLease {
		if len(leaseValues) != 1 || strings.TrimSpace(leaseValues[0]) == "" {
			writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
			return
		}
		leaseID, err := uuid.Parse(leaseValues[0])
		if err != nil || leaseID == uuid.Nil {
			writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
			return
		}
		query = query.Where("leases.id = ?", leaseID)
	}
	var count int64
	err = query.Count(&count).Error
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "media authorization could not be checked")
		return
	}
	if count != 1 {
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *App) expireViewerLeases(ctx context.Context) {
	now := time.Now().UTC()
	if err := a.db.WithContext(ctx).Model(&ViewerLease{}).
		Where("released_at IS NULL AND expires_at <= ?", now).
		Updates(map[string]any{"released_at": now, "kick_pending": true}).Error; err != nil {
		return
	}
	a.processPendingKicks(ctx)
}

func (a *App) processPendingKicks(ctx context.Context) {
	if a.mediaMTX == nil {
		return
	}
	var leases []ViewerLease
	if err := a.db.WithContext(ctx).Where("kick_pending = ?", true).Order("created_at ASC").Limit(100).Find(&leases).Error; err != nil {
		return
	}
	for _, lease := range leases {
		if ctx.Err() != nil {
			return
		}
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := a.mediaMTX.KickLease(callCtx, lease.StreamID.String(), lease.ID.String())
		cancel()
		if err != nil {
			continue
		}
		_ = a.db.WithContext(ctx).Model(&ViewerLease{}).Where("id = ? AND kick_pending = ?", lease.ID, true).Update("kick_pending", false).Error
	}
}
