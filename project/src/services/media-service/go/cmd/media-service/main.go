package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	config, err := LoadConfig()
	if err != nil {
		slog.Error("invalid_configuration", "error", err.Error())
		os.Exit(2)
	}
	db, err := OpenDatabase(config.DatabaseURL)
	if err != nil {
		slog.Error("database_initialization_failed", "error", err.Error())
		os.Exit(1)
	}
	if err := migrateLegacyMediaSources(context.Background(), db, config.MediaSourceEncryptionKey); err != nil {
		slog.Error("media_source_migration_failed", "error", err.Error())
		os.Exit(1)
	}
	sqlDB, err := db.DB()
	if err != nil {
		slog.Error("database_initialization_failed", "error", err.Error())
		os.Exit(1)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	app := NewAppFromConfig(config, db)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go app.reconciliationLoop(ctx)
	server := &http.Server{Addr: config.ListenAddress, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		slog.Info("media_service_started", "listen_address", config.ListenAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("media_service_failed", "error", err.Error())
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		slog.Error("http_shutdown_failed", "error", err.Error())
	}
	_ = sqlDB.Close()
}
