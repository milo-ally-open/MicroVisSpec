package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	viewerLeaseLifetime  = 25 * time.Second
	maxActiveSourceCount = 12
	maxViewerLeaseCount  = 24
	mediaQuotaLockID     = int64(728194631)
)

var errMediaCapacityFull = errors.New("media capacity is full")
var errMediaStreamNotConfigured = errors.New("media stream is not configured")

func (a *App) authorizedStreamForCamera(ctx context.Context, accountID, sessionID, cameraID uuid.UUID, requestID string) (Stream, error) {
	var stream Stream
	if a.device == nil || a.authorize == nil {
		return stream, errors.New("device and authorization services are required")
	}
	ownership, err := a.device.ResolveCamera(ctx, cameraID.String())
	if errors.Is(err, errOwnershipNotFound) {
		return stream, errOwnershipNotFound
	}
	if err != nil {
		return stream, err
	}
	ownerID, ownerErr := uuid.Parse(ownership.AccountID)
	deviceID, deviceErr := uuid.Parse(ownership.DeviceID)
	resolvedCameraID, cameraErr := uuid.Parse(ownership.CameraID)
	if ownerErr != nil || deviceErr != nil || cameraErr != nil || deviceID == uuid.Nil || ownerID == uuid.Nil || resolvedCameraID != cameraID {
		return stream, errOwnershipNotFound
	}
	if err := a.db.WithContext(ctx).Where("camera_id = ? AND account_id = ? AND active = ?", cameraID, ownerID, true).Take(&stream).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return stream, errMediaStreamNotConfigured
		}
		return stream, err
	}
	if stream.DeviceID != deviceID {
		return stream, errOwnershipNotFound
	}
	if err := a.authorize.CheckDeviceCapability(ctx, accountID, sessionID, deviceID, "stream.view", requestID); err != nil {
		return stream, err
	}
	return stream, nil
}

