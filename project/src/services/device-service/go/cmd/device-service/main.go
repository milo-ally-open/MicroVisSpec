package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	heartbeatInterval = 30 * time.Second
	heartbeatTimeout  = 90 * time.Second
	maxPageSize       = 100
)

type Config struct {
	DatabaseURL          string
	ListenAddress        string
	BrokerURL            string
	AdvertisedBrokerURL  string
	BrokerAdminUsername  string
	BrokerAdminPassword  string
	BrokerAdminClientID  string
	BrokerCAFile         string
	BrokerClientCertFile string
	BrokerClientKeyFile  string
	BrokerServiceUser    string
	BrokerServicePass    string
	BrokerServiceClient  string
	InternalToken        string
	MediaServiceURL      string
	MediaServiceToken    string
	ActivationRatePerMin int
}

func LoadConfig() (Config, error) {
	var configErr error
	read := func(name string) string {
		if configErr != nil {
			return ""
		}
		value, err := configValue(name)
		if err != nil {
			configErr = err
			return ""
		}
		return value
	}
	c := Config{
		DatabaseURL:          read("DATABASE_URL"),
		ListenAddress:        env("DEVICE_LISTEN_ADDRESS", ":8080"),
		BrokerURL:            env("MQTT_BROKER_URL", "ssl://mosquitto:8883"),
		AdvertisedBrokerURL:  env("MQTT_ADVERTISED_BROKER_URL", ""),
		BrokerAdminUsername:  read("MQTT_ADMIN_USERNAME"),
		BrokerAdminPassword:  read("MQTT_ADMIN_PASSWORD"),
		BrokerAdminClientID:  env("MQTT_ADMIN_CLIENT_ID", "yunshao-device-service-dynsec"),
		BrokerCAFile:         os.Getenv("MQTT_CA_FILE"),
		BrokerClientCertFile: os.Getenv("MQTT_CLIENT_CERT_FILE"),
		BrokerClientKeyFile:  os.Getenv("MQTT_CLIENT_KEY_FILE"),
		BrokerServiceUser:    read("MQTT_DEVICE_SERVICE_USERNAME"),
		BrokerServicePass:    read("MQTT_DEVICE_SERVICE_PASSWORD"),
		BrokerServiceClient:  env("MQTT_DEVICE_SERVICE_CLIENT_ID", "yunshao-device-service"),
		InternalToken:        read("DEVICE_INTERNAL_SERVICE_TOKEN"),
		MediaServiceURL:      strings.TrimRight(os.Getenv("MEDIA_SERVICE_INTERNAL_URL"), "/"),
		MediaServiceToken:    read("MEDIA_SERVICE_INTERNAL_TOKEN"),
		ActivationRatePerMin: envPositiveInt("DEVICE_ACTIVATION_RATE_PER_MINUTE", 10),
	}
	if configErr != nil {
		return c, configErr
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if c.BrokerAdminUsername == "" || c.BrokerAdminPassword == "" {
		return c, errors.New("MQTT_ADMIN_USERNAME and MQTT_ADMIN_PASSWORD are required")
	}
	if c.BrokerServiceUser == "" || c.BrokerServicePass == "" {
		return c, errors.New("MQTT_DEVICE_SERVICE_USERNAME and MQTT_DEVICE_SERVICE_PASSWORD are required")
	}
	if c.InternalToken == "" {
		return c, errors.New("DEVICE_INTERNAL_SERVICE_TOKEN is required")
	}
	if err := validateMQTTBrokerURL(c.BrokerURL); err != nil {
		return c, errors.New("MQTT_BROKER_URL must be an absolute ssl:// broker URL without credentials or path")
	}
	if c.AdvertisedBrokerURL == "" {
		return c, errors.New("MQTT_ADVERTISED_BROKER_URL is required")
	}
	if err := validateMQTTBrokerURL(c.AdvertisedBrokerURL); err != nil {
		return c, errors.New("MQTT_ADVERTISED_BROKER_URL must be an absolute ssl:// broker URL without credentials or path")
	}
	if (c.MediaServiceURL == "") != (c.MediaServiceToken == "") {
		return c, errors.New("MEDIA_SERVICE_INTERNAL_URL and MEDIA_SERVICE_INTERNAL_TOKEN must be configured together")
	}
	if c.MediaServiceURL != "" {
		parsed, err := url.ParseRequestURI(c.MediaServiceURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return c, errors.New("MEDIA_SERVICE_INTERNAL_URL must be an absolute HTTP URL")
		}
	}
	return c, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envPositiveInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

type Device struct {
	ID              uuid.UUID       `gorm:"type:uuid;primaryKey" json:"device_id"`
	AccountID       uuid.UUID       `gorm:"type:uuid;not null;index:idx_devices_account_active,priority:1" json:"-"`
	Name            string          `gorm:"size:120;not null" json:"name"`
	IP              string          `gorm:"size:45;not null" json:"ip"`
	Location        string          `gorm:"size:256;not null" json:"location"`
	ActivationState string          `gorm:"size:24;not null;default:pending;index" json:"activation_state"`
	Active          bool            `gorm:"not null;default:true;index:idx_devices_account_active,priority:2" json:"active"`
	LastSeenAt      *time.Time      `gorm:"index" json:"last_seen_at,omitempty"`
	ReportedAt      *time.Time      `gorm:"index" json:"reported_at,omitempty"`
	ReportedState   json.RawMessage `gorm:"type:jsonb;not null;default:'{}'" json:"reported_state"`
	CreatedAt       time.Time       `gorm:"not null;index:idx_devices_created" json:"created_at"`
	UpdatedAt       time.Time       `gorm:"not null" json:"updated_at"`
	DisabledAt      *time.Time      `json:"disabled_at,omitempty"`
}

func (Device) TableName() string { return "device.devices" }

type Camera struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey" json:"camera_id"`
	AccountID      uuid.UUID  `gorm:"type:uuid;not null;index;uniqueIndex:idx_cameras_account_device_key,priority:1" json:"-"`
	DeviceID       uuid.UUID  `gorm:"type:uuid;not null;index;uniqueIndex:idx_cameras_account_device_key,priority:2" json:"device_id"`
	Name           string     `gorm:"size:120;not null" json:"name"`
	CameraKey      string     `gorm:"size:120;not null;uniqueIndex:idx_cameras_account_device_key,priority:3" json:"camera_key"`
	Active         bool       `gorm:"not null;default:true" json:"active"`
	CameraState    string     `gorm:"size:32;not null;default:unknown" json:"camera_state"`
	StreamState    string     `gorm:"size:32;not null;default:unknown" json:"stream_state"`
	LastReportedAt *time.Time `json:"last_reported_at,omitempty"`
	CreatedAt      time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"not null" json:"updated_at"`
}

func (Camera) TableName() string { return "device.cameras" }

type AlgorithmAssignment struct {
	ID                 uuid.UUID       `gorm:"type:uuid;primaryKey" json:"assignment_id"`
	AccountID          uuid.UUID       `gorm:"type:uuid;not null;index" json:"-"`
	DeviceID           uuid.UUID       `gorm:"type:uuid;not null;index;uniqueIndex:idx_assignments_device_algorithm,priority:1" json:"device_id"`
	AlgorithmKey       string          `gorm:"size:120;not null;index;uniqueIndex:idx_assignments_device_algorithm,priority:2" json:"algorithm_key"`
	AlgorithmVersion   string          `gorm:"size:120;not null" json:"algorithm_version"`
	RuntimeState       string          `gorm:"size:32;not null;default:unknown" json:"runtime_state"`
	ConfigState        string          `gorm:"size:24;not null;default:pending" json:"config_state"`
	Capabilities       json.RawMessage `gorm:"type:jsonb;not null;default:'{}'" json:"capabilities"`
	ReportedParameters json.RawMessage `gorm:"type:jsonb;not null;default:'{}'" json:"reported_parameters"`
	DesiredParameters  json.RawMessage `gorm:"type:jsonb;not null;default:'{}'" json:"desired_parameters"`
	DesiredVersion     int64           `gorm:"not null;default:0" json:"desired_version"`
	ReportedVersion    int64           `gorm:"not null;default:0" json:"reported_version"`
	ReportedAt         *time.Time      `json:"reported_at,omitempty"`
	CreatedAt          time.Time       `gorm:"not null" json:"created_at"`
	UpdatedAt          time.Time       `gorm:"not null" json:"updated_at"`
}

func (AlgorithmAssignment) TableName() string { return "device.algorithm_assignments" }

type ActivationTicket struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey"`
	DeviceID       uuid.UUID  `gorm:"type:uuid;not null;index"`
	TokenHash      string     `gorm:"size:64;not null;uniqueIndex"`
	ExpiresAt      time.Time  `gorm:"not null;index"`
	ConsumedAt     *time.Time `gorm:"index"`
	RevokedAt      *time.Time `gorm:"index"`
	ClaimID        *uuid.UUID `gorm:"type:uuid;index"`
	ClaimExpiresAt *time.Time `gorm:"index"`
	CreatedAt      time.Time  `gorm:"not null"`
}

func (ActivationTicket) TableName() string { return "device.activation_tickets" }

type CommandRecord struct {
	ID              uuid.UUID       `gorm:"type:uuid;primaryKey" json:"command_id"`
	AssignmentID    uuid.UUID       `gorm:"type:uuid;not null;index" json:"assignment_id"`
	DeviceID        uuid.UUID       `gorm:"type:uuid;not null;index" json:"device_id"`
	ExpectedVersion int64           `gorm:"not null" json:"expected_version"`
	Parameters      json.RawMessage `gorm:"type:jsonb;not null" json:"parameters"`
	Status          string          `gorm:"size:24;not null;index" json:"status"`
	FailureReason   string          `gorm:"size:500" json:"failure_reason,omitempty"`
	ReportedVersion int64           `gorm:"not null;default:0" json:"reported_version"`
	Attempts        int             `gorm:"not null;default:0" json:"attempts"`
	LastAttemptAt   *time.Time      `json:"last_attempt_at,omitempty"`
	CreatedAt       time.Time       `gorm:"not null;index" json:"created_at"`
	AcknowledgedAt  *time.Time      `json:"acknowledged_at,omitempty"`
}

func (CommandRecord) TableName() string { return "device.command_records" }

type ConfigSnapshot struct {
	ID               uuid.UUID       `gorm:"type:uuid;primaryKey"`
	AssignmentID     uuid.UUID       `gorm:"type:uuid;not null;uniqueIndex:idx_config_snapshots_version,priority:1"`
	DeviceID         uuid.UUID       `gorm:"type:uuid;not null;index"`
	Version          int64           `gorm:"not null;uniqueIndex:idx_config_snapshots_version,priority:2"`
	AlgorithmVersion string          `gorm:"size:120;not null"`
	Parameters       json.RawMessage `gorm:"type:jsonb;not null"`
	CreatedAt        time.Time       `gorm:"not null"`
	AppliedAt        *time.Time      `gorm:"index"`
}

func (ConfigSnapshot) TableName() string { return "device.config_snapshots" }

type Outbox struct {
	ID            uuid.UUID       `gorm:"type:uuid;primaryKey" json:"event_id"`
	EventType     string          `gorm:"size:100;not null;index" json:"event_type"`
	Payload       json.RawMessage `gorm:"type:jsonb;not null" json:"payload"`
	CreatedAt     time.Time       `gorm:"not null;index" json:"created_at"`
	DeliveredAt   *time.Time      `gorm:"index" json:"delivered_at,omitempty"`
	Attempts      int             `gorm:"not null;default:0" json:"attempts"`
	NextAttemptAt time.Time       `gorm:"not null;index" json:"next_attempt_at"`
	LastError     string          `gorm:"size:500" json:"last_error,omitempty"`
	LeaseID       *uuid.UUID      `gorm:"type:uuid;index" json:"-"`
	LeaseUntil    *time.Time      `gorm:"index" json:"-"`
}

func (Outbox) TableName() string { return "device.outbox" }

type BrokerCredential struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey"`
	DeviceID     uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex"`
	Username     string     `gorm:"size:120;not null;uniqueIndex"`
	ClientID     string     `gorm:"size:120;not null;uniqueIndex"`
	PasswordHash string     `gorm:"size:64;not null"`
	CreatedAt    time.Time  `gorm:"not null"`
	RevokedAt    *time.Time `gorm:"index"`
}

