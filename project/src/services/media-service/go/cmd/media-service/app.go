package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var errOwnershipNotFound = errors.New("camera ownership not found")

type App struct {
	db        *gorm.DB
	config    Config
	device    CameraOwnershipResolver
	authorize DeviceCapabilityAuthorizer
	mediaMTX  MediaPathControl
	auth      AuthSessionValidator
}

func OpenDatabase(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn), TranslateError: true})
	if err != nil {
		return nil, err
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS media").Error; err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Stream{}, &ViewerLease{}, &Inbox{}); err != nil {
		return nil, err
	}
	// Expand/backfill phase. Workspace IDs currently map to the existing PC
	// owner account IDs; keep the column nullable until orphan review is done.
	if err := db.Exec("UPDATE media.streams SET workspace_id = account_id WHERE workspace_id IS NULL").Error; err != nil {
		return nil, err
	}
	return db, nil
}

func NewApp(config Config, db *gorm.DB, device CameraOwnershipResolver, mediaMTX MediaPathControl) *App {
	return &App{db: db, config: config, device: device, authorize: NewAuthAuthorizationClient(config),
		mediaMTX: mediaMTX, auth: NewAuthSessionClient(config)}
}

func NewAppFromConfig(config Config, db *gorm.DB) *App {
	return NewApp(config, db, NewDeviceServiceClient(config), NewMediaMTXClient(config))
}

func (a *App) Handler() http.Handler {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(gin.Recovery(), a.requestContext(), a.accessLog())
	r.GET("/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "live"}) })
	r.GET("/health/ready", a.ready)
	api := r.Group("/api/v1/media", a.requireAccount())
	api.PUT("/cameras/:camera_id/source", a.putSource)
	api.GET("/cameras/:camera_id/status", a.cameraStatus)
	api.GET("/cameras/:camera_id/playback", a.createPlaybackLease)
	api.POST("/viewer-leases/:lease_id/renew", a.renewViewerLease)
	api.DELETE("/viewer-leases/:lease_id", a.releaseViewerLease)
	r.POST("/internal/v1/inbox", a.requireInboxService(), a.receiveInbox)
	r.GET("/internal/v1/media/authorize-playback", a.requireNginxToken(), a.authorizePlayback)
	return r
}

func (a *App) requestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if len(requestID) > 64 || strings.ContainsAny(requestID, "\r\n") {
			requestID = ""
		}
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func (a *App) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		slog.Info("media_http_request", "request_id", c.GetString("request_id"), "method", c.Request.Method,
			"path", c.Request.URL.Path, "status", c.Writer.Status(), "duration_ms", time.Since(started).Milliseconds())
	}
}

func (a *App) ready(c *gin.Context) {
	if a.db == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "database is not ready")
		return
	}
	sqlDB, err := a.db.DB()
	if err == nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		err = sqlDB.PingContext(ctx)
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "database is not ready")
		return
	}
	if a.mediaMTX == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MediaMTX control API is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	if err := a.mediaMTX.Check(ctx); err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MediaMTX control API is not ready")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "dependencies": gin.H{"postgres": "ready", "mediamtx_control_api": "ready"}})
}

func (a *App) requireAccount() gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, err := uuid.Parse(c.GetHeader("X-Account-ID"))
		if err != nil || accountID == uuid.Nil {
			writeError(c, http.StatusUnauthorized, "unauthorized", "a valid account session is required")
			c.Abort()
			return
		}
		c.Set("account_id", accountID)
		c.Next()
	}
}

func (a *App) requireInboxService() gin.HandlerFunc {
	return func(c *gin.Context) {
		service := c.GetHeader("X-Internal-Service")
		var expected string
		switch service {
		case "device-service":
			expected = a.config.InternalDeviceToken
		case "auth-service":
			expected = a.config.InternalAuthToken
		default:
			expected = ""
		}
		provided := c.GetHeader("X-Internal-Service-Token")
		if expected == "" || len(expected) != len(provided) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeError(c, http.StatusUnauthorized, "unauthorized", "service authentication required")
			c.Abort()
			return
		}
		c.Set("internal_producer", service)
		c.Next()
	}
}

