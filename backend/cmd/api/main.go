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
	"github.com/jesperfrisk/elimin8/backend/internal/realtime"
	"github.com/jesperfrisk/elimin8/backend/internal/repositories/mongodb"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/storage"
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
	if err := cfg.ValidatePhotoStorage(); err != nil {
		return errors.New("photo storage configuration is invalid; check PHOTO_STORAGE settings")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var photoStore storage.Store
	var photoRetention time.Duration
	if cfg.PhotoStorage.Endpoint != "" {
		retention, err := config.LoadPhotoRetention()
		if err != nil {
			return err
		}
		photoRetention = retention
		storageCtx, storageCancel := context.WithTimeout(ctx, 15*time.Second)
		// Configured storage must pass private-store checks before the API starts.
		store, err := storage.NewS3(storageCtx, cfg.PhotoStorage)
		storageCancel()
		if err != nil {
			return errors.New("private photo storage could not be initialized; check PHOTO_STORAGE settings and storage availability")
		}
		photoStore = store
	}

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
	gameRepo := mongodb.NewGameRepository(db)
	indexCtx, indexCancel := context.WithTimeout(ctx, 10*time.Second)
	err = gameRepo.EnsureIndexes(indexCtx)
	if err == nil && photoStore != nil {
		err = gameRepo.EnsurePhotoRetentionIndex(indexCtx)
	}
	indexCancel()
	if err != nil {
		return errors.New("game database indexes could not be initialized")
	}
	gameService := services.NewGameService(gameRepo)
	broker, err := realtime.New(gameService)
	if err != nil {
		return errors.New("MQTT broker could not be initialized")
	}
	defer broker.Close()
	gameService.Notify = broker.Notify
	outboxCtx, stopOutbox := context.WithCancel(ctx)
	defer stopOutbox()
	go broker.RunOutbox(outboxCtx, gameRepo)
	go gameService.RunTimerSweep(outboxCtx, 5*time.Second)
	controllers.NewGameController(gameService).Register(mux)
	if photoStore != nil {
		photos := services.NewPhotoService(gameService, photoStore)
		photos.Retention = photoRetention
		controllers.NewPhotoController(gameService, photos).Register(mux)
		go services.NewPhotoRetentionService(gameService, gameRepo, photoStore, photoRetention).Run(outboxCtx, time.Minute)
	}
	mux.Handle("GET /api/mqtt", broker)

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
