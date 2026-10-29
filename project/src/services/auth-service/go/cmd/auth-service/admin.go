package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type adminPrincipal struct {
	Account Account
	Session Session
}

func (a *App) bootstrapAdmin(c *gin.Context) {
	if a.config.AdminBootstrapToken == "" || !constantTimeEqual(c.GetHeader("X-Admin-Bootstrap-Token"), a.config.AdminBootstrapToken) {
		writeError(c, http.StatusNotFound, "not_found", "bootstrap is unavailable")
		return
	}
	var body credentials
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "invalid request body")
		return
	}
	username := strings.TrimSpace(body.Username)
	normalized := strings.ToLower(username)
	if !validUsername(normalized) || len(body.Password) < 12 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_credentials", "username or password does not meet requirements")
		return
	}
	passwordHash, err := hashPassword(body.Password)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	now := time.Now().UTC()
	account := Account{ID: uuid.New(), Username: username, NormalizedUser: normalized, PasswordHash: passwordHash,
		UserType: "admin", Status: "active", CreatedAt: now, UpdatedAt: now}
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(718292611)").Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Table(Account{}.TableName()).Where("user_type = ?", "admin").Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errAdminBootstrapConsumed
		}
		if err := tx.Table(Account{}.TableName()).Create(&account).Error; err != nil {
			return err
		}
		audit := AuditLog{ID: uuid.New(), ActorID: account.ID, WorkspaceID: uuid.Nil, Action: "admin.bootstrap.completed",
			ResourceID: account.ID.String(), Result: "success", RequestID: c.GetString("request_id"), CreatedAt: now}
		return tx.Table(AuditLog{}.TableName()).Create(&audit).Error
	})
	if err != nil {
		if errors.Is(err, errAdminBootstrapConsumed) {
			writeError(c, http.StatusConflict, "bootstrap_consumed", "an administrator has already been initialized")
			return
		}
		if isUniqueViolation(err) {
			writeError(c, http.StatusConflict, "username_unavailable", "username is already in use")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "administrator could not be initialized")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"account_id": account.ID, "username": account.Username, "user_type": account.UserType})
}

func (a *App) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, session, account, ok := a.loadSession(c, false)
		if !ok {
			writeError(c, http.StatusUnauthorized, "unauthorized", "a valid session is required")
			c.Abort()
			return
		}
		if account.UserType != "admin" {
			writeError(c, http.StatusForbidden, "forbidden", "administrator access is required")
			c.Abort()
			return
		}
		c.Set("admin_account", account)
		c.Set("admin_session", session)
		c.Next()
	}
}

