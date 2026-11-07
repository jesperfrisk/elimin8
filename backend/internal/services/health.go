package services

import (
	"context"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

type DatabasePinger interface {
	Ping(context.Context) error
}

type HealthService struct {
	database DatabasePinger
}

func NewHealthService(database DatabasePinger) *HealthService {
	return &HealthService{database: database}
}

func (s *HealthService) Readiness(ctx context.Context) (models.HealthResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.database.Ping(ctx); err != nil {
		return models.HealthResponse{Status: "unavailable", Database: "disconnected"}, err
	}
	return models.HealthResponse{Status: "ok", Database: "connected"}, nil
}