func (BrokerCredential) TableName() string { return "device.broker_credentials" }

func openDatabase(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn), TranslateError: true})
	if err != nil {
		return nil, err
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS device").Error; err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Device{}, &Camera{}, &AlgorithmAssignment{}, &ActivationTicket{}, &CommandRecord{}, &ConfigSnapshot{}, &Outbox{}, &BrokerCredential{}); err != nil {
		return nil, err
	}
	// Remove the account-only unique index left by an early schema draft; it
	// incorrectly limited each account to one registered device.
	if err := db.Exec("DROP INDEX IF EXISTS device.idx_devices_account_id_id").Error; err != nil {
		return nil, err
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_devices_tenant_lookup ON device.devices (account_id, id)").Error; err != nil {
		return nil, err
	}
	return db, nil
}

type App struct {
	db                *gorm.DB
	config            Config
	broker            DeviceBroker
	messages          DeviceMessagePlane
	outboxHTTP        *http.Client
	activationLimiter *windowLimiter
}

type DeviceView struct {
	Device
	Online     bool                  `json:"online"`
	Cameras    []Camera              `json:"cameras"`
	Algorithms []AlgorithmAssignment `json:"algorithms"`
}

func NewHandler(db *gorm.DB) http.Handler {
	return NewHandlerWithDependencies(Config{}, db, nil)
}

