package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type memberView struct {
	ID        uuid.UUID              `json:"member_id"`
	AccountID uuid.UUID              `json:"account_id"`
	Username  string                 `json:"username"`
	Status    string                 `json:"status"`
	JoinedAt  time.Time              `json:"joined_at"`
	Grants    []invitationGrantDraft `json:"device_grants"`
}

func (a *App) getWorkspace(c *gin.Context) {
	_, _, account, ok := a.loadSession(c, false)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return
	}
	var workspace Workspace
	var member WorkspaceMember
	if account.UserType == "pc" {
		if err := a.db.WithContext(c.Request.Context()).Table(Workspace{}.TableName()).
			Where("owner_account_id = ? AND status = ?", account.ID, "active").Take(&workspace).Error; err != nil {
			workspaceUnavailable(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"workspace_id": workspace.ID, "name": workspace.Name, "status": workspace.Status, "role": "owner"})
		return
	}
	if account.UserType != "mobile" {
		writeError(c, http.StatusForbidden, "forbidden", "this account has no workspace membership")
		return
	}
	if err := a.db.WithContext(c.Request.Context()).Table(WorkspaceMember{}.TableName()).
		Where("account_id = ? AND status = ?", account.ID, "active").Take(&member).Error; err != nil {
		workspaceUnavailable(c)
		return
	}
	if err := a.db.WithContext(c.Request.Context()).Table(Workspace{}.TableName()+" AS workspaces").
		Joins("JOIN auth.accounts AS owners ON owners.id = workspaces.owner_account_id").
		Where("workspaces.id = ? AND workspaces.status = ? AND owners.status = ?", member.WorkspaceID, "active", "active").Take(&workspace).Error; err != nil {
		workspaceUnavailable(c)
		return
	}
	var grants []MemberDeviceGrant
	if err := a.db.WithContext(c.Request.Context()).Table(MemberDeviceGrant{}.TableName()).
		Where("member_id = ?", member.ID).Order("device_id ASC, capability ASC").Find(&grants).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace permissions could not be loaded")
		return
	}
	byDevice := make(map[uuid.UUID][]string)
	for _, grant := range grants {
		byDevice[grant.DeviceID] = append(byDevice[grant.DeviceID], grant.Capability)
	}
	items := make([]invitationGrantDraft, 0, len(byDevice))
	for deviceID, capabilities := range byDevice {
		items = append(items, invitationGrantDraft{DeviceID: deviceID.String(), Capabilities: capabilities})
	}
	c.JSON(http.StatusOK, gin.H{"workspace_id": workspace.ID, "name": workspace.Name, "status": workspace.Status,
		"role": "member", "member_id": member.ID, "device_grants": items})
}

func (a *App) listWorkspaceMembers(c *gin.Context) {
	_, _, _, workspace, ok := a.loadOwnerWorkspace(c, false)
	if !ok {
		return
	}
	var members []WorkspaceMember
	if err := a.db.WithContext(c.Request.Context()).Table(WorkspaceMember{}.TableName()).
		Where("workspace_id = ?", workspace.ID).Order("created_at ASC").Find(&members).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace members could not be loaded")
		return
	}
	items := make([]memberView, 0, len(members))
	for _, member := range members {
		var account Account
		if err := a.db.WithContext(c.Request.Context()).Table(Account{}.TableName()).Where("id = ?", member.AccountID).Take(&account).Error; err != nil {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace members could not be loaded")
			return
		}
		var grants []MemberDeviceGrant
		if err := a.db.WithContext(c.Request.Context()).Table(MemberDeviceGrant{}.TableName()).
			Where("member_id = ?", member.ID).Order("device_id ASC, capability ASC").Find(&grants).Error; err != nil {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace permissions could not be loaded")
			return
		}
		byDevice := make(map[uuid.UUID][]string)
		for _, grant := range grants {
			byDevice[grant.DeviceID] = append(byDevice[grant.DeviceID], grant.Capability)
		}
		deviceGrants := make([]invitationGrantDraft, 0, len(byDevice))
		for deviceID, capabilities := range byDevice {
			deviceGrants = append(deviceGrants, invitationGrantDraft{DeviceID: deviceID.String(), Capabilities: capabilities})
		}
		items = append(items, memberView{ID: member.ID, AccountID: member.AccountID, Username: account.Username,
			Status: member.Status, JoinedAt: member.JoinedAt, Grants: deviceGrants})
	}
	c.JSON(http.StatusOK, gin.H{"workspace_id": workspace.ID, "items": items})
}