func (a *App) requireNginxToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := c.GetHeader("X-Internal-Service-Token")
		expected := a.config.InternalNginxToken
		if expected == "" || len(expected) != len(provided) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			slog.Warn("media_authorization_proxy_token_rejected", "request_id", c.GetString("request_id"), "provided_present", provided != "", "expected_present", expected != "")
			writeError(c, http.StatusUnauthorized, "unauthorized", "media authorization request is not trusted")
			c.Abort()
			return
		}
		c.Next()
	}
}

func decodeJSON(c *gin.Context, destination any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func (a *App) putSource(c *gin.Context) {
	cameraID, err := uuid.Parse(c.Param("camera_id"))
	if err != nil || cameraID == uuid.Nil {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
		return
	}
	var input struct {
		RTSPURL string `json:"rtsp_url"`
	}
	if decodeJSON(c, &input) != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "request must contain only an RTSP URL")
		return
	}
	trimmedURL := strings.TrimSpace(input.RTSPURL)
	parsed, err := url.Parse(trimmedURL)
	if err != nil || len(trimmedURL) > 2048 || (parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "rtsp_url must be an RTSP URL without embedded credentials")
		return
	}
	if host, _, err := net.SplitHostPort(parsed.Host); err == nil && net.ParseIP(host) == nil && strings.Contains(host, " ") {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "rtsp_url host is invalid")
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
	accountID := c.MustGet("account_id").(uuid.UUID)
	ownerID, ownerErr := uuid.Parse(ownership.AccountID)
	deviceID, deviceErr := uuid.Parse(ownership.DeviceID)
	if ownerErr != nil || deviceErr != nil || ownerID != accountID || ownership.CameraID != cameraID.String() {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
		return
	}
	if len(a.config.MediaSourceEncryptionKey) != 32 {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "media source encryption is not configured")
		return
	}
	encrypted, err := encryptMediaSource(a.config.MediaSourceEncryptionKey, mediaSourceSecret{URL: parsed.String()})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "media source could not be secured")
		return
	}
	workspaceID := ownerID
	stream := Stream{ID: uuid.New(), AccountID: ownerID, WorkspaceID: &workspaceID, DeviceID: deviceID, CameraID: cameraID, EncryptedSource: encrypted,
		ConfigVersion: 1, ConfigStatus: "pending", SourceState: "configured", MediaState: "unknown", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var existing Stream
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("camera_id = ?", cameraID).Take(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&stream).Error
		}
		if err != nil {
			return err
		}
		stream = existing
		stream.AccountID = ownerID
		stream.DeviceID = deviceID
		stream.EncryptedSource = encrypted
		stream.ConfigVersion++
		stream.ConfigStatus = "pending"
		stream.SourceState = "configured"
		stream.LastError = ""
		stream.Active = true
		stream.DisabledAt = nil
		stream.UpdatedAt = time.Now().UTC()
		return tx.Save(&stream).Error
	})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "media source could not be saved")
		return
	}
	if err := a.reconcileStream(c.Request.Context(), stream.ID); err != nil {
		c.JSON(http.StatusAccepted, gin.H{"stream_id": stream.ID, "config_status": "pending", "config_version": stream.ConfigVersion})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stream_id": stream.ID, "config_status": "applied", "config_version": stream.ConfigVersion})
}

