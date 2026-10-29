package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const eventCursorLifetime = 15 * time.Minute

type eventFilters struct {
	StartAt   time.Time `json:"start_at"`
	EndAt     time.Time `json:"end_at"`
	EventType string    `json:"event_type"`
	Severity  string    `json:"severity"`
	DeviceID  uuid.UUID `json:"device_id"`
	CameraID  uuid.UUID `json:"camera_id"`
	Location  string    `json:"location"`
	Status    string    `json:"status"`
}

type eventCursor struct {
	OccurredAt time.Time    `json:"occurred_at"`
	ID         uuid.UUID    `json:"id"`
	FilterHash string       `json:"filter_hash"`
	ExpiresAt  time.Time    `json:"expires_at"`
	Filters    eventFilters `json:"filters"`
}

type eventCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

func (a *App) listEvents(c *gin.Context) {
	deviceIDs, mobile, scopeErr := a.eventDeviceScope(c, "event.read")
	if scopeErr != nil {
		if errors.Is(scopeErr, errEventCapabilityDenied) {
			writeError(c, http.StatusForbidden, "forbidden", "event.read permission is required")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event authorization could not be checked")
		}
		return
	}
	var cursor *eventCursor
	if token := c.Query("cursor"); token != "" {
		decoded, err := a.decodeEventCursor(token, time.Now().UTC())
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "invalid_cursor", "cursor is invalid or expired")
			return
		}
		cursor = &decoded
	}
	var defaults *eventFilters
	if cursor != nil {
		defaults = &cursor.Filters
	}
	filters, err := parseEventFilters(c, defaults)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", err.Error())
		return
	}
	limit := 20
	if raw := c.Query("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "limit must be between 1 and 100")
			return
		}
	}
	filterHash, err := hashFilters(filters)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "event filters could not be encoded")
		return
	}
	if cursor != nil {
		if hasEventFilterQuery(c) {
			if filterHash != cursor.FilterHash {
				writeError(c, http.StatusUnprocessableEntity, "invalid_cursor", "cursor belongs to different filters")
				return
			}
		} else {
			filters, filterHash = cursor.Filters, cursor.FilterHash
		}
	}
	query := a.filteredEvents(c.Request.Context(), accountID(c), filters, deviceIDs, mobile)
	if cursor != nil {
		query = query.Where("(occurred_at < ?) OR (occurred_at = ? AND id < ?)", cursor.OccurredAt, cursor.OccurredAt, cursor.ID)
	}
	var rows []Event
	if err := query.Order("occurred_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "events could not be loaded")
		return
	}
	var nextCursor *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		token, err := a.encodeEventCursor(eventCursor{OccurredAt: last.OccurredAt, ID: last.ID,
			FilterHash: filterHash, ExpiresAt: time.Now().UTC().Add(eventCursorLifetime), Filters: filters})
		if err != nil {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event pagination is not configured")
			return
		}
		nextCursor = &token
	}
	c.JSON(http.StatusOK, gin.H{"items": rows, "next_cursor": nextCursor})
}

func hasEventFilterQuery(c *gin.Context) bool {
	for _, key := range []string{"start_at", "end_at", "event_type", "severity", "device_id", "camera_id", "location", "status"} {
		if _, exists := c.GetQuery(key); exists {
			return true
		}
	}
	return false
}

