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

	"github.com/jesperfrisk/elimin8/backend/internal/config"
	"github.com/jesperfrisk/elimin8/backend/internal/controllers"
	"github.com/jesperfrisk/elimin8/backend/internal/database"
	"github.com/jesperfrisk/elimin8/backend/internal/repositories/mongodb"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func main() {
	if err := run(); err != nil {
		// Connection errors may contain credentials; keep detailed driver errors out of logs.
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := database.Connect(connectCtx, cfg.MongoDBURI, cfg.MongoDBDatabase)
	cancel()
	if err != nil {
		return errors.New("MongoDB connection failed; check MONGODB_URI and database availability")
	}
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Client().Disconnect(disconnectCtx); err != nil {
			slog.Warn("MongoDB disconnect failed")
		}
	}()

	health := controllers.NewHealthController(services.NewHealthService(mongodb.NewHealthRepository(db)))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health/live", health.Liveness)
	mux.HandleFunc("GET /api/health/ready", health.Readiness)

	server := &http.Server{
		Addr: cfg.HTTPAddress, Handler: mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("API listening", "address", cfg.HTTPAddress)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		slog.Info("API shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}
	return nil
}