func NewHandlerWithDependencies(config Config, db *gorm.DB, broker DeviceBroker) http.Handler {
	return NewHandlerWithServices(config, db, broker, nil)
}

func NewHandlerWithServices(config Config, db *gorm.DB, broker DeviceBroker, messages DeviceMessagePlane) http.Handler {
	return NewApp(config, db, broker, messages).Handler()
}

func NewApp(config Config, db *gorm.DB, broker DeviceBroker, messages DeviceMessagePlane) *App {
	return &App{db: db, config: config, broker: broker, messages: messages, outboxHTTP: &http.Client{Timeout: 5 * time.Second}, activationLimiter: newWindowLimiter(config.ActivationRatePerMin)}
}

func (app *App) Handler() http.Handler {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(gin.Recovery(), app.requestContext(), app.accessLog())
	r.GET("/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "live"}) })
	r.GET("/health/ready", app.ready)
	api := r.Group("/api/v1/devices", app.requireAccount())
	api.GET("/stats", app.stats)
	api.POST("", app.createDevice)
	api.GET("", app.listDevices)
	api.GET("/:device_id", app.getDevice)
	api.PATCH("/:device_id", app.updateDevice)
	api.DELETE("/:device_id", app.disableDevice)
	api.POST("/:device_id/activation-tickets", app.createActivationTicket)
	api.POST("/:device_id/cameras", app.createCamera)
	api.GET("/:device_id/algorithms", app.listAlgorithms)
	api.PUT("/:device_id/algorithms/:assignment_id/parameters", app.updateAlgorithmParameters)
	r.POST("/edge/v1/activate", app.activateDevice)
	r.POST("/internal/v1/devices/:device_id/http-authenticate", app.requireInternalToken(), app.authenticateDeviceHTTP)
	r.GET("/internal/v1/cameras/:camera_id/ownership", app.requireInternalToken(), app.resolveCameraOwnershipByID)
	r.GET("/internal/v1/devices/:device_id/cameras/:camera_id/ownership", app.requireInternalToken(), app.resolveCameraOwnership)
	r.POST("/internal/v1/devices/:device_id/classroom-events/validate", app.requireInternalToken(), app.validateClassroomEvent)
	return r
}

func (a *App) requestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if len(id) > 64 || strings.ContainsAny(id, "\r\n") {
			id = ""
		}
		if id == "" {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func (a *App) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http_request", "request_id", c.GetString("request_id"), "method", c.Request.Method,
			"path", c.Request.URL.Path, "status", c.Writer.Status(), "duration_ms", time.Since(start).Milliseconds())
	}
}

