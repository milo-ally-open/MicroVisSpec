package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxEvidenceBytes = 4 << 20

type deviceHTTPAuthResult struct {
	Allowed  bool      `json:"allowed"`
	DeviceID uuid.UUID `json:"device_id"`
}

func (a *App) uploadEvidence(c *gin.Context) {
	deviceID, errDevice := uuid.Parse(c.Param("device_id"))
	edgeEventID, errEdge := uuid.Parse(c.Param("edge_event_id"))
	if errDevice != nil || deviceID == uuid.Nil || errEdge != nil || edgeEventID == uuid.Nil {
		writeError(c, http.StatusNotFound, "not_found", "event was not found")
		return
	}
	if c.Request.ContentLength > maxEvidenceBytes {
		writeError(c, http.StatusRequestEntityTooLarge, "evidence_too_large", "evidence must be no larger than 4 MiB")
		return
	}
	username, password, ok := c.Request.BasicAuth()
	if !ok || username == "" || password == "" {
		c.Header("WWW-Authenticate", `Basic realm="edge evidence"`)
		writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are required")
		return
	}
	if err := a.authenticateEvidenceDevice(c.Request.Context(), deviceID, username, password); err != nil {
		if errors.Is(err, errDeviceCredentialsRejected) {
			writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device credentials could not be verified")
		}
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxEvidenceBytes))
	if err != nil || len(body) == 0 || len(body) > maxEvidenceBytes {
		writeError(c, http.StatusRequestEntityTooLarge, "invalid_evidence", "evidence is empty or larger than 4 MiB")
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "image/jpeg" && mediaType != "image/png" {
		writeError(c, http.StatusUnsupportedMediaType, "invalid_evidence", "evidence must be JPEG or PNG")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || (format == "jpeg" && mediaType != "image/jpeg") || (format == "png" && mediaType != "image/png") || format != "jpeg" && format != "png" ||
		config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 || uint64(config.Width)*uint64(config.Height) > 50_000_000 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_evidence", "image signature or dimensions are invalid")
		return
	}
	var event Event
	if err := a.db.WithContext(c.Request.Context()).Table(Event{}.TableName()).Where("device_id = ? AND edge_event_id = ?", deviceID, edgeEventID).Take(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "event was not found")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event ownership could not be verified")
		return
	}
	digest := sha256.Sum256(body)
	digestText := hex.EncodeToString(digest[:])
	var existing Evidence
	err = a.db.WithContext(c.Request.Context()).Table(Evidence{}.TableName()).Where("event_id = ?", event.ID).Take(&existing).Error
	if err == nil {
		if existing.SHA256 != digestText {
			writeError(c, http.StatusConflict, "evidence_conflict", "evidence for this event has already been uploaded")
			return
		}
		c.JSON(http.StatusOK, evidenceResponse(existing, time.Now().UTC()))
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence state could not be loaded")
		return
	}
	if err := os.MkdirAll(a.config.EvidenceDir, 0o750); err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence storage is unavailable")
		return
	}
	extension := ".jpg"
	if format == "png" {
		extension = ".png"
	}
	finalName := uuid.NewString() + extension
	temporary, err := os.CreateTemp(a.config.EvidenceDir, ".upload-*")
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence storage is unavailable")
		return
	}
	tempName := temporary.Name()
	cleanupTemp := func() { _ = os.Remove(tempName) }
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		cleanupTemp()
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence could not be stored")
		return
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanupTemp()
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence could not be stored")
		return
	}
	if err := temporary.Close(); err != nil {
		cleanupTemp()
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence could not be stored")
		return
	}
	finalPath := filepath.Join(a.config.EvidenceDir, finalName)
	if err := os.Rename(tempName, finalPath); err != nil {
		cleanupTemp()
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence could not be stored")
		return
	}
	stored := false
	defer func() {
		if !stored {
			_ = os.Remove(finalPath)
		}
	}()
	// PostgreSQL timestamps have microsecond precision. Normalize before both
	// persisting and returning the 24-hour deadline so an idempotent retry
	// returns the exact timestamp that was stored.
	now := time.Now().UTC().Truncate(time.Microsecond)
	evidence := Evidence{ID: uuid.New(), EventID: event.ID, RelativePath: finalName, MIME: mediaType,
		SHA256: digestText, UploadedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	created := false
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Table(Evidence{}.TableName()).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, DoNothing: true}).Create(&evidence)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			created = true
			return nil
		}
		return tx.Table(Evidence{}.TableName()).Where("event_id = ?", event.ID).Take(&existing).Error
	})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence metadata could not be saved")
		return
	}
	if !created {
		if existing.SHA256 != digestText {
			writeError(c, http.StatusConflict, "evidence_conflict", "evidence for this event has already been uploaded")
			return
		}
		evidence = existing
	} else {
		stored = true
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	c.JSON(status, evidenceResponse(evidence, time.Now().UTC()))
}

