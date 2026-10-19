package main

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AlgorithmOwnershipSnapshot struct {
	AlgorithmKey     string `json:"algorithm_key"`
	AlgorithmVersion string `json:"algorithm_version"`
	RuntimeState     string `json:"runtime_state"`
	ReportedVersion  int64  `json:"reported_version"`
}

type CameraOwnershipSnapshot struct {
	DeviceID       uuid.UUID                    `json:"device_id"`
	CameraID       uuid.UUID                    `json:"camera_id"`
	AccountID      uuid.UUID                    `json:"account_id"`
	DeviceName     string                       `json:"device_name"`
	DeviceLocation string                       `json:"device_location"`
	CameraName     string                       `json:"camera_name"`
	Algorithms     []AlgorithmOwnershipSnapshot `json:"algorithms"`
}

func (a *App) resolveCameraOwnership(c *gin.Context) {
	deviceID, err := uuid.Parse(c.Param("device_id"))
	if err != nil || deviceID == uuid.Nil {
		writeError(c, http.StatusNotFound, "device_not_found", "device or camera was not found")
		return
	}
	cameraID, err := uuid.Parse(c.Param("camera_id"))
	if err != nil || cameraID == uuid.Nil {
		writeError(c, http.StatusNotFound, "camera_not_found", "device or camera was not found")
		return
	}
	var device Device
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND active = ?", deviceID, true).Take(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "device_not_found", "device or camera was not found")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device ownership could not be verified")
		return
	}
	var camera Camera
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND device_id = ? AND account_id = ? AND active = ?", cameraID, device.ID, device.AccountID, true).Take(&camera).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "camera_not_found", "device or camera was not found")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "camera ownership could not be verified")
		return
	}
	var assignments []AlgorithmAssignment
	if err := a.db.WithContext(c.Request.Context()).Where("device_id = ? AND account_id = ?", device.ID, device.AccountID).Order("algorithm_key ASC").Find(&assignments).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "algorithm ownership could not be verified")
		return
	}
	algorithms := make([]AlgorithmOwnershipSnapshot, 0, len(assignments))
	for _, assignment := range assignments {
		algorithms = append(algorithms, AlgorithmOwnershipSnapshot{AlgorithmKey: assignment.AlgorithmKey,
			AlgorithmVersion: assignment.AlgorithmVersion, RuntimeState: assignment.RuntimeState, ReportedVersion: assignment.ReportedVersion})
	}
	c.JSON(http.StatusOK, CameraOwnershipSnapshot{DeviceID: device.ID, CameraID: camera.ID, AccountID: device.AccountID,
		DeviceName: device.Name, DeviceLocation: device.Location, CameraName: camera.Name, Algorithms: algorithms})
}

func (a *App) resolveCameraOwnershipByID(c *gin.Context) {
	cameraID, err := uuid.Parse(c.Param("camera_id"))
	if err != nil || cameraID == uuid.Nil {
		writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
		return
	}
	var camera Camera
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND active = ?", cameraID, true).Take(&camera).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "camera_not_found", "camera was not found")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "camera ownership could not be verified")
		return
	}
	var device Device
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND account_id = ? AND active = ?", camera.DeviceID, camera.AccountID, true).Take(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "device_not_found", "camera was not found")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device ownership could not be verified")
		return
	}
	var assignments []AlgorithmAssignment
	if err := a.db.WithContext(c.Request.Context()).Where("device_id = ? AND account_id = ?", device.ID, device.AccountID).Order("algorithm_key ASC").Find(&assignments).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "algorithm ownership could not be verified")
		return
	}
	algorithms := make([]AlgorithmOwnershipSnapshot, 0, len(assignments))
	for _, assignment := range assignments {
		algorithms = append(algorithms, AlgorithmOwnershipSnapshot{AlgorithmKey: assignment.AlgorithmKey,
			AlgorithmVersion: assignment.AlgorithmVersion, RuntimeState: assignment.RuntimeState, ReportedVersion: assignment.ReportedVersion})
	}
	c.JSON(http.StatusOK, CameraOwnershipSnapshot{DeviceID: device.ID, CameraID: camera.ID, AccountID: device.AccountID,
		DeviceName: device.Name, DeviceLocation: device.Location, CameraName: camera.Name, Algorithms: algorithms})
}
