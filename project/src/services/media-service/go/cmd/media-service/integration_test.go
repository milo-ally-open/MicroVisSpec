//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeOwnershipResolver struct {
	ownership CameraOwnership
	err       error
}

func (f fakeOwnershipResolver) ResolveCamera(context.Context, string) (CameraOwnership, error) {
	return f.ownership, f.err
}

type fakeMediaMTX struct {
	applyCalls  int
	deleteCalls int
	kickCalls   int
	lastName    string
	lastSource  string
	online      bool
	err         error
}

func (f *fakeMediaMTX) Check(context.Context) error { return f.err }
func (f *fakeMediaMTX) ApplyPath(_ context.Context, name, source string) error {
	f.applyCalls++
	f.lastName = name
	f.lastSource = source
	return f.err
}
func (f *fakeMediaMTX) DeletePath(_ context.Context, name string) error {
	f.deleteCalls++
	f.lastName = name
	return f.err
}
func (f *fakeMediaMTX) PathState(context.Context, string) (MediaPathState, error) {
	return MediaPathState{Online: f.online}, f.err
}
func (f *fakeMediaMTX) KickLease(context.Context, string, string) error {
	f.kickCalls++
	return f.err
}

func mediaIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for media-service integration tests")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	database := strings.ToLower(strings.Trim(parsed.Path, "/"))
	if !strings.Contains(database, "_it") && !strings.Contains(database, "test") {
		t.Fatalf("refusing integration test against non-test database %q", database)
	}
	db, err := OpenDatabase(dsn)
	if err != nil {
		t.Fatalf("open isolated integration database: %v", err)
	}
	gin.SetMode(gin.TestMode)
	return db
}