func (a *App) cameraStatus(c *gin.Context) {
	cameraID, err := uuid.Parse(c.Param("camera_id"))
	if err != nil || cameraID == uuid.Nil {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
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
	accountID := c.MustGet("account_id").(uuid.UUID)
	ownerID, err := uuid.Parse(ownership.AccountID)
	if err != nil || ownerID != accountID || ownership.CameraID != cameraID.String() {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
		return
	}
	var stream Stream
	err = a.db.WithContext(c.Request.Context()).Where("camera_id = ? AND account_id = ? AND active = ?", cameraID, accountID, true).Take(&stream).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"camera_id": cameraID, "device_id": ownership.DeviceID, "source_state": "not_configured", "media_state": "unknown", "config_status": "not_configured"})
		return
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "media stream status could not be loaded")
		return
	}
	source, err := decryptMediaSource(a.config.MediaSourceEncryptionKey, stream.EncryptedSource)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "media source configuration is unavailable")
		return
	}
	sourceState := "not_probed"
	mediaState := "idle"
	browserCompatibility := "unknown"
	if a.mediaMTX != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		state, statusErr := a.mediaMTX.PathState(ctx, stream.ID.String())
		cancel()
		if statusErr == nil {
			if state.Online {
				sourceState = "reachable"
				mediaState = "streaming"
				browserCompatibility = "unsupported_or_unknown"
				for _, track := range state.Tracks2 {
					if strings.EqualFold(track.Codec, "H264") && strings.Contains(strings.ToLower(track.CodecProps.Profile), "baseline") {
						browserCompatibility = "likely_h264_baseline"
						break
					}
				}
			}
			stream.SourceState = sourceState
			stream.MediaState = mediaState
			_ = a.db.WithContext(c.Request.Context()).Model(&Stream{}).Where("id = ?", stream.ID).Updates(map[string]any{"source_state": sourceState, "media_state": mediaState}).Error
		} else {
			sourceState = "unknown"
			mediaState = "unknown"
		}
	}
	c.JSON(http.StatusOK, gin.H{"stream_id": stream.ID, "device_id": stream.DeviceID, "camera_id": stream.CameraID,
		"source_state": sourceState, "media_state": mediaState, "browser_compatibility": browserCompatibility,
		"rtsp_url": source.URL, "config_status": stream.ConfigStatus, "config_version": stream.ConfigVersion,
		"applied_config_version": stream.AppliedConfigVersion, "last_error": stream.LastError})
}

