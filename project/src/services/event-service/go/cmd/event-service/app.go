package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type EventMessagePlane interface {
	Check(context.Context) error
	Start(context.Context, func(string, []byte)) error
	Publish(context.Context, string, []byte) error
	Close()
}

type App struct {
	config        Config
	db            *gorm.DB
	messages      EventMessagePlane
	deviceHTTP    *http.Client
	pushHTTP      *http.Client
	authorization EventAuthorizationClient
}

func openDatabase(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, err
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS event").Error; err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Event{}, &Evidence{}, &Disposition{}, &Outbox{}); err != nil {
		return nil, err
	}
	// Expand/backfill phase: workspace IDs currently map to the event's
	// trusted PC owner. Outbox ownership is derived from its persisted event.
	if err := db.Exec("UPDATE event.events SET workspace_id = account_id WHERE workspace_id IS NULL").Error; err != nil {
		return nil, err
	}
	if err := db.Exec("UPDATE event.outbox AS outboxes SET workspace_id = events.workspace_id FROM event.events AS events WHERE outboxes.id = events.id AND outboxes.workspace_id IS NULL").Error; err != nil {
		return nil, err
	}
	for _, statement := range []string{
		"UPDATE event.evidence AS child SET workspace_id = parent.workspace_id FROM event.events AS parent WHERE child.event_id = parent.id AND child.workspace_id IS NULL",
		"UPDATE event.dispositions AS child SET workspace_id = parent.workspace_id FROM event.events AS parent WHERE child.event_id = parent.id AND child.workspace_id IS NULL",
	} {
		if err := db.Exec(statement).Error; err != nil {
			return nil, err
		}
	}
	return db, nil
}

func NewApp(config Config, db *gorm.DB, messages EventMessagePlane, deviceHTTP *http.Client) *App {
	if deviceHTTP == nil {
		deviceHTTP = &http.Client{Timeout: 5 * time.Second}
	}
	return &App{config: config, db: db, messages: messages, deviceHTTP: deviceHTTP, authorization: NewEventAuthorizationClient(config), pushHTTP: &http.Client{Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (a *App) Handler() http.Handler {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(gin.Recovery(), a.requestContext(), a.accessLog())
	r.GET("/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "live"}) })
	r.GET("/health/ready", a.ready)
	api := r.Group("/api/v1/events", a.requireAccount())
	api.GET("/stats", a.eventStats)
	api.GET("", a.listEvents)
	api.GET("/:event_id", a.getEvent)
	api.GET("/:event_id/evidence", a.readEvidence)
	api.POST("/:event_id/transitions", a.transitionEvent)
	r.PUT("/edge/v1/devices/:device_id/events/:edge_event_id/evidence", a.uploadEvidence)
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
		err = sqlDB.PingContext(ctx)
		cancel()
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "database is not ready")
		return
	}
	info, err := os.Stat(a.config.EvidenceDir)
	if err != nil || !info.IsDir() {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "evidence volume is not ready")
		return
	}
	if a.messages == nil || a.messages.Check(c.Request.Context()) != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "MQTT event messaging is not ready")
		return
	}
	if len(a.config.CursorSigningKey) < 32 || a.config.PushServiceURL == "" || a.config.PushServiceToken == "" {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "event delivery and cursor signing are not configured")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "dependencies": gin.H{"postgres": "ready", "evidence_volume": "ready", "mqtt": "ready"}})
}

func (a *App) requireAccount() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.GetHeader("X-Account-ID"))
		sessionID := strings.TrimSpace(c.GetHeader("X-Session-ID"))
		userType := strings.TrimSpace(c.GetHeader("X-User-Type"))
		if err != nil || id == uuid.Nil || sessionID == "" || len(sessionID) > 128 || (userType != "pc" && userType != "mobile") {
			writeError(c, http.StatusUnauthorized, "unauthorized", "a valid account session is required")
			c.Abort()
			return
		}
		c.Set("account_id", id)
		c.Set("session_id", sessionID)
		c.Set("user_type", userType)
		c.Next()
	}
}

func accountID(c *gin.Context) uuid.UUID { return c.MustGet("account_id").(uuid.UUID) }

func writeError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "request_id": c.GetString("request_id")})
}

func run() error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.EvidenceDir, 0o750); err != nil {
		return fmt.Errorf("initialize evidence volume: %w", err)
	}
	db, err := openDatabase(config.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database initialization failed: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	messages, err := NewPahoEventMessagePlane(config)
	if err != nil {
		return fmt.Errorf("initialize MQTT event messaging: %w", err)
	}
	defer messages.Close()
	app := NewApp(config, db, messages, nil)
	workerCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := messages.Start(workerCtx, app.handleMQTTMessage); err != nil {
		return err
	}
	app.startOutboxWorker(workerCtx)
	app.startMaintenance(workerCtx)
	server := &http.Server{Addr: config.ListenAddress, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	slog.Info("event_service_starting", "listen_address", config.ListenAddress)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	select {
	case <-workerCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func main() {
	if err := run(); err != nil {
		slog.Error("event_service_stopped", "error", err.Error())
		os.Exit(1)
	}
}
