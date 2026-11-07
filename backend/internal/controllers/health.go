package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

type HealthController struct {
	service *services.HealthService
}

func NewHealthController(service *services.HealthService) *HealthController {
	return &HealthController{service: service}
}

func (c *HealthController) Liveness(w http.ResponseWriter, r *http.Request) {
	writeHealth(w, http.StatusOK, models.HealthResponse{Status: "ok"})
}

func (c *HealthController) Readiness(w http.ResponseWriter, r *http.Request) {
	response, err := c.service.Readiness(r.Context())
	status := http.StatusOK
	if err != nil {
		status = http.StatusServiceUnavailable
	}
	writeHealth(w, status, response)
}

func writeHealth(w http.ResponseWriter, status int, response models.HealthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