type internalInboxEnvelope struct {
	Producer      string          `json:"producer"`
	EventType     string          `json:"event_type"`
	EventID       uuid.UUID       `json:"event_id"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

func (a *App) receiveInbox(c *gin.Context) {
	var envelope internalInboxEnvelope
	if decodeJSON(c, &envelope) != nil || envelope.EventID == uuid.Nil || envelope.SchemaVersion != 1 || envelope.Payload == nil || envelope.OccurredAt.IsZero() {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "internal event envelope is invalid")
		return
	}
	producer := c.GetString("internal_producer")
	if envelope.Producer != producer || !validProducerEvent(envelope.Producer, envelope.EventType) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "internal event producer or type is invalid")
		return
	}
	var devicePayload struct {
		AccountID string `json:"account_id"`
		DeviceID  string `json:"device_id"`
	}
	var sessionPayload struct {
		AccountID string `json:"account_id"`
		SessionID string `json:"session_id"`
	}
	switch envelope.EventType {
	case "device.disabled", "device.activated":
		if json.Unmarshal(envelope.Payload, &devicePayload) != nil || !validUUIDString(devicePayload.AccountID) || !validUUIDString(devicePayload.DeviceID) {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "device lifecycle payload is invalid")
			return
		}
	case "session.revoked":
		if json.Unmarshal(envelope.Payload, &sessionPayload) != nil || !validUUIDString(sessionPayload.AccountID) || strings.TrimSpace(sessionPayload.SessionID) == "" || len(sessionPayload.SessionID) > 128 {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "session.revoked payload is invalid")
			return
		}
	}
	ignoreLifecycle := false
	if envelope.EventType == "device.disabled" || envelope.EventType == "device.activated" {
		currentActive, err := a.deviceLifecycleActive(c.Request.Context(), devicePayload.DeviceID, devicePayload.AccountID)
		if err != nil {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device lifecycle state could not be checked")
			return
		}
		ignoreLifecycle = envelope.EventType == "device.disabled" && currentActive || envelope.EventType == "device.activated" && !currentActive
	}
	var affectedStreams []uuid.UUID
	duplicate := false
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		inbox := Inbox{ID: uuid.New(), Producer: producer, EventID: envelope.EventID, EventType: envelope.EventType,
			SchemaVersion: envelope.SchemaVersion, OccurredAt: envelope.OccurredAt.UTC(), Payload: envelope.Payload,
			ReceivedAt: time.Now().UTC()}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "producer"}, {Name: "event_id"}, {Name: "event_type"}}, DoNothing: true}).Create(&inbox)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			duplicate = true
			return nil
		}
		now := time.Now().UTC()
		switch envelope.EventType {
		case "device.disabled":
			if ignoreLifecycle {
				break
			}
			deviceID, _ := uuid.Parse(devicePayload.DeviceID)
			accountID, _ := uuid.Parse(devicePayload.AccountID)
			if err := tx.Model(&Stream{}).Where("device_id = ? AND account_id = ? AND active = ?", deviceID, accountID, true).Pluck("id", &affectedStreams).Error; err != nil {
				return err
			}
			if len(affectedStreams) > 0 {
				if err := tx.Model(&Stream{}).Where("id IN ?", affectedStreams).Updates(map[string]any{"active": false, "config_status": "delete_pending", "disabled_at": now, "config_version": gorm.Expr("config_version + 1"), "updated_at": now}).Error; err != nil {
					return err
				}
				if err := tx.Model(&ViewerLease{}).Where("stream_id IN ? AND released_at IS NULL", affectedStreams).Update("released_at", now).Error; err != nil {
					return err
				}
			}
		case "device.activated":
			if ignoreLifecycle {
				break
			}
			deviceID, _ := uuid.Parse(devicePayload.DeviceID)
			accountID, _ := uuid.Parse(devicePayload.AccountID)
			if err := tx.Model(&Stream{}).Where("device_id = ? AND account_id = ? AND active = ? AND disabled_at IS NOT NULL", deviceID, accountID, false).Pluck("id", &affectedStreams).Error; err != nil {
				return err
			}
			if len(affectedStreams) > 0 {
				if err := tx.Model(&Stream{}).Where("id IN ?", affectedStreams).Updates(map[string]any{"active": true, "config_status": "pending", "disabled_at": nil, "config_version": gorm.Expr("config_version + 1"), "updated_at": now}).Error; err != nil {
					return err
				}
			}
		case "session.revoked":
			accountID, _ := uuid.Parse(sessionPayload.AccountID)
			if err := tx.Model(&ViewerLease{}).Where("account_id = ? AND session_id = ? AND released_at IS NULL", accountID, sessionPayload.SessionID).
				Updates(map[string]any{"released_at": now, "kick_pending": true}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&Inbox{}).Where("producer = ? AND event_id = ? AND event_type = ?", producer, envelope.EventID, envelope.EventType).Update("processed_at", now).Error
	})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "internal event could not be persisted")
		return
	}
	if !duplicate && (envelope.EventType == "device.disabled" || envelope.EventType == "device.activated") {
		for _, streamID := range affectedStreams {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
			_ = a.reconcileStream(ctx, streamID)
			cancel()
		}
	}
	if !duplicate && envelope.EventType == "session.revoked" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
		a.processPendingKicks(ctx)
		cancel()
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted", "duplicate": duplicate})
}

func validProducerEvent(producer, eventType string) bool {
	return (producer == "device-service" && (eventType == "device.disabled" || eventType == "device.activated")) || (producer == "auth-service" && eventType == "session.revoked")
}

func (a *App) deviceLifecycleActive(ctx context.Context, rawDeviceID, rawAccountID string) (bool, error) {
	deviceID, _ := uuid.Parse(rawDeviceID)
	accountID, _ := uuid.Parse(rawAccountID)
	var camera struct{ CameraID uuid.UUID }
	err := a.db.WithContext(ctx).Model(&Stream{}).Select("camera_id").Where("device_id = ? AND account_id = ?", deviceID, accountID).Order("updated_at DESC").Take(&camera).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if a.device == nil {
		return false, errors.New("device ownership service is unavailable")
	}
	ownership, err := a.device.ResolveCamera(ctx, camera.CameraID.String())
	if errors.Is(err, errOwnershipNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ownership.DeviceID == deviceID.String() && ownership.AccountID == accountID.String() && ownership.CameraID == camera.CameraID.String(), nil
}

func validUUIDString(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil
}

func (a *App) reconcileStream(ctx context.Context, streamID uuid.UUID) error {
	var stream Stream
	if err := a.db.WithContext(ctx).First(&stream, "id = ?", streamID).Error; err != nil {
		return err
	}
	if a.mediaMTX == nil {
		return errors.New("MediaMTX is not configured")
	}
	if !stream.Active {
		if err := a.mediaMTX.DeletePath(ctx, stream.ID.String()); err != nil {
			_ = a.db.WithContext(ctx).Model(&Stream{}).Where("id = ?", stream.ID).Update("last_error", "mediamtx_delete_failed").Error
			return err
		}
		return a.db.WithContext(ctx).Model(&Stream{}).Where("id = ?", stream.ID).Updates(map[string]any{"config_status": "disabled", "applied_config_version": stream.ConfigVersion, "last_error": "", "updated_at": time.Now().UTC()}).Error
	}
	source, err := decryptMediaSource(a.config.MediaSourceEncryptionKey, stream.EncryptedSource)
	if err != nil {
		_ = a.db.WithContext(ctx).Model(&Stream{}).Where("id = ?", stream.ID).Updates(map[string]any{"config_status": "failed", "last_error": "source_decryption_failed"}).Error
		return err
	}
	parsed, err := url.Parse(source.URL)
	if err != nil || parsed.User != nil || (parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps") || parsed.Host == "" {
		return errors.New("stored media source URL is invalid")
	}
	if err := a.mediaMTX.ApplyPath(ctx, stream.ID.String(), parsed.String()); err != nil {
		_ = a.db.WithContext(ctx).Model(&Stream{}).Where("id = ?", stream.ID).Updates(map[string]any{"config_status": "pending", "last_error": "mediamtx_reconcile_failed"}).Error
		return err
	}
	return a.db.WithContext(ctx).Model(&Stream{}).Where("id = ? AND config_version = ?", stream.ID, stream.ConfigVersion).
		Updates(map[string]any{"config_status": "applied", "applied_config_version": stream.ConfigVersion, "last_error": "", "updated_at": time.Now().UTC()}).Error
}

func (a *App) reconciliationLoop(ctx context.Context) {
	a.reconcilePendingStreams(ctx)
	a.expireViewerLeases(ctx)
	reconcileTicker := time.NewTicker(5 * time.Second)
	expiryTicker := time.NewTicker(time.Second)
	defer reconcileTicker.Stop()
	defer expiryTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcileTicker.C:
			a.reconcilePendingStreams(ctx)
		case <-expiryTicker.C:
			a.expireViewerLeases(ctx)
		}
	}
}

func (a *App) reconcilePendingStreams(ctx context.Context) {
	var streams []Stream
	if err := a.db.WithContext(ctx).Where("config_status IN ?", []string{"pending", "delete_pending"}).Order("updated_at ASC").Limit(100).Find(&streams).Error; err != nil {
		slog.Error("media_reconciliation_query_failed", "error", err.Error())
		return
	}
	for _, stream := range streams {
		if ctx.Err() != nil {
			return
		}
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := a.reconcileStream(callCtx, stream.ID); err != nil {
			slog.Error("media_stream_reconciliation_failed", "stream_id", stream.ID, "error", err.Error())
		}
		cancel()
	}
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "request_id": c.GetString("request_id")})
}