func parseEventFilters(c *gin.Context, defaults *eventFilters) (eventFilters, error) {
	now := time.Now().UTC()
	filters := eventFilters{StartAt: now.Add(-7 * 24 * time.Hour), EndAt: now}
	if defaults != nil {
		filters = *defaults
	}
	if raw := c.Query("start_at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return filters, errors.New("start_at and end_at must be RFC3339 timestamps")
		}
		filters.StartAt = parsed.UTC()
	}
	if raw := c.Query("end_at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return filters, errors.New("start_at and end_at must be RFC3339 timestamps")
		}
		filters.EndAt = parsed.UTC()
	}
	if !filters.StartAt.Before(filters.EndAt) {
		return filters, errors.New("start_at must be earlier than end_at")
	}
	filters.EventType, filters.Location = strings.TrimSpace(c.Query("event_type")), strings.TrimSpace(c.Query("location"))
	filters.Severity, filters.Status = strings.TrimSpace(c.Query("severity")), strings.TrimSpace(c.Query("status"))
	if len(filters.EventType) > 120 || len(filters.Location) > 256 {
		return filters, errors.New("event_type or location filter is too long")
	}
	if filters.Severity != "" && !validSeverities[filters.Severity] {
		return filters, errors.New("severity filter is unsupported")
	}
	if filters.Status != "" && !containsString([]string{"pending", "processing", "resolved", "false_positive"}, filters.Status) {
		return filters, errors.New("status filter is unsupported")
	}
	if raw := c.Query("device_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return filters, errors.New("device_id is invalid")
		}
		filters.DeviceID = id
	}
	if raw := c.Query("camera_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return filters, errors.New("camera_id is invalid")
		}
		filters.CameraID = id
	}
	return filters, nil
}

func (a *App) eventDeviceScope(c *gin.Context, capability string) ([]uuid.UUID, bool, error) {
	if c.GetString("user_type") == "pc" {
		return nil, false, nil
	}
	if a.authorization == nil {
		return nil, true, errors.New("authorization client is not configured")
	}
	deviceIDs, err := a.authorization.ListAuthorizedDevices(c.Request.Context(), accountID(c), c.GetString("session_id"), capability, c.GetString("request_id"))
	return deviceIDs, true, err
}

func (a *App) requireEventCapability(c *gin.Context, deviceID uuid.UUID, capability string) error {
	if c.GetString("user_type") == "pc" {
		return nil
	}
	if a.authorization == nil {
		return errors.New("authorization client is not configured")
	}
	return a.authorization.CheckDeviceCapability(c.Request.Context(), accountID(c), c.GetString("session_id"), deviceID, capability, c.GetString("request_id"))
}

func (a *App) filteredEvents(ctx context.Context, account uuid.UUID, filters eventFilters, deviceIDs []uuid.UUID, mobile bool) *gorm.DB {
	query := a.db.WithContext(ctx).Table(Event{}.TableName()).Where("occurred_at >= ? AND occurred_at < ?", filters.StartAt, filters.EndAt)
	if mobile {
		if len(deviceIDs) == 0 {
			query = query.Where("1 = 0")
		} else {
			query = query.Where("device_id IN ?", deviceIDs)
		}
	} else {
		query = query.Where("workspace_id = ?", account)
	}
	if filters.EventType != "" {
		query = query.Where("event_type = ?", filters.EventType)
	}
	if filters.Severity != "" {
		query = query.Where("severity = ?", filters.Severity)
	}
	if filters.DeviceID != uuid.Nil {
		query = query.Where("device_id = ?", filters.DeviceID)
	}
	if filters.CameraID != uuid.Nil {
		query = query.Where("camera_id = ?", filters.CameraID)
	}
	if filters.Location != "" {
		query = query.Where("location ILIKE ?", "%"+escapeLike(filters.Location)+"%")
	}
	if filters.Status != "" {
		query = query.Where("status = ?", filters.Status)
	}
	return query
}

func escapeLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}

func hashFilters(filters eventFilters) (string, error) {
	encoded, err := json.Marshal(filters)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func (a *App) encodeEventCursor(cursor eventCursor) (string, error) {
	if len(a.config.CursorSigningKey) < 32 || cursor.ID == uuid.Nil || cursor.OccurredAt.IsZero() {
		return "", errors.New("event cursor signing key or cursor is invalid")
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, a.config.CursorSigningKey)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...)), nil
}