func (a *App) createPlaybackLease(c *gin.Context) {
	cameraID, err := uuid.Parse(c.Param("camera_id"))
	if err != nil || cameraID == uuid.Nil {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
		return
	}
	accountID := c.MustGet("account_id").(uuid.UUID)
	sessionID, err := uuid.Parse(strings.TrimSpace(c.GetHeader("X-Session-ID")))
	if err != nil || sessionID == uuid.Nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	stream, err := a.authorizedStreamForCamera(c.Request.Context(), accountID, sessionID, cameraID, c.GetString("request_id"))
	if errors.Is(err, errOwnershipNotFound) || errors.Is(err, errDeviceCapabilityDenied) {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
		return
	}
	if errors.Is(err, errMediaStreamNotConfigured) {
		writeError(c, http.StatusConflict, "stream_not_configured", "camera has no configured media source")
		return
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "camera authorization could not be verified")
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
	lease := ViewerLease{ID: uuid.New(), AccountID: accountID, SessionID: sessionID.String(), StreamID: stream.ID,
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
	sessionID, err := uuid.Parse(strings.TrimSpace(c.GetHeader("X-Session-ID")))
	if err != nil || sessionID == uuid.Nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	now := time.Now().UTC()
	newExpiry := now.Add(viewerLeaseLifetime)
	var lease ViewerLease
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND account_id = ? AND session_id = ? AND released_at IS NULL AND expires_at > ?",
		leaseID, accountID, sessionID.String(), now).Take(&lease).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusGone, "lease_expired", "viewer lease is no longer active")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "viewer lease could not be loaded")
		return
	}
	var stream Stream
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND active = ?", lease.StreamID, true).Take(&stream).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusGone, "lease_expired", "viewer lease is no longer active")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "media stream could not be loaded")
		}
		return
	}
	if _, err := a.authorizedStreamForCamera(c.Request.Context(), accountID, sessionID, stream.CameraID, c.GetString("request_id")); err != nil {
		if errors.Is(err, errOwnershipNotFound) || errors.Is(err, errDeviceCapabilityDenied) || errors.Is(err, errMediaStreamNotConfigured) {
			writeError(c, http.StatusGone, "lease_expired", "viewer lease is no longer authorized")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "viewer authorization could not be checked")
		return
	}
	result := a.db.WithContext(c.Request.Context()).Model(&ViewerLease{}).
		Where("id = ? AND account_id = ? AND session_id = ? AND released_at IS NULL AND expires_at > ?", leaseID, accountID, sessionID.String(), now).
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
	logReject := func(reason string) {
		slog.Warn("media_playback_authorization_denied", "request_id", c.GetString("request_id"), "reason", reason,
			"original_uri_present", requestURI != "")
	}
	if requestURI == "" || len(requestURI) > 4096 || strings.ContainsAny(requestURI, "\r\n") {
		logReject("invalid_original_uri")
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	parsedURI, err := url.ParseRequestURI(requestURI)
	if err != nil || parsedURI.IsAbs() || parsedURI.Host != "" || parsedURI.Fragment != "" {
		logReject("invalid_original_uri")
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	pathParts := strings.Split(strings.Trim(parsedURI.Path, "/"), "/")
	if len(pathParts) < 3 || pathParts[0] != "media" {
		logReject("invalid_media_path")
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	streamID, streamErr := uuid.Parse(pathParts[1])
	if streamErr != nil || streamID == uuid.Nil {
		logReject("invalid_stream_id")
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	values, err := url.ParseQuery(parsedURI.RawQuery)
	if err != nil || !validPlaybackQuery(values) {
		logReject("invalid_lease_query")
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	var requestedLeaseID uuid.UUID
	if leaseValues, hasLease := values["lease_id"]; hasLease {
		if len(leaseValues) != 1 || strings.TrimSpace(leaseValues[0]) == "" {
			writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
			return
		}
		leaseID, err := uuid.Parse(leaseValues[0])
		if err != nil || leaseID == uuid.Nil {
			logReject("invalid_lease_id")
			writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
			return
		}
		requestedLeaseID = leaseID
	}
	if a.auth == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "session could not be validated")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
	identity, authErr := a.auth.ValidateSession(ctx, c.Request.Header.Get("Cookie"), c.GetHeader("X-CSRF-Token"),
		c.GetHeader("X-Original-Method"), c.GetHeader("X-Original-Origin"), c.GetString("request_id"))
	cancel()
	if errors.Is(authErr, errAuthSessionUnauthorized) {
		slog.Warn("media_auth_session_rejected", "request_id", c.GetString("request_id"),
			"cookie_present", c.Request.Header.Get("Cookie") != "", "csrf_token_present", c.GetHeader("X-CSRF-Token") != "",
			"original_method_present", c.GetHeader("X-Original-Method") != "", "origin_present", c.GetHeader("X-Original-Origin") != "")
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	if errors.Is(authErr, errAuthSessionForbidden) {
		writeError(c, http.StatusForbidden, "csrf_rejected", "same-origin request with a valid CSRF token required")
		return
	}
	if authErr != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "session could not be validated")
		return
	}
	query := a.db.WithContext(c.Request.Context()).Table("media.viewer_leases AS leases").
		Joins("JOIN media.streams AS streams ON streams.id = leases.stream_id").
		Where("leases.account_id = ? AND leases.session_id = ? AND leases.stream_id = ? AND leases.released_at IS NULL AND leases.expires_at > ? AND streams.active = ?", identity.AccountID, identity.SessionID, streamID, time.Now().UTC(), true)
	if requestedLeaseID != uuid.Nil {
		query = query.Where("leases.id = ?", requestedLeaseID)
	}
	var count int64
	err = query.Count(&count).Error
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "media authorization could not be checked")
		return
	}
	if count != 1 {
		logReject("lease_not_found_for_session")
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	var stream Stream
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND active = ?", streamID, true).Take(&stream).Error; err != nil {
		logReject("stream_not_found")
		writeError(c, http.StatusUnauthorized, "unauthorized", "media access is not authorized")
		return
	}
	authorizedSessionID, parseErr := uuid.Parse(identity.SessionID)
	if parseErr != nil || authorizedSessionID == uuid.Nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "session identity could not be validated")
		return
	}
	if _, err := a.authorizedStreamForCamera(c.Request.Context(), identity.AccountID, authorizedSessionID, stream.CameraID, c.GetString("request_id")); err != nil {
		if !errors.Is(err, errOwnershipNotFound) && !errors.Is(err, errDeviceCapabilityDenied) && !errors.Is(err, errMediaStreamNotConfigured) {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "media authorization could not be checked")
			return
		}
		logReject("device_capability_denied")
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
	semaphore := make(chan struct{}, maxViewerLeaseCount)
	var workers sync.WaitGroup
	for _, lease := range leases {
		if ctx.Err() != nil {
			break
		}
		semaphore <- struct{}{}
		workers.Add(1)
		go func(item ViewerLease) {
			defer workers.Done()
			defer func() { <-semaphore }()
			callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := a.mediaMTX.KickLease(callCtx, item.StreamID.String(), item.ID.String())
			cancel()
			if err != nil {
				return
			}
			_ = a.db.WithContext(ctx).Model(&ViewerLease{}).Where("id = ? AND kick_pending = ?", item.ID, true).Update("kick_pending", false).Error
		}(lease)
	}
	workers.Wait()
}