func mediaRequest(handler http.Handler, method, path string, accountID uuid.UUID, body any, internalService, internalToken string) *httptest.ResponseRecorder {
	var payload bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&payload).Encode(body)
	}
	request := httptest.NewRequest(method, path, &payload)
	if accountID != uuid.Nil {
		request.Header.Set("X-Account-ID", accountID.String())
		request.Header.Set("X-Session-ID", "session-it")
	}
	if internalService != "" {
		request.Header.Set("X-Internal-Service", internalService)
		request.Header.Set("X-Internal-Service-Token", internalToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func mediaAuthorizationRequest(handler http.Handler, forgedAccountID uuid.UUID, originalURI, internalToken, originalMethod string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/media/authorize-playback", nil)
	request.Header.Set("Cookie", "yunshao_session=opaque-session-cookie")
	request.Header.Set("X-CSRF-Token", "csrf-it")
	request.Header.Set("X-Original-Method", originalMethod)
	request.Header.Set("X-Original-Origin", "https://app.example.test")
	request.Header.Set("X-Account-ID", forgedAccountID.String())
	request.Header.Set("X-Session-ID", "forged-session")
	request.Header.Set("X-Original-URI", originalURI)
	if internalToken != "" {
		request.Header.Set("X-Internal-Service-Token", internalToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestSourceConfigurationChecksOwnershipAndStoresOnlyURL(t *testing.T) {
	db := mediaIntegrationDB(t)
	accountID, deviceID, cameraID := uuid.New(), uuid.New(), uuid.New()
	ownership := CameraOwnership{AccountID: accountID.String(), DeviceID: deviceID.String(), CameraID: cameraID.String(), DeviceName: "Atlas room", DeviceLocation: "Room 102", CameraName: "Front"}
	mtx := &fakeMediaMTX{online: true}
	config := Config{MediaSourceEncryptionKey: bytes.Repeat([]byte{0x81}, 32)}
	app := NewApp(config, db, fakeOwnershipResolver{ownership: ownership}, mtx)
	handler := app.Handler()
	defer db.Where("camera_id = ?", cameraID).Delete(&Stream{})
	input := map[string]string{"rtsp_url": "rtsp://192.168.0.2:8554/processed"}
	response := mediaRequest(handler, http.MethodPut, "/api/v1/media/cameras/"+cameraID.String()+"/source", accountID, input, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("configure stream: status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), input["rtsp_url"]) {
		t.Fatal("source write response exposed the RTSP URL")
	}
	var stream Stream
	if err := db.First(&stream, "camera_id = ?", cameraID).Error; err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stream.EncryptedSource, []byte(input["rtsp_url"])) {
		t.Fatal("database source ciphertext contains the plaintext URL")
	}
	secret, err := decryptMediaSource(config.MediaSourceEncryptionKey, stream.EncryptedSource)
	if err != nil || secret.URL != input["rtsp_url"] || secret.Version != currentMediaSourceVersion {
		t.Fatalf("stored source did not decrypt correctly: %+v err=%v", secret, err)
	}
	if stream.ConfigStatus != "applied" || stream.AppliedConfigVersion != stream.ConfigVersion || mtx.applyCalls != 1 || mtx.lastSource != input["rtsp_url"] {
		t.Fatalf("source path was not applied: stream=%+v mtx=%+v", stream, mtx)
	}
	status := mediaRequest(handler, http.MethodGet, "/api/v1/media/cameras/"+cameraID.String()+"/status", accountID, nil, "", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"rtsp_url":"`+input["rtsp_url"]+`"`) {
		t.Fatalf("camera status did not return the authorized source URL: %d %s", status.Code, status.Body.String())
	}

	otherAccount := uuid.New()
	denied := mediaRequest(handler, http.MethodPut, "/api/v1/media/cameras/"+cameraID.String()+"/source", otherAccount, input, "", "")
	if denied.Code != http.StatusNotFound {
		t.Fatalf("cross-account source configuration must be hidden, got %d", denied.Code)
	}
	withCredentials := map[string]string{"rtsp_url": input["rtsp_url"], "source_username": "legacy-user", "source_password": "legacy-password"}
	legacyFields := mediaRequest(handler, http.MethodPut, "/api/v1/media/cameras/"+cameraID.String()+"/source", accountID, withCredentials, "", "")
	if legacyFields.Code != http.StatusUnprocessableEntity {
		t.Fatalf("legacy source credential fields should be rejected, got %d", legacyFields.Code)
	}
	badURL := map[string]string{"rtsp_url": "rtsp://embedded:secret@192.168.0.2:8554/stream"}
	invalid := mediaRequest(handler, http.MethodPut, "/api/v1/media/cameras/"+cameraID.String()+"/source", accountID, badURL, "", "")
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("URL-embedded credentials should be rejected, got %d", invalid.Code)
	}
}

func TestLegacyMediaSourceMigrationRemovesCredentialsAndIsIdempotent(t *testing.T) {
	db := mediaIntegrationDB(t)
	key := bytes.Repeat([]byte{0x82}, 32)
	legacyURL := "rtsp://192.168.0.2:8554/processed"
	legacyPlaintext, err := json.Marshal(legacyMediaSourcePayload{URL: legacyURL, Username: "legacy-user", Password: "legacy-secret"})
	if err != nil {
		t.Fatal(err)
	}
	legacyCiphertext, err := encryptMediaSourcePayload(key, legacyPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	stream := Stream{ID: uuid.New(), AccountID: uuid.New(), DeviceID: uuid.New(), CameraID: uuid.New(), EncryptedSource: legacyCiphertext,
		ConfigVersion: 1, ConfigStatus: "applied", SourceState: "configured", MediaState: "idle", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	broken := Stream{ID: uuid.New(), AccountID: stream.AccountID, DeviceID: stream.DeviceID, CameraID: uuid.New(), EncryptedSource: []byte("not-ciphertext"),
		ConfigVersion: 1, ConfigStatus: "applied", SourceState: "configured", MediaState: "idle", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := db.Create(&stream).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&broken).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&Stream{}, "id IN ?", []uuid.UUID{stream.ID, broken.ID})
	if err := migrateLegacyMediaSources(context.Background(), db, key); err == nil {
		t.Fatal("migration should fail closed when an existing ciphertext cannot be opened")
	}
	var unchanged Stream
	if err := db.First(&unchanged, "id = ?", stream.ID).Error; err != nil || !bytes.Equal(unchanged.EncryptedSource, legacyCiphertext) {
		t.Fatal("failed migration must not partially rewrite stored sources")
	}
	if err := db.Delete(&Stream{}, "id = ?", broken.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyMediaSources(context.Background(), db, key); err != nil {
		t.Fatalf("migrate legacy source: %v", err)
	}
	var migrated Stream
	if err := db.First(&migrated, "id = ?", stream.ID).Error; err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(migrated.EncryptedSource, legacyCiphertext) || bytes.Contains(migrated.EncryptedSource, []byte("legacy-secret")) {
		t.Fatal("migration did not replace the legacy ciphertext")
	}
	payload, err := decryptMediaSourcePayload(key, migrated.EncryptedSource)
	if err != nil || payload.Version != currentMediaSourceVersion || payload.URL != legacyURL || payload.Username != "" || payload.Password != "" {
		t.Fatalf("migration retained legacy credentials: payload=%+v err=%v", payload, err)
	}
	stableCiphertext := append([]byte(nil), migrated.EncryptedSource...)
	if err := migrateLegacyMediaSources(context.Background(), db, key); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err := db.First(&migrated, "id = ?", stream.ID).Error; err != nil || !bytes.Equal(stableCiphertext, migrated.EncryptedSource) {
		t.Fatal("repeat migration should be a no-op")
	}
}

func TestDeviceDisabledInboxIsIdempotentAndReleasesLeases(t *testing.T) {
	db := mediaIntegrationDB(t)
	accountID, deviceID, cameraID, streamID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	stream := Stream{ID: streamID, AccountID: accountID, DeviceID: deviceID, CameraID: cameraID, EncryptedSource: []byte("ciphertext"), ConfigVersion: 1, AppliedConfigVersion: 1, ConfigStatus: "applied", SourceState: "configured", MediaState: "online", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	lease := ViewerLease{ID: uuid.New(), AccountID: accountID, SessionID: "session-1", StreamID: streamID, ExpiresAt: time.Now().UTC().Add(time.Minute), CreatedAt: time.Now().UTC()}
	if err := db.Create(&stream).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&lease).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&ViewerLease{}, "id = ?", lease.ID)
	defer db.Delete(&Stream{}, "id = ?", streamID)

	mtx := &fakeMediaMTX{}
	app := NewApp(Config{InternalDeviceToken: "device-internal-token", InternalAuthToken: "auth-internal-token"}, db, nil, mtx)
	handler := app.Handler()
	eventID := uuid.New()
	envelope := internalInboxEnvelope{Producer: "device-service", EventType: "device.disabled", EventID: eventID, SchemaVersion: 1,
		OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{"account_id":"` + accountID.String() + `","device_id":"` + deviceID.String() + `"}`)}
	first := mediaRequest(handler, http.MethodPost, "/internal/v1/inbox", uuid.Nil, envelope, "device-service", "device-internal-token")
	if first.Code != http.StatusAccepted || !strings.Contains(first.Body.String(), `"duplicate":false`) {
		t.Fatalf("first device.disabled delivery: status=%d body=%s", first.Code, first.Body.String())
	}
	second := mediaRequest(handler, http.MethodPost, "/internal/v1/inbox", uuid.Nil, envelope, "device-service", "device-internal-token")
	if second.Code != http.StatusAccepted || !strings.Contains(second.Body.String(), `"duplicate":true`) {
		t.Fatalf("duplicate device.disabled delivery: status=%d body=%s", second.Code, second.Body.String())
	}
	if unauthorized := mediaRequest(handler, http.MethodPost, "/internal/v1/inbox", uuid.Nil, envelope, "device-service", "wrong"); unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("invalid producer token should be rejected, got %d", unauthorized.Code)
	}
	if err := db.First(&stream, "id = ?", streamID).Error; err != nil {
		t.Fatal(err)
	}
	if stream.Active || stream.ConfigStatus != "disabled" || mtx.deleteCalls != 1 {
		t.Fatalf("device disable did not remove desired MediaMTX path: stream=%+v mtx=%+v", stream, mtx)
	}
	if err := db.First(&lease, "id = ?", lease.ID).Error; err != nil || lease.ReleasedAt == nil {
		t.Fatalf("device disable did not release active viewer lease: lease=%+v err=%v", lease, err)
	}
}

func TestPlaybackLeaseEnforcesQuotaAuthorizationAndRelease(t *testing.T) {
	db := mediaIntegrationDB(t)
	accountID, deviceID, cameraID, streamID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ownership := CameraOwnership{AccountID: accountID.String(), DeviceID: deviceID.String(), CameraID: cameraID.String(), CameraName: "Front"}
	stream := Stream{ID: streamID, AccountID: accountID, DeviceID: deviceID, CameraID: cameraID, EncryptedSource: []byte("ciphertext"), ConfigVersion: 1, AppliedConfigVersion: 1, ConfigStatus: "applied", SourceState: "configured", MediaState: "online", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := db.Create(&stream).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&ViewerLease{}, "stream_id = ?", streamID)
	defer db.Delete(&Stream{}, "id = ?", streamID)

	authCalls := 0
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCalls++
		if r.Method != http.MethodGet || r.URL.Path != "/internal/v1/sessions/validate" || r.Header.Get("X-Internal-Service-Token") != "auth-internal-token" {
			t.Errorf("unexpected auth-service request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Cookie") != "yunshao_session=opaque-session-cookie" || r.Header.Get("X-Original-Origin") != "https://app.example.test" {
			t.Errorf("original browser session metadata was not forwarded")
		}
		if r.Header.Get("X-Account-ID") != "" || r.Header.Get("X-Session-ID") != "" {
			t.Errorf("untrusted client identity headers reached auth-service")
		}
		if r.Header.Get("X-Original-Method") == http.MethodPost && r.Header.Get("X-CSRF-Token") != "csrf-it" {
			t.Errorf("CSRF token was not forwarded for the proxied write")
		}
		w.Header().Set("X-Account-ID", accountID.String())
		w.Header().Set("X-Session-ID", "session-it")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer authServer.Close()
	mtx := &fakeMediaMTX{online: true}
	app := NewApp(Config{PublicMediaBaseURL: "https://media.example.test", AuthServiceInternalURL: authServer.URL,
		InternalAuthToken: "auth-internal-token", InternalNginxToken: "nginx-token"}, db, fakeOwnershipResolver{ownership: ownership}, mtx)
	handler := app.Handler()
	seeded := make([]ViewerLease, 0, maxActiveSourceCount)
	for index := 0; index < maxActiveSourceCount; index++ {
		seeded = append(seeded, ViewerLease{ID: uuid.New(), AccountID: uuid.New(), SessionID: "seed", StreamID: uuid.New(), ExpiresAt: time.Now().UTC().Add(time.Minute), CreatedAt: time.Now().UTC()})
	}
	if err := db.Create(&seeded).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Where("session_id = ?", "seed").Delete(&ViewerLease{})
	capacity := mediaRequest(handler, http.MethodGet, "/api/v1/media/cameras/"+cameraID.String()+"/playback", accountID, nil, "", "")
	if capacity.Code != http.StatusTooManyRequests || !strings.Contains(capacity.Body.String(), "capacity_full") {
		t.Fatalf("13th active source should be rejected: status=%d body=%s", capacity.Code, capacity.Body.String())
	}
	if err := db.Where("session_id = ?", "seed").Delete(&ViewerLease{}).Error; err != nil {
		t.Fatal(err)
	}
	connections := make([]ViewerLease, 0, maxViewerLeaseCount)
	for index := 0; index < maxViewerLeaseCount; index++ {
		connections = append(connections, ViewerLease{ID: uuid.New(), AccountID: accountID, SessionID: "connection-seed-" + uuid.NewString(), StreamID: streamID, ExpiresAt: time.Now().UTC().Add(time.Minute), CreatedAt: time.Now().UTC()})
	}
	if err := db.Create(&connections).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Where("stream_id = ? AND id <> ?", streamID, uuid.Nil).Delete(&ViewerLease{})
	connectionCapacity := mediaRequest(handler, http.MethodGet, "/api/v1/media/cameras/"+cameraID.String()+"/playback", accountID, nil, "", "")
	if connectionCapacity.Code != http.StatusTooManyRequests || !strings.Contains(connectionCapacity.Body.String(), "capacity_full") {
		t.Fatalf("25th viewer connection should be rejected: status=%d body=%s", connectionCapacity.Code, connectionCapacity.Body.String())
	}
	if err := db.Where("stream_id = ? AND id <> ?", streamID, uuid.Nil).Delete(&ViewerLease{}).Error; err != nil {
		t.Fatal(err)
	}
	response := mediaRequest(handler, http.MethodGet, "/api/v1/media/cameras/"+cameraID.String()+"/playback", accountID, nil, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("playback lease status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		LeaseID  uuid.UUID `json:"lease_id"`
		StreamID uuid.UUID `json:"stream_id"`
		WHEPURL  string    `json:"whep_url"`
		HLSURL   string    `json:"hls_url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.LeaseID == uuid.Nil || body.StreamID != streamID || !strings.Contains(body.WHEPURL, body.LeaseID.String()) || !strings.Contains(body.HLSURL, body.LeaseID.String()) {
		t.Fatalf("playback URLs did not include the active lease: %+v", body)
	}
	renew := mediaRequest(handler, http.MethodPost, "/api/v1/media/viewer-leases/"+body.LeaseID.String()+"/renew", accountID, nil, "", "")
	if renew.Code != http.StatusOK {
		t.Fatalf("lease renewal status=%d body=%s", renew.Code, renew.Body.String())
	}
	forgedAccountID := uuid.New()
	authorization := mediaAuthorizationRequest(handler, forgedAccountID, "/media/"+streamID.String()+"/whep?lease_id="+body.LeaseID.String(), "", http.MethodPost)
	if authorization.Code != http.StatusUnauthorized {
		t.Fatalf("authorization must reject a request without the internal proxy credential, got %d", authorization.Code)
	}
	if authCalls != 0 {
		t.Fatal("auth-service must not be called for a request without the internal proxy credential")
	}
	authorization = mediaAuthorizationRequest(handler, forgedAccountID, "/media/"+streamID.String()+"/whep?lease_id="+body.LeaseID.String(), "nginx-token", http.MethodPost)
	if authorization.Code != http.StatusNoContent {
		t.Fatalf("active lease should authorize the identity resolved by auth-service despite forged identity headers, got %d %s", authorization.Code, authorization.Body.String())
	}
	fragmentAuthorization := mediaAuthorizationRequest(handler, forgedAccountID, "/media/"+streamID.String()+"/index.m3u8/segment_0.m4s", "nginx-token", http.MethodGet)
	if fragmentAuthorization.Code != http.StatusNoContent {
		t.Fatalf("HLS fragment without a lease query should authorize through the current session, got %d", fragmentAuthorization.Code)
	}
	released := mediaRequest(handler, http.MethodDelete, "/api/v1/media/viewer-leases/"+body.LeaseID.String(), accountID, nil, "", "")
	if released.Code != http.StatusNoContent || mtx.kickCalls != 1 {
		t.Fatalf("lease release should close the MediaMTX session: status=%d kicks=%d", released.Code, mtx.kickCalls)
	}
	authorization = mediaAuthorizationRequest(handler, forgedAccountID, "/media/"+streamID.String()+"/whep?lease_id="+body.LeaseID.String(), "nginx-token", http.MethodPost)
	if authorization.Code != http.StatusUnauthorized {
		t.Fatalf("released lease must not authorize media, got %d", authorization.Code)
	}
}

func TestSessionRevocationInboxKicksAssociatedLease(t *testing.T) {
	db := mediaIntegrationDB(t)
	accountID, streamID, leaseID := uuid.New(), uuid.New(), uuid.New()
	stream := Stream{ID: streamID, AccountID: accountID, DeviceID: uuid.New(), CameraID: uuid.New(), EncryptedSource: []byte("ciphertext"), ConfigVersion: 1, AppliedConfigVersion: 1, ConfigStatus: "applied", SourceState: "configured", Active: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	lease := ViewerLease{ID: leaseID, AccountID: accountID, SessionID: "session-revoke", StreamID: streamID, ExpiresAt: time.Now().UTC().Add(time.Minute), CreatedAt: time.Now().UTC()}
	if err := db.Create(&stream).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&lease).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&ViewerLease{}, "id = ?", leaseID)
	defer db.Delete(&Stream{}, "id = ?", streamID)
	mtx := &fakeMediaMTX{}
	app := NewApp(Config{InternalAuthToken: "auth-internal-token"}, db, nil, mtx)
	envelope := internalInboxEnvelope{Producer: "auth-service", EventType: "session.revoked", EventID: uuid.New(), SchemaVersion: 1, OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{"account_id":"` + accountID.String() + `","session_id":"session-revoke"}`)}
	response := mediaRequest(app.Handler(), http.MethodPost, "/internal/v1/inbox", uuid.Nil, envelope, "auth-service", "auth-internal-token")
	if response.Code != http.StatusAccepted {
		t.Fatalf("session.revoked inbox status=%d body=%s", response.Code, response.Body.String())
	}
	if err := db.First(&lease, "id = ?", leaseID).Error; err != nil || lease.ReleasedAt == nil || lease.KickPending || mtx.kickCalls != 1 {
		t.Fatalf("revoked session did not release and kick media lease: lease=%+v kicks=%d err=%v", lease, mtx.kickCalls, err)
	}
}