func (a *App) decodeEventCursor(token string, now time.Time) (eventCursor, error) {
	var cursor eventCursor
	if len(a.config.CursorSigningKey) < 32 || len(token) > 2048 {
		return cursor, errors.New("cursor signing key is unavailable")
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(data) <= sha256.Size {
		return cursor, errors.New("invalid cursor encoding")
	}
	payload, signature := data[:len(data)-sha256.Size], data[len(data)-sha256.Size:]
	mac := hmac.New(sha256.New, a.config.CursorSigningKey)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return cursor, errors.New("invalid cursor signature")
	}
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.ID == uuid.Nil || cursor.OccurredAt.IsZero() {
		return cursor, errors.New("invalid cursor payload")
	}
	if !cursor.ExpiresAt.After(now) {
		return cursor, errors.New("expired cursor")
	}
	filterHash, err := hashFilters(cursor.Filters)
	if err != nil || cursor.FilterHash != filterHash {
		return cursor, errors.New("cursor filter data is invalid")
	}
	return cursor, nil
}

func (a *App) getEvent(c *gin.Context) {
	id, err := uuid.Parse(c.Param("event_id"))
	if err != nil || id == uuid.Nil {
		writeError(c, http.StatusNotFound, "not_found", "event was not found")
		return
	}
	var event Event
	eventQuery := a.db.WithContext(c.Request.Context()).Table(Event{}.TableName()).Where("id = ?", id)
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
	evidenceStatus, err := a.evidenceStatus(c.Request.Context(), id, time.Now().UTC())
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence status could not be loaded")
		return
	}
	var dispositions []Disposition
	if err := a.db.WithContext(c.Request.Context()).Table(Disposition{}.TableName()).Where("event_id = ?", id).Order("acted_at ASC, id ASC").Find(&dispositions).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event history could not be loaded")
		return
	}
	c.JSON(http.StatusOK, gin.H{"event": event, "evidence": gin.H{"status": evidenceStatus}, "dispositions": dispositions})
}