func (a *App) ready(c *gin.Context) {
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
	if a.broker == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MQTT management is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	if err := a.broker.Check(ctx); err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MQTT management is not ready")
		return
	}
	if a.messages == nil || a.messages.Check(ctx) != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MQTT device messaging is not ready")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "dependencies": gin.H{"postgres": "ready", "mqtt_dynamic_security": "ready", "mqtt_device_messaging": "ready"}})
}

func (a *App) requireInternalToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a.config.InternalToken == "" || !constantTimeEqual(c.GetHeader("X-Internal-Service-Token"), a.config.InternalToken) {
			writeError(c, http.StatusUnauthorized, "unauthorized", "service authentication required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (a *App) requireAccount() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.GetHeader("X-Account-ID"))
		if err != nil || id == uuid.Nil {
			writeError(c, http.StatusUnauthorized, "unauthorized", "a valid account session is required")
			c.Abort()
			return
		}
		c.Set("account_id", id)
		c.Next()
	}
}

func accountID(c *gin.Context) uuid.UUID { return c.MustGet("account_id").(uuid.UUID) }

func decodeJSON(c *gin.Context, dst any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func (a *App) createDevice(c *gin.Context) {
	var input struct {
		Name     string `json:"name"`
		IP       string `json:"ip"`
		Location string `json:"location"`
	}
	if decodeJSON(c, &input) != nil || validateAsset(input.Name, input.IP, input.Location) != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "name, IP address, and location are required and must be valid")
		return
	}
	now := time.Now().UTC()
	device := Device{ID: uuid.New(), AccountID: accountID(c), Name: strings.TrimSpace(input.Name), IP: strings.TrimSpace(input.IP), Location: strings.TrimSpace(input.Location), ActivationState: "pending", Active: true, ReportedState: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now}
	if err := a.db.Create(&device).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device could not be registered")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"device_id": device.ID, "activation_state": device.ActivationState, "created_at": device.CreatedAt})
}