func (a *App) requireAdminCSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := c.MustGet("admin_session").(Session)
		if !a.validCSRF(c, session) {
			writeError(c, http.StatusForbidden, "csrf_rejected", "valid CSRF token required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func adminAccount(c *gin.Context) Account { return c.MustGet("admin_account").(Account) }

func (a *App) listAdminAccounts(c *gin.Context) {
	var accounts []Account
	if err := a.db.WithContext(c.Request.Context()).Table(Account{}.TableName()).
		Select("id, username, user_type, status, created_at, updated_at").Order("created_at DESC, id DESC").Limit(500).Find(&accounts).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "accounts could not be loaded")
		return
	}
	type item struct {
		ID        uuid.UUID `json:"account_id"`
		Username  string    `json:"username"`
		UserType  string    `json:"user_type"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	items := make([]item, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, item{ID: account.ID, Username: account.Username, UserType: account.UserType,
			Status: account.Status, CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "truncated": len(items) == 500})
}

func (a *App) createAdminManagedPC(c *gin.Context) {
	var body credentials
	if err := decodeJSON(c, &body); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "invalid request body")
		return
	}
	username := strings.TrimSpace(body.Username)
	normalized := strings.ToLower(username)
	if !validUsername(normalized) || len(body.Password) < 12 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_credentials", "username or password does not meet requirements")
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	now := time.Now().UTC()
	account := Account{ID: uuid.New(), Username: username, NormalizedUser: normalized, PasswordHash: hash,
		UserType: "pc", Status: "active", CreatedAt: now, UpdatedAt: now}
	workspace := Workspace{ID: account.ID, OwnerAccountID: account.ID, Name: account.Username,
		Status: "active", CreatedAt: now, UpdatedAt: now}
	admin := adminAccount(c)
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(Account{}.TableName()).Create(&account).Error; err != nil {
			return err
		}
		if err := tx.Table(Workspace{}.TableName()).Create(&workspace).Error; err != nil {
			return err
		}
		audit := AuditLog{ID: uuid.New(), ActorID: admin.ID, WorkspaceID: workspace.ID, Action: "admin.pc_account.created",
			ResourceID: account.ID.String(), Result: "success", RequestID: c.GetString("request_id"), CreatedAt: now}
		return tx.Table(AuditLog{}.TableName()).Create(&audit).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(c, http.StatusConflict, "username_unavailable", "username is already in use")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "PC account could not be created")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"account_id": account.ID, "username": account.Username, "user_type": account.UserType, "status": account.Status})
}

func (a *App) setAdminAccountStatus(c *gin.Context) {
	targetID, err := uuid.Parse(c.Param("account_id"))
	if err != nil || targetID == uuid.Nil {
		writeError(c, http.StatusNotFound, "not_found", "account was not found")
		return
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(c, &body); err != nil || (body.Status != "active" && body.Status != "suspended") || len(strings.TrimSpace(body.Reason)) > 240 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "status must be active or suspended and reason must be at most 240 characters")
		return
	}
	admin := adminAccount(c)
	now := time.Now().UTC()
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var target Account
		if err := tx.Table(Account{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_type <> ?", targetID, "admin").Take(&target).Error; err != nil {
			return err
		}
		if target.Status == body.Status {
			return nil
		}
		if err := tx.Table(Account{}.TableName()).Where("id = ?", target.ID).Updates(map[string]any{"status": body.Status, "updated_at": now}).Error; err != nil {
			return err
		}
		workspaceID := target.ID
		if target.UserType == "pc" {
			workspaceStatus := "active"
			if body.Status == "suspended" {
				workspaceStatus = "restricted"
			}
			if err := tx.Table(Workspace{}.TableName()).Where("owner_account_id = ?", target.ID).
				Updates(map[string]any{"status": workspaceStatus, "updated_at": now}).Error; err != nil {
				return err
			}
		} else {
			var member WorkspaceMember
			if err := tx.Table(WorkspaceMember{}.TableName()).Where("account_id = ?", target.ID).Take(&member).Error; err == nil {
				workspaceID = member.WorkspaceID
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if body.Status == "suspended" {
			var sessions []Session
			if err := tx.Table(Session{}.TableName()).Where("account_id = ? AND revoked_at IS NULL", target.ID).Find(&sessions).Error; err != nil {
				return err
			}
			for _, session := range sessions {
				if err := tx.Table(Session{}.TableName()).Where("id = ? AND revoked_at IS NULL", session.ID).Update("revoked_at", now).Error; err != nil {
					return err
				}
				if err := writeRevocation(tx, session, now); err != nil {
					return err
				}
			}
		}
		audit := AuditLog{ID: uuid.New(), ActorID: admin.ID, WorkspaceID: workspaceID, Action: "admin.account.status_changed",
			ResourceID: target.ID.String(), Result: "success", Reason: strings.TrimSpace(body.Reason), RequestID: c.GetString("request_id"), CreatedAt: now}
		return tx.Table(AuditLog{}.TableName()).Create(&audit).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "account was not found")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "account status could not be changed")
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *App) listAdminAudit(c *gin.Context) {
	var entries []AuditLog
	if err := a.db.WithContext(c.Request.Context()).Table(AuditLog{}.TableName()).Order("created_at DESC, id DESC").Limit(500).Find(&entries).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "audit records could not be loaded")
		return
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].CreatedAt.After(entries[j].CreatedAt) })
	c.JSON(http.StatusOK, gin.H{"items": entries, "truncated": len(entries) == 500})
}

var errAdminBootstrapConsumed = errors.New("administrator bootstrap has already been consumed")

type readinessSnapshot struct {
	Status       string            `json:"status"`
	Dependencies map[string]string `json:"dependencies"`
}

type serviceHealth struct {
	Name         string            `json:"name"`
	Status       string            `json:"status"`
	CheckedAt    time.Time         `json:"checked_at"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}

func (a *App) adminHealth(c *gin.Context) {
	targets := []struct{ name, baseURL string }{
		{"device-service", a.config.DeviceServiceURL}, {"event-service", a.config.EventServiceURL},
		{"media-service", a.config.MediaServiceURL}, {"push-service", a.config.PushServiceURL},
	}
	results := make([]serviceHealth, len(targets)+1)
	results[0] = serviceHealth{Name: "auth-service", Status: "unknown", CheckedAt: time.Now().UTC()}
	sqlDB, err := a.db.DB()
	if err == nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		err = sqlDB.PingContext(ctx)
		cancel()
	}
	if err == nil {
		results[0].Status = "ready"
	} else {
		results[0].Status = "unavailable"
	}
	dependencies := map[string]string{"postgres": results[0].Status, "mqtt": "unknown", "mediamtx": "unknown", "web_push": "unknown"}
	type probeResult struct {
		index  int
		result serviceHealth
	}
	ch := make(chan probeResult, len(targets))
	for i, target := range targets {
		go func(index int, name, baseURL string) {
			ch <- probeResult{index: index + 1, result: probeReadiness(c.Request.Context(), name, baseURL)}
		}(i, target.name, target.baseURL)
	}
	for range targets {
		probe := <-ch
		results[probe.index] = probe.result
		for key, value := range probe.result.Dependencies {
			normalized := strings.ToLower(key)
			if normalized == "postgres" {
				dependencies["postgres"] = mergeHealth(dependencies["postgres"], value)
			}
			if strings.Contains(normalized, "mqtt") {
				dependencies["mqtt"] = mergeHealth(dependencies["mqtt"], value)
			}
			if strings.Contains(normalized, "mediamtx") {
				dependencies["mediamtx"] = mergeHealth(dependencies["mediamtx"], value)
			}
			if normalized == "web_push" {
				dependencies["web_push"] = mergeHealth(dependencies["web_push"], value)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"services": results, "dependencies": dependencies, "checked_at": time.Now().UTC()})
}

func probeReadiness(parent context.Context, name, baseURL string) serviceHealth {
	result := serviceHealth{Name: name, Status: "unknown", CheckedAt: time.Now().UTC()}
	if baseURL == "" {
		return result
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/health/ready", nil)
	if err != nil {
		return result
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		result.Status = "unavailable"
		return result
	}
	defer response.Body.Close()
	var snapshot readinessSnapshot
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<10)).Decode(&snapshot); err != nil {
		if response.StatusCode != http.StatusOK {
			result.Status = "unavailable"
		}
		return result
	}
	result.Dependencies = snapshot.Dependencies
	if response.StatusCode == http.StatusOK && snapshot.Status == "ready" {
		result.Status = "ready"
	} else {
		result.Status = "unavailable"
	}
	return result
}

func mergeHealth(current, incoming string) string {
	if incoming != "ready" {
		return "unavailable"
	}
	if current == "unavailable" {
		return current
	}
	return "ready"
}