var errDeviceCredentialsRejected = errors.New("device credentials rejected")

func (a *App) authenticateEvidenceDevice(ctx context.Context, deviceID uuid.UUID, username, password string) error {
	if a.config.DeviceServiceURL == "" || a.config.DeviceServiceToken == "" {
		return errors.New("device credential validator is not configured")
	}
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(a.config.DeviceServiceURL, "/") + "/internal/v1/devices/" + deviceID.String() + "/http-authenticate"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-Service-Token", a.config.DeviceServiceToken)
	request.Header.Set("X-Request-ID", uuid.NewString())
	response, err := a.deviceHTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusNotFound {
		return errDeviceCredentialsRejected
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("device credential validator returned HTTP %d", response.StatusCode)
	}
	var result deviceHTTPAuthResult
	if json.NewDecoder(io.LimitReader(response.Body, 8<<10)).Decode(&result) != nil || !result.Allowed || result.DeviceID != deviceID {
		return errors.New("device credential validator returned an invalid response")
	}
	return nil
}

func evidenceResponse(evidence Evidence, now time.Time) gin.H {
	status := "available"
	if evidence.DeletedAt != nil || !evidence.ExpiresAt.After(now) {
		status = "expired"
	}
	return gin.H{"evidence_id": evidence.ID, "status": status, "mime": evidence.MIME, "sha256": evidence.SHA256,
		"uploaded_at": evidence.UploadedAt, "expires_at": evidence.ExpiresAt}
}

func (a *App) evidenceStatus(ctx context.Context, eventID uuid.UUID, now time.Time) (string, error) {
	var evidence Evidence
	err := a.db.WithContext(ctx).Table(Evidence{}.TableName()).Where("event_id = ?", eventID).Take(&evidence).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "missing", nil
	}
	if err != nil {
		return "unavailable", err
	}
	if evidence.DeletedAt != nil || !evidence.ExpiresAt.After(now) {
		return "expired", nil
	}
	if _, err := evidenceAbsolutePath(a.config.EvidenceDir, evidence.RelativePath); err != nil {
		return "unavailable", nil
	}
	path, _ := evidenceAbsolutePath(a.config.EvidenceDir, evidence.RelativePath)
	if _, err := os.Stat(path); err != nil {
		return "unavailable", nil
	}
	return "available", nil
}

func evidenceAbsolutePath(root, relative string) (string, error) {
	if !filepath.IsLocal(relative) || filepath.Clean(relative) != relative || strings.ContainsAny(relative, `/\\`) {
		return "", errors.New("invalid evidence path")
	}
	return filepath.Join(root, relative), nil
}

func (a *App) readEvidence(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("event_id"))
	if err != nil || eventID == uuid.Nil {
		writeError(c, http.StatusNotFound, "not_found", "event was not found")
		return
	}
	var event Event
	eventQuery := a.db.WithContext(c.Request.Context()).Table(Event{}.TableName()).Where("id = ?", eventID)
	if c.GetString("user_type") == "pc" {
		eventQuery = eventQuery.Where("workspace_id = ?", accountID(c))
	}
	if err := eventQuery.Take(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "event was not found")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event could not be loaded")
		return
	}
	if err := a.requireEventCapability(c, event.DeviceID, "event.read"); err != nil {
		if errors.Is(err, errEventCapabilityDenied) {
			writeError(c, http.StatusNotFound, "not_found", "event was not found")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event authorization could not be checked")
		}
		return
	}
	var evidence Evidence
	if err := a.db.WithContext(c.Request.Context()).Table(Evidence{}.TableName()).Where("event_id = ?", event.ID).Take(&evidence).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "evidence_missing", "event has no uploaded evidence")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence could not be loaded")
		return
	}
	now := time.Now().UTC()
	if evidence.DeletedAt != nil || !evidence.ExpiresAt.After(now) {
		writeError(c, http.StatusGone, "evidence_expired", "event evidence has expired")
		return
	}
	path, err := evidenceAbsolutePath(a.config.EvidenceDir, evidence.RelativePath)
	if err != nil {
		writeError(c, http.StatusGone, "evidence_unavailable", "event evidence is unavailable")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_ = a.db.WithContext(c.Request.Context()).Table(Evidence{}.TableName()).Where("id = ? AND deleted_at IS NULL", evidence.ID).Update("deleted_at", now).Error
			writeError(c, http.StatusGone, "evidence_unavailable", "event evidence is unavailable")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event evidence could not be read")
		return
	}
	defer file.Close()
	c.Header("Content-Type", evidence.MIME)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Length", strconv.FormatInt(fileStatSize(file), 10))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, file)
}

func fileStatSize(file *os.File) int64 {
	info, err := file.Stat()
	if err != nil {
		return 0
	}
	return info.Size()
}
