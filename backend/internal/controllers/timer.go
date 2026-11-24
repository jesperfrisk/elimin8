package controllers

import (
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func (c *GameController) registerTimer(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/games/{id}/timer", c.setTimer)
}

func (c *GameController) setTimer(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	if a.PlayerID != "" {
		gameError(w, services.ErrForbidden)
		return
	}
	var in struct {
		DurationMinutes *int `json:"durationMinutes"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	if err := c.service.SetTimer(r.Context(), a, in.DurationMinutes); err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 200, map[string]string{"status": "ok"})
}
