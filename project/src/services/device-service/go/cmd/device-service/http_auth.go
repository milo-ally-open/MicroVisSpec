package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (a *App) authenticateDeviceHTTP(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("device_id"))
	if err != nil || deviceID == uuid.Nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decodeJSON(c, &input) != nil || len(input.Username) > 120 || len(input.Password) < 32 || len(input.Password) > 256 {
		writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
		return
	}
	var device Device
	err = a.db.WithContext(c.Request.Context()).Table(Device{}.TableName()).Where("id = ? AND active = ? AND (activation_expires_at IS NULL OR activation_expires_at > ?)", deviceID, true, time.Now().UTC()).Take(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device credentials could not be verified")
		return
	}
	var credential BrokerCredential
	err = a.db.WithContext(c.Request.Context()).Table(BrokerCredential{}.TableName()).
		Where("device_id = ? AND revoked_at IS NULL", deviceID).Take(&credential).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device credentials could not be verified")
		return
	}
	if !constantTimeEqual(input.Username, credential.Username) || !constantTimeEqual(hashOpaque(input.Password), credential.PasswordHash) {
		writeError(c, http.StatusUnauthorized, "unauthorized", "device credentials are invalid")
		return
	}
	c.JSON(http.StatusOK, gin.H{"allowed": true, "device_id": deviceID})
}