func validateAsset(name, ip, location string) error {
	if strings.TrimSpace(name) == "" || len(strings.TrimSpace(name)) > 120 || net.ParseIP(strings.TrimSpace(ip)) == nil || strings.TrimSpace(location) == "" || len(strings.TrimSpace(location)) > 256 {
		return errors.New("invalid asset fields")
	}
	return nil
}

func (a *App) listDevices(c *gin.Context) {
	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_pagination", "limit must be between 1 and 100")
		return
	}
	query := a.db.Where("account_id = ? AND active = ?", accountID(c), true)
	if token := c.Query("cursor"); token != "" {
		createdAt, id, err := decodeCursor(token)
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "invalid_cursor", "cursor is invalid")
			return
		}
		query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", createdAt, createdAt, id)
	}
	var devices []Device
	if err := query.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&devices).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "devices could not be listed")
		return
	}
	more := len(devices) > limit
	if more {
		devices = devices[:limit]
	}
	items := make([]DeviceView, 0, len(devices))
	for _, device := range devices {
		view, err := a.deviceView(device)
		if err != nil {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device details could not be loaded")
			return
		}
		items = append(items, view)
	}
	next := ""
	if more && len(devices) > 0 {
		next = encodeCursor(devices[len(devices)-1].CreatedAt, devices[len(devices)-1].ID)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 20, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxPageSize {
		return 0, errors.New("invalid limit")
	}
	return n, nil
}

