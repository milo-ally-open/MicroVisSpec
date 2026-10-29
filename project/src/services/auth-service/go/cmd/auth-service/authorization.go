package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type checkAuthorizationRequest struct {
	AccountID  string `json:"account_id"`
	SessionID  string `json:"session_id"`
	DeviceID   string `json:"device_id"`
	Capability string `json:"capability"`
}

func (a *App) listAlertRecipients(c *gin.Context) {
	var body struct {
		DeviceID string `json:"device_id"`
		Cursor   string `json:"cursor"`
		Limit    int    `json:"limit"`
	}
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "recipient request is invalid")
		return
	}
	deviceID, err := uuid.Parse(body.DeviceID)
	if err != nil || deviceID == uuid.Nil || body.Limit < 1 || body.Limit > 100 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "device_id and limit are required")
		return
	}
	var cursorID uuid.UUID
	if body.Cursor != "" {
		cursorID, err = uuid.Parse(body.Cursor)
		if err != nil || cursorID == uuid.Nil {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "recipient cursor is invalid")
			return
		}
	}
	device, found, err := a.getDeviceOwnership(c.Request.Context(), deviceID)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device ownership could not be verified")
		return
	}
	if !found || !device.Active || device.DeviceID != deviceID || device.AccountID == uuid.Nil {
		c.JSON(http.StatusOK, gin.H{"account_ids": []uuid.UUID{}, "next_cursor": ""})
		return
	}
	var workspace Workspace
	if err := a.db.WithContext(c.Request.Context()).Table(Workspace{}.TableName()+" AS workspaces").
		Joins("JOIN auth.accounts AS owners ON owners.id = workspaces.owner_account_id").
		Where("workspaces.owner_account_id = ? AND workspaces.status = ? AND owners.status = ?", device.AccountID, "active", "active").
		Take(&workspace).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusOK, gin.H{"account_ids": []uuid.UUID{}, "next_cursor": ""})
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace authorization could not be checked")
		return
	}
	query := a.db.WithContext(c.Request.Context()).Table(WorkspaceMember{}.TableName()+" AS members").
		Distinct("members.account_id").
		Joins("JOIN auth.accounts AS accounts ON accounts.id = members.account_id").
		Joins("JOIN auth.workspace_member_device_grants AS grants ON grants.member_id = members.id").
		Where("members.workspace_id = ? AND members.status = ? AND accounts.status = ? AND accounts.user_type = ? AND grants.device_id = ? AND grants.capability = ?",
			workspace.ID, "active", "active", "mobile", deviceID, "alert.receive")
	if cursorID != uuid.Nil {
		query = query.Where("members.account_id > ?", cursorID)
	}
	var accountIDs []uuid.UUID
	if err := query.Order("members.account_id ASC").Limit(body.Limit+1).Pluck("members.account_id", &accountIDs).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "alert recipients could not be loaded")
		return
	}
	nextCursor := ""
	if len(accountIDs) > body.Limit {
		accountIDs = accountIDs[:body.Limit]
		nextCursor = accountIDs[len(accountIDs)-1].String()
	}
	c.JSON(http.StatusOK, gin.H{"account_ids": accountIDs, "next_cursor": nextCursor})
}

