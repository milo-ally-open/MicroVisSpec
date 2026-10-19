package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestValidateAsset(t *testing.T) {
	valid := []struct{ name, ip, location string }{
		{"Classroom camera", "192.168.0.2", "Building A / Room 101"},
		{"Edge", "2001:db8::1", "Room 2"},
	}
	for _, tc := range valid {
		if err := validateAsset(tc.name, tc.ip, tc.location); err != nil {
			t.Errorf("validateAsset(%q, %q, %q): %v", tc.name, tc.ip, tc.location, err)
		}
	}
	invalid := []struct{ name, ip, location string }{
		{"", "192.168.0.2", "Room"},
		{"Camera", "not-an-ip", "Room"},
		{"Camera", "192.168.0.2", ""},
		{"Camera", "192.168.0.2", string(make([]byte, 257))},
	}
	for _, tc := range invalid {
		if err := validateAsset(tc.name, tc.ip, tc.location); err == nil {
			t.Errorf("validateAsset(%q, %q, location length %d) unexpectedly succeeded", tc.name, tc.ip, len(tc.location))
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 10, 19, 12, 30, 40, 123456789, time.FixedZone("offset", 8*60*60))
	id := uuid.New()
	token := encodeCursor(createdAt, id)
	gotTime, gotID, err := decodeCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(createdAt) || gotID != id {
		t.Fatalf("cursor round trip mismatch: got %s %s", gotTime, gotID)
	}
	if _, _, err := decodeCursor("not-a-cursor"); err == nil {
		t.Fatal("malformed cursor was accepted")
	}
}

func TestDeviceOnlineRequiresActiveActivationAndFreshHeartbeat(t *testing.T) {
	now := time.Now().UTC()
	last := now.Add(-heartbeatTimeout + time.Second)
	device := Device{Active: true, ActivationState: "active", LastSeenAt: &last}
	if !isOnline(device, now) {
		t.Fatal("fresh activated device should be online")
	}
	last = now.Add(-heartbeatTimeout - time.Second)
	if isOnline(device, now) {
		t.Fatal("expired heartbeat should be offline")
	}
	last = now
	device.ActivationState = "pending"
	if isOnline(device, now) {
		t.Fatal("pending activation must not appear online")
	}
}

func TestProtectedRoutesRequireAccountIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	NewHandler(nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 before any database access, got %d", recorder.Code)
	}
	if !contains(recorder.Body.String(), `"request_id"`) || !contains(recorder.Body.String(), `"error"`) {
		t.Fatalf("error response does not follow common contract: %s", recorder.Body.String())
	}
}

func contains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