func encodeCursor(createdAt time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(token string) (time.Time, uuid.UUID, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, errors.New("malformed cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	return createdAt, id, nil
}

func (a *App) deviceView(device Device) (DeviceView, error) {
	view := DeviceView{Device: device, Online: isOnline(device, time.Now().UTC()), Cameras: []Camera{}, Algorithms: []AlgorithmAssignment{}}
	if err := a.db.Where("account_id = ? AND device_id = ? AND active = ?", device.AccountID, device.ID, true).Order("created_at ASC").Find(&view.Cameras).Error; err != nil {
		return DeviceView{}, err
	}
	if err := a.db.Where("account_id = ? AND device_id = ?", device.AccountID, device.ID).Order("algorithm_key ASC").Find(&view.Algorithms).Error; err != nil {
		return DeviceView{}, err
	}
	return view, nil
}

func isOnline(device Device, now time.Time) bool {
	return device.Active && device.ActivationState == "active" && device.LastSeenAt != nil && now.Sub(*device.LastSeenAt) <= heartbeatTimeout
}

func (a *App) getDevice(c *gin.Context) {
	device, ok := a.findOwnedDevice(c, c.Param("device_id"), true)
	if !ok {
		return
	}
	view, err := a.deviceView(device)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device details could not be loaded")
		return
	}
	c.JSON(http.StatusOK, view)
}

func (a *App) findOwnedDevice(c *gin.Context, rawID string, activeOnly bool) (Device, bool) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		writeError(c, http.StatusNotFound, "not_found", "device was not found")
		return Device{}, false
	}
	query := a.db.Where("id = ? AND account_id = ?", id, accountID(c))
	if activeOnly {
		query = query.Where("active = ?", true)
	}
	var device Device
	if err := query.First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "device was not found")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device could not be loaded")
		}
		return Device{}, false
	}
	return device, true
}

func (a *App) updateDevice(c *gin.Context) {
	device, ok := a.findOwnedDevice(c, c.Param("device_id"), true)
	if !ok {
		return
	}
	var input struct {
		Name     *string `json:"name"`
		IP       *string `json:"ip"`
		Location *string `json:"location"`
	}
	if decodeJSON(c, &input) != nil || input.Name == nil && input.IP == nil && input.Location == nil {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "at least one of name, ip, or location is required")
		return
	}
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if value == "" || len(value) > 120 {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "name must contain 1 to 120 characters")
			return
		}
		updates["name"] = value
	}
	if input.IP != nil {
		value := strings.TrimSpace(*input.IP)
		if net.ParseIP(value) == nil {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "ip must be a valid IP address")
			return
		}
		updates["ip"] = value
	}
	if input.Location != nil {
		value := strings.TrimSpace(*input.Location)
		if len(value) > 256 {
			writeError(c, http.StatusUnprocessableEntity, "invalid_request", "location must be at most 256 characters")
			return
		}
		updates["location"] = value
	}
	if err := a.db.Model(&Device{}).Where("id = ? AND account_id = ? AND active = ?", device.ID, accountID(c), true).Updates(updates).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device could not be updated")
		return
	}
	device, ok = a.findOwnedDevice(c, device.ID.String(), true)
	if !ok {
		return
	}
	view, err := a.deviceView(device)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device details could not be loaded")
		return
	}
	c.JSON(http.StatusOK, view)
}

func (a *App) disableDevice(c *gin.Context) {
	device, ok := a.findOwnedDevice(c, c.Param("device_id"), false)
	if !ok {
		return
	}
	if !device.Active {
		c.Status(http.StatusNoContent)
		return
	}
	now := time.Now().UTC()
	payload, _ := json.Marshal(map[string]string{"account_id": device.AccountID.String(), "device_id": device.ID.String()})
	err := a.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Device{}).Where("id = ? AND account_id = ? AND active = ?", device.ID, device.AccountID, true).Updates(map[string]any{
			"active": false, "activation_state": "disabled", "disabled_at": now, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		return tx.Create(&Outbox{ID: uuid.New(), EventType: "device.disabled", Payload: payload, CreatedAt: now, NextAttemptAt: now}).Error
	})
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device could not be disabled")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	if err := a.revokeDeviceCredentials(ctx, device.ID); err != nil {
		slog.Error("broker_credential_revocation_failed", "request_id", c.GetString("request_id"), "device_id", device.ID, "error", err.Error())
	}
	cancel()
	c.Status(http.StatusNoContent)
}