func (a *App) listAuthorizedDevices(c *gin.Context) {
	var body struct {
		AccountID  string `json:"account_id"`
		SessionID  string `json:"session_id"`
		Capability string `json:"capability"`
		Cursor     string `json:"cursor"`
		Limit      int    `json:"limit"`
	}
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "authorization request is invalid")
		return
	}
	accountID, accountErr := uuid.Parse(body.AccountID)
	sessionID, sessionErr := uuid.Parse(body.SessionID)
	if accountErr != nil || sessionErr != nil || accountID == uuid.Nil || sessionID == uuid.Nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "authorization identity is required")
		return
	}
	if _, ok := allowedDeviceCapabilities[body.Capability]; !ok {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "capability is unsupported")
		return
	}
	if body.Limit < 1 || body.Limit > 100 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "limit must be between 1 and 100")
		return
	}
	var cursorID uuid.UUID
	if body.Cursor != "" {
		var err error
		cursorID, err = uuid.Parse(body.Cursor)
		if err != nil || cursorID == uuid.Nil {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "device cursor is invalid")
			return
		}
	}
	now := time.Now().UTC()
	var session Session
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND account_id = ? AND revoked_at IS NULL AND expires_at > ?",
		sessionID, accountID, now).Take(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusForbidden, "authorization_denied", "session is not authorized")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "authorization could not be checked")
		return
	}
	var account Account
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND user_type = ? AND status = ?", accountID, "mobile", "active").Take(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusForbidden, "authorization_denied", "active mobile membership is required")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "authorization could not be checked")
		return
	}
	var member WorkspaceMember
	if err := a.db.WithContext(c.Request.Context()).Where("account_id = ? AND status = ?", account.ID, "active").Take(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusForbidden, "authorization_denied", "active mobile membership is required")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace membership could not be checked")
		return
	}
	var workspace Workspace
	if err := a.db.WithContext(c.Request.Context()).Table(Workspace{}.TableName()+" AS workspaces").
		Joins("JOIN auth.accounts AS owners ON owners.id = workspaces.owner_account_id").
		Where("workspaces.id = ? AND workspaces.status = ? AND owners.status = ?", member.WorkspaceID, "active", "active").
		Take(&workspace).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusForbidden, "authorization_denied", "workspace is not authorized")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace authorization could not be checked")
		return
	}
	grantQuery := a.db.WithContext(c.Request.Context()).Table(MemberDeviceGrant{}.TableName()).
		Distinct("device_id").Where("member_id = ? AND capability = ?", member.ID, body.Capability)
	if cursorID != uuid.Nil {
		grantQuery = grantQuery.Where("device_id > ?", cursorID)
	}
	var deviceIDs []uuid.UUID
	if err := grantQuery.Order("device_id ASC").Limit(body.Limit+1).Pluck("device_id", &deviceIDs).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device grants could not be loaded")
		return
	}
	nextCursor := ""
	if len(deviceIDs) > body.Limit {
		deviceIDs = deviceIDs[:body.Limit]
		nextCursor = deviceIDs[len(deviceIDs)-1].String()
	}
	c.JSON(http.StatusOK, gin.H{"workspace_id": workspace.ID, "device_ids": deviceIDs, "next_cursor": nextCursor})
}

func (a *App) checkAuthorization(c *gin.Context) {
	var body checkAuthorizationRequest
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "authorization request is invalid")
		return
	}
	accountID, accountErr := uuid.Parse(body.AccountID)
	sessionID, sessionErr := uuid.Parse(body.SessionID)
	deviceID, deviceErr := uuid.Parse(body.DeviceID)
	if accountErr != nil || sessionErr != nil || deviceErr != nil || accountID == uuid.Nil || sessionID == uuid.Nil || deviceID == uuid.Nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "authorization identity and resource identifiers are required")
		return
	}
	if _, ok := allowedDeviceCapabilities[body.Capability]; !ok {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "capability is unsupported")
		return
	}
	now := time.Now().UTC()
	var session Session
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND account_id = ? AND revoked_at IS NULL AND expires_at > ?",
		sessionID, accountID, now).Take(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusForbidden, "authorization_denied", "session is not authorized")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "authorization could not be checked")
		return
	}
	var account Account
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND status = ?", accountID, "active").Take(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusForbidden, "authorization_denied", "account is not authorized")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "authorization could not be checked")
		return
	}
	device, found, err := a.getDeviceOwnership(c.Request.Context(), deviceID)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device ownership could not be verified")
		return
	}
	if !found || !device.Active || device.DeviceID != deviceID || device.AccountID == uuid.Nil {
		writeError(c, http.StatusForbidden, "authorization_denied", "device is not authorized")
		return
	}
	var workspace Workspace
	if err := a.db.WithContext(c.Request.Context()).Table(Workspace{}.TableName()+" AS workspaces").
		Joins("JOIN auth.accounts AS owners ON owners.id = workspaces.owner_account_id").
		Where("workspaces.owner_account_id = ? AND workspaces.status = ? AND owners.status = ?", device.AccountID, "active", "active").
		Take(&workspace).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusForbidden, "authorization_denied", "workspace is not authorized")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace authorization could not be checked")
		return
	}
	if account.UserType == "pc" && account.ID == workspace.OwnerAccountID {
		c.Status(http.StatusNoContent)
		return
	}
	if account.UserType != "mobile" {
		writeError(c, http.StatusForbidden, "authorization_denied", "account role is not authorized for workspace resources")
		return
	}
	var member WorkspaceMember
	if err := a.db.WithContext(c.Request.Context()).Where("account_id = ? AND workspace_id = ? AND status = ?",
		account.ID, workspace.ID, "active").Take(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusForbidden, "authorization_denied", "workspace membership is not active")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace membership could not be checked")
		return
	}
	var grant MemberDeviceGrant
	if err := a.db.WithContext(c.Request.Context()).Where("member_id = ? AND device_id = ? AND capability = ?",
		member.ID, deviceID, body.Capability).Take(&grant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusForbidden, "authorization_denied", "device capability is not granted")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device capability could not be checked")
		return
	}
	c.Status(http.StatusNoContent)
}
