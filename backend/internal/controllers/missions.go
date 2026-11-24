package controllers

import (
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func (c *GameController) addMissions(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	if a.PlayerID != "" {
		gameError(w, services.ErrForbidden)
		return
	}
	var in struct {
		Texts []string `json:"texts"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	missions, err := c.service.AddMissions(r.Context(), a, in.Texts)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 201, map[string]any{"missions": missions})
}

func (c *GameController) setMissionEnabled(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	if a.PlayerID != "" {
		gameError(w, services.ErrForbidden)
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		gameError(w, services.ErrInvalid)
		return
	}
	if err := c.service.SetMissionEnabled(r.Context(), a, r.PathValue("mission"), *in.Enabled); err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 200, map[string]string{"status": "ok"})
}

func (c *GameController) registerMissions(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/games/{id}/missions", c.addMissions)
	mux.HandleFunc("PATCH /api/games/{id}/missions/{mission}", c.setMissionEnabled)
}