func (a *App) replaceMemberGrants(c *gin.Context) {
	_, session, owner, workspace, ok := a.loadOwnerWorkspace(c, true)
	if !ok {
		return
	}
	memberID, err := uuid.Parse(c.Param("member_id"))
	if err != nil || memberID == uuid.Nil {
		writeError(c, http.StatusNotFound, "not_found", "workspace member was not found")
		return
	}
	var body createInvitationRequest
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "device_grants must be a valid array")
		return
	}
	grants, err := normalizeGrantDrafts(body.DeviceGrants)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_grants", "device grants are invalid")
		return
	}
	owned, err := a.verifyDeviceOwnership(c.Request.Context(), owner.ID, grants)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device ownership could not be verified")
		return
	}
	if !owned {
		writeError(c, http.StatusUnprocessableEntity, "invalid_grants", "every granted device must be active and owned by this workspace")
		return
	}
	now := time.Now().UTC()
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var member WorkspaceMember
		if err := tx.Table(WorkspaceMember{}.TableName()).Where("id = ? AND workspace_id = ? AND status = ?",
			memberID, workspace.ID, "active").Take(&member).Error; err != nil {
			return err
		}
		if err := tx.Table(MemberDeviceGrant{}.TableName()).Where("member_id = ?", member.ID).Delete(&MemberDeviceGrant{}).Error; err != nil {
			return err
		}
		for _, draft := range grants {
			deviceID, _ := uuid.Parse(draft.DeviceID)
			for _, capability := range draft.Capabilities {
				grant := MemberDeviceGrant{ID: uuid.New(), MemberID: member.ID, DeviceID: deviceID, Capability: capability, CreatedAt: now}
				if err := tx.Table(MemberDeviceGrant{}.TableName()).Create(&grant).Error; err != nil {
					return err
				}
			}
		}
		audit := AuditLog{ID: uuid.New(), ActorID: owner.ID, WorkspaceID: workspace.ID, Action: "member.grants.replaced",
			ResourceID: member.ID.String(), Result: "success", RequestID: c.GetString("request_id"), CreatedAt: now}
		return tx.Table(AuditLog{}.TableName()).Create(&audit).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "workspace member was not found")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace permissions could not be updated")
		return
	}
	_ = session
	c.Status(http.StatusNoContent)
}

func (a *App) revokeWorkspaceMember(c *gin.Context) {
	_, _, owner, workspace, ok := a.loadOwnerWorkspace(c, true)
	if !ok {
		return
	}
	memberID, err := uuid.Parse(c.Param("member_id"))
	if err != nil || memberID == uuid.Nil {
		c.Status(http.StatusNoContent)
		return
	}
	now := time.Now().UTC()
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var member WorkspaceMember
		if err := tx.Table(WorkspaceMember{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND workspace_id = ? AND status = ?", memberID, workspace.ID, "active").Take(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if err := tx.Table(WorkspaceMember{}.TableName()).Where("id = ? AND status = ?", member.ID, "active").
			Updates(map[string]any{"status": "revoked", "revoked_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Table(MemberDeviceGrant{}.TableName()).Where("member_id = ?", member.ID).Delete(&MemberDeviceGrant{}).Error; err != nil {
			return err
		}
		audit := AuditLog{ID: uuid.New(), ActorID: owner.ID, WorkspaceID: workspace.ID, Action: "member.revoked",
			ResourceID: member.ID.String(), Result: "success", RequestID: c.GetString("request_id"), CreatedAt: now}
		return tx.Table(AuditLog{}.TableName()).Create(&audit).Error
	})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace member could not be revoked")
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *App) listWorkspaceInvitations(c *gin.Context) {
	_, _, _, workspace, ok := a.loadOwnerWorkspace(c, false)
	if !ok {
		return
	}
	var invitations []WorkspaceInvitation
	if err := a.db.WithContext(c.Request.Context()).Table(WorkspaceInvitation{}.TableName()).
		Select("id, workspace_id, created_by, expires_at, used_at, revoked_at, created_at").
		Where("workspace_id = ?", workspace.ID).Order("created_at DESC").Limit(200).Find(&invitations).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "workspace invitations could not be loaded")
		return
	}
	items := make([]gin.H, 0, len(invitations))
	for _, invitation := range invitations {
		items = append(items, gin.H{"invitation_id": invitation.ID, "workspace_id": invitation.WorkspaceID,
			"created_by": invitation.CreatedBy, "expires_at": invitation.ExpiresAt, "used_at": invitation.UsedAt,
			"revoked_at": invitation.RevokedAt, "created_at": invitation.CreatedAt})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *App) revokeWorkspaceInvitation(c *gin.Context) {
	_, _, owner, workspace, ok := a.loadOwnerWorkspace(c, true)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("invitation_id"))
	if err != nil || id == uuid.Nil {
		c.Status(http.StatusNoContent)
		return
	}
	now := time.Now().UTC()
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Table(WorkspaceInvitation{}.TableName()).Where("id = ? AND workspace_id = ? AND used_at IS NULL AND revoked_at IS NULL", id, workspace.ID).
			Update("revoked_at", now)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		audit := AuditLog{ID: uuid.New(), ActorID: owner.ID, WorkspaceID: workspace.ID, Action: "invitation.revoked",
			ResourceID: id.String(), Result: "success", RequestID: c.GetString("request_id"), CreatedAt: now}
		return tx.Table(AuditLog{}.TableName()).Create(&audit).Error
	})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "invitation could not be revoked")
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *App) loadOwnerWorkspace(c *gin.Context, mutation bool) (string, Session, Account, Workspace, bool) {
	cookie, session, account, ok := a.loadSession(c, false)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
		return "", Session{}, Account{}, Workspace{}, false
	}
	if mutation && !a.validCSRF(c, session) {
		writeError(c, http.StatusForbidden, "csrf_rejected", "valid CSRF token required")
		return "", Session{}, Account{}, Workspace{}, false
	}
	if account.UserType != "pc" {
		writeError(c, http.StatusForbidden, "forbidden", "only a PC workspace owner can manage members")
		return "", Session{}, Account{}, Workspace{}, false
	}
	var workspace Workspace
	if err := a.db.WithContext(c.Request.Context()).Table(Workspace{}.TableName()).
		Where("owner_account_id = ? AND status = ?", account.ID, "active").Take(&workspace).Error; err != nil {
		workspaceUnavailable(c)
		return "", Session{}, Account{}, Workspace{}, false
	}
	return cookie, session, account, workspace, true
}

func workspaceUnavailable(c *gin.Context) {
	writeError(c, http.StatusForbidden, "workspace_unavailable", "workspace is not available")
}