func (a *App) createCamera(c *gin.Context) {
	device, ok := a.findOwnedDevice(c, c.Param("device_id"), true)
	if !ok {
		return
	}
	var input struct {
		Name      string `json:"name"`
		CameraKey string `json:"camera_key"`
	}
	if decodeJSON(c, &input) != nil || strings.TrimSpace(input.Name) == "" || len(strings.TrimSpace(input.Name)) > 120 || strings.TrimSpace(input.CameraKey) == "" || len(strings.TrimSpace(input.CameraKey)) > 120 {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "name and camera_key are required and must be at most 120 characters")
		return
	}
	now := time.Now().UTC()
	camera := Camera{ID: uuid.New(), AccountID: device.AccountID, DeviceID: device.ID, Name: strings.TrimSpace(input.Name), CameraKey: strings.TrimSpace(input.CameraKey), Active: true, CreatedAt: now, UpdatedAt: now}
	if err := a.db.Create(&camera).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			writeError(c, http.StatusConflict, "camera_key_conflict", "camera_key is already registered for this device")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "camera could not be registered")
		return
	}
	c.JSON(http.StatusCreated, camera)
}

func (a *App) listAlgorithms(c *gin.Context) {
	device, ok := a.findOwnedDevice(c, c.Param("device_id"), true)
	if !ok {
		return
	}
	var algorithms []AlgorithmAssignment
	if err := a.db.Where("account_id = ? AND device_id = ?", device.AccountID, device.ID).Order("algorithm_key ASC").Find(&algorithms).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "algorithms could not be loaded")
		return
	}
	if algorithms == nil {
		algorithms = []AlgorithmAssignment{}
	}
	c.JSON(http.StatusOK, gin.H{"items": algorithms})
}

func (a *App) stats(c *gin.Context) {
	var registered int64
	var online int64
	account := accountID(c)
	if err := a.db.Model(&Device{}).Where("account_id = ? AND active = ?", account, true).Count(&registered).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device statistics could not be loaded")
		return
	}
	cutoff := time.Now().UTC().Add(-heartbeatTimeout)
	if err := a.db.Model(&Device{}).Where("account_id = ? AND active = ? AND activation_state = ? AND last_seen_at >= ?", account, true, "active", cutoff).Count(&online).Error; err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "device statistics could not be loaded")
		return
	}
	c.JSON(http.StatusOK, gin.H{"registered": registered, "online": online, "as_of": time.Now().UTC()})
}

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "request_id": c.GetString("request_id")})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	config, err := LoadConfig()
	if err != nil {
		slog.Error("invalid_configuration", "error", err.Error())
		os.Exit(2)
	}
	db, err := openDatabase(config.DatabaseURL)
	if err != nil {
		slog.Error("database_initialization_failed", "error", err.Error())
		os.Exit(1)
	}
	sqlDB, err := db.DB()
	if err != nil {
		slog.Error("database_initialization_failed", "error", err.Error())
		os.Exit(1)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	broker, err := NewDynamicSecurityBroker(config)
	if err != nil {
		slog.Error("mqtt_configuration_failed", "error", err.Error())
		os.Exit(2)
	}
	messages, err := NewPahoDeviceMessagePlane(config)
	if err != nil {
		slog.Error("mqtt_configuration_failed", "error", err.Error())
		os.Exit(2)
	}
	app := NewApp(config, db, broker, messages)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := messages.Start(ctx, app.handleMQTTMessage); err != nil {
		slog.Error("device_mqtt_start_failed", "error", err.Error())
		os.Exit(1)
	}
	defer messages.Close()
	go app.pendingCommandLoop(ctx)
	go app.outboxLoop(ctx)
	go app.brokerRevocationLoop(ctx)
	server := &http.Server{Addr: config.ListenAddress, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		slog.Info("device_service_started", "listen_address", config.ListenAddress, "heartbeat_interval_seconds", int(heartbeatInterval.Seconds()))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("device_service_failed", "error", err.Error())
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		slog.Error("http_shutdown_failed", "error", fmt.Sprintf("%v", err))
	}
	_ = sqlDB.Close()
}