func (a *App) transitionEvent(c *gin.Context) {
	id, err := uuid.Parse(c.Param("event_id"))
	if err != nil || id == uuid.Nil {
		writeError(c, http.StatusNotFound, "not_found", "event was not found")
		return
	}
	var input struct {
		ToStatus        string `json:"to_status"`
		Result          string `json:"result"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if decodeEventJSON(c, &input, 16<<10) != nil || input.ExpectedVersion < 1 || len(input.Result) > 2000 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "to_status, expected_version, and a result up to 2000 characters are required")
		return
	}
	input.Result = strings.TrimSpace(input.Result)
	if isTerminalEventStatus(input.ToStatus) && input.Result == "" {
		writeError(c, http.StatusUnprocessableEntity, "result_required", "a result is required to close an event")
		return
	}
	var targetEvent Event
	targetQuery := a.db.WithContext(c.Request.Context()).Table(Event{}.TableName()).Where("id = ?", id)
	if c.GetString("user_type") == "pc" {
		targetQuery = targetQuery.Where("workspace_id = ?", accountID(c))
	}
	if err := targetQuery.Take(&targetEvent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "event was not found")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event could not be loaded")
		}
		return
	}
	if err := a.requireEventCapability(c, targetEvent.DeviceID, "event.handle"); err != nil {
		if errors.Is(err, errEventCapabilityDenied) {
			writeError(c, http.StatusNotFound, "not_found", "event was not found")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event authorization could not be checked")
		}
		return
	}
	now := time.Now().UTC()
	var updated Event
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var current Event
		currentQuery := tx.Table(Event{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id)
		if c.GetString("user_type") == "pc" {
			currentQuery = currentQuery.Where("workspace_id = ?", accountID(c))
		} else {
			currentQuery = currentQuery.Where("device_id = ?", targetEvent.DeviceID)
		}
		if err := currentQuery.Take(&current).Error; err != nil {
			return err
		}
		if current.Version != input.ExpectedVersion {
			return errEventVersionConflict
		}
		if !validEventTransition(current.Status, input.ToStatus) {
			return errEventTransitionInvalid
		}
		updates := map[string]any{"status": input.ToStatus, "version": current.Version + 1}
		if isTerminalEventStatus(input.ToStatus) {
			updates["resolved_at"] = now
		}
		updateQuery := tx.Table(Event{}.TableName()).Where("id = ? AND version = ?", current.ID, current.Version)
		if c.GetString("user_type") == "pc" {
			updateQuery = updateQuery.Where("workspace_id = ?", accountID(c))
		} else {
			updateQuery = updateQuery.Where("device_id = ?", targetEvent.DeviceID)
		}
		if err := updateQuery.Updates(updates).Error; err != nil {
			return err
		}
		disposition := Disposition{ID: uuid.New(), WorkspaceID: current.WorkspaceID, EventID: current.ID, ActorAccountID: accountID(c), FromStatus: current.Status,
			ToStatus: input.ToStatus, Result: input.Result, ActedAt: now}
		if err := tx.Table(Disposition{}.TableName()).Create(&disposition).Error; err != nil {
			return err
		}
		current.Status, current.Version = input.ToStatus, current.Version+1
		if isTerminalEventStatus(input.ToStatus) {
			current.ResolvedAt = &now
		}
		updated = current
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			writeError(c, http.StatusNotFound, "not_found", "event was not found")
		case errors.Is(err, errEventVersionConflict):
			writeError(c, http.StatusConflict, "version_conflict", "event changed since it was loaded")
		case errors.Is(err, errEventTransitionInvalid):
			writeError(c, http.StatusUnprocessableEntity, "invalid_transition", "event status transition is not allowed")
		default:
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event transition could not be saved")
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"event": updated})
}

func decodeEventJSON(c *gin.Context, target any, limit int64) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

var errEventVersionConflict = errors.New("event version conflict")
var errEventTransitionInvalid = errors.New("event transition invalid")

func validEventTransition(from, to string) bool {
	switch from + "->" + to {
	case "pending->processing", "pending->false_positive", "processing->resolved", "processing->false_positive":
		return true
	default:
		return false
	}
}

func isTerminalEventStatus(status string) bool {
	return status == "resolved" || status == "false_positive"
}

func (a *App) eventStats(c *gin.Context) {
	requested := c.DefaultQuery("range", "7d")
	if requested != "today" && requested != "7d" && requested != "30d" {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "range must be today, 7d, or 30d")
		return
	}
	deviceIDs, mobile, scopeErr := a.eventDeviceScope(c, "event.read")
	if scopeErr != nil {
		if errors.Is(scopeErr, errEventCapabilityDenied) {
			writeError(c, http.StatusForbidden, "forbidden", "event.read permission is required")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event authorization could not be checked")
		}
		return
	}
	location, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().UTC()
	localNow := now.In(location)
	startLocal := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	unit, format := "day", "YYYY-MM-DD"
	switch requested {
	case "7d":
		startLocal = startLocal.AddDate(0, 0, -6)
	case "30d":
		startLocal = startLocal.AddDate(0, 0, -29)
	case "today":
		unit, format = "hour", "YYYY-MM-DD HH24:00"
	}
	start := startLocal.UTC()
	var total int64
	totalQuery := a.db.WithContext(c.Request.Context()).Table(Event{}.TableName()).Where("occurred_at >= ? AND occurred_at < ?", start, now)
	if mobile {
		totalQuery = totalQuery.Where("device_id IN ?", deviceIDs)
	} else {
		totalQuery = totalQuery.Where("workspace_id = ?", accountID(c))
	}
	if err := totalQuery.Count(&total).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event statistics could not be loaded")
		return
	}
	var open int64
	openQuery := a.db.WithContext(c.Request.Context()).Table(Event{}.TableName()).Where("status IN ?", []string{"pending", "processing"})
	if mobile {
		openQuery = openQuery.Where("device_id IN ?", deviceIDs)
	} else {
		openQuery = openQuery.Where("workspace_id = ?", accountID(c))
	}
	if err := openQuery.Count(&open).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "open event statistics could not be loaded")
		return
	}
	var trendRows []struct {
		Bucket string `json:"bucket"`
		Count  int64  `json:"count"`
	}
	trendQuery := a.db.WithContext(c.Request.Context()).Table(Event{}.TableName()).
		Select("to_char(date_trunc(?, occurred_at AT TIME ZONE 'Asia/Shanghai'), ?) AS bucket, COUNT(*) AS count", unit, format).
		Where("occurred_at >= ? AND occurred_at < ?", start, now)
	if mobile {
		trendQuery = trendQuery.Where("device_id IN ?", deviceIDs)
	} else {
		trendQuery = trendQuery.Where("workspace_id = ?", accountID(c))
	}
	if err := trendQuery.Group("bucket").Order("bucket ASC").Scan(&trendRows).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event trend could not be loaded")
		return
	}
	trendMap := make(map[string]int64, len(trendRows))
	for _, row := range trendRows {
		trendMap[row.Bucket] = row.Count
	}
	trend := make([]eventCount, 0, 30)
	if requested == "today" {
		for bucket := startLocal.Truncate(time.Hour); !bucket.After(localNow); bucket = bucket.Add(time.Hour) {
			key := bucket.Format("2006-01-02 15:00")
			trend = append(trend, eventCount{Key: key, Count: trendMap[key]})
		}
	} else {
		for bucket := startLocal; !bucket.After(localNow); bucket = bucket.AddDate(0, 0, 1) {
			key := bucket.Format("2006-01-02")
			trend = append(trend, eventCount{Key: key, Count: trendMap[key]})
		}
	}
	bySeverity, err := a.groupEventCounts(c, "severity", start, now, deviceIDs, mobile)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "severity statistics could not be loaded")
		return
	}
	byType, err := a.groupEventCounts(c, "event_type", start, now, deviceIDs, mobile)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "type statistics could not be loaded")
		return
	}
	byLocation, err := a.groupEventCounts(c, "location", start, now, deviceIDs, mobile)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "location statistics could not be loaded")
		return
	}
	byStatus, err := a.groupEventCounts(c, "status", start, now, deviceIDs, mobile)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "status statistics could not be loaded")
		return
	}
	bySeverity = withKnownKeys(bySeverity, []string{"low", "medium", "high", "critical"})
	byStatus = withKnownKeys(byStatus, []string{"pending", "processing", "resolved", "false_positive"})
	c.JSON(http.StatusOK, gin.H{"range": requested, "from": start, "to": now, "as_of": now, "total_events": total,
		"open_events": open, "trend": trend, "by_severity": bySeverity, "by_type": topFiveWithOther(byType),
		"by_location": topFiveWithOther(byLocation), "by_status": byStatus})
}

func (a *App) groupEventCounts(c *gin.Context, column string, start, end time.Time, deviceIDs []uuid.UUID, mobile bool) ([]eventCount, error) {
	allowed := map[string]bool{"severity": true, "event_type": true, "location": true, "status": true}
	if !allowed[column] {
		return nil, fmt.Errorf("unsupported event statistics column")
	}
	var rows []eventCount
	query := a.db.WithContext(c.Request.Context()).Table(Event{}.TableName()).Select(column+" AS key, COUNT(*) AS count").
		Where("occurred_at >= ? AND occurred_at < ?", start, end)
	if mobile {
		query = query.Where("device_id IN ?", deviceIDs)
	} else {
		query = query.Where("workspace_id = ?", accountID(c))
	}
	err := query.Group(column).Order("count DESC, key ASC").Find(&rows).Error
	return rows, err
}

func topFiveWithOther(rows []eventCount) []eventCount {
	if len(rows) <= 5 {
		return rows
	}
	other := int64(0)
	for _, row := range rows[5:] {
		other += row.Count
	}
	result := append([]eventCount(nil), rows[:5]...)
	result = append(result, eventCount{Key: "other", Count: other})
	return result
}

func withKnownKeys(rows []eventCount, keys []string) []eventCount {
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Key] = row.Count
	}
	result := make([]eventCount, 0, len(keys))
	for _, key := range keys {
		result = append(result, eventCount{Key: key, Count: counts[key]})
	}
	return result
}
