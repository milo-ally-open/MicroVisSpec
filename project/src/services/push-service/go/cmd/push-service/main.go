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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	config, err := LoadConfig()
	if err != nil {
		slog.Error("push_config_invalid", "error", err.Error())
		os.Exit(1)
	}
	db, err := openDatabase(config.DatabaseURL)
	if err != nil {
		slog.Error("push_database_open_failed", "error", err.Error())
		os.Exit(1)
	}
	sender := newVAPIDPushSender(config)
	app, err := NewApp(config, db, sender)
	if err != nil {
		slog.Error("push_app_init_failed", "error", err.Error())
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app.startOutboxWorker(ctx)
	server := &http.Server{Addr: config.ListenAddress, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("push_http_server_starting", "address", config.ListenAddress)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("push_http_shutdown_failed", "error", err.Error())
		}
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("push_http_server_failed", "error", err.Error())
			os.Exit(1)
		}
	}
}
