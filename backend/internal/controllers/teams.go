package controllers

import (
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func (c *GameController) registerTeams(mux *http.ServeMux) {
	mux.HandleFunc("PATCH /api/games/{id}/settings", c.setSettings)
	mux.HandleFunc("PUT /api/games/{id}/teams", c.setTeams)
	mux.HandleFunc("POST /api/games/{id}/teams/randomize", c.randomizeTeams)
}

func (c *GameController) setSettings(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	var in struct {
		Mode models.GameMode `json:"mode"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	if err := c.service.SetMode(r.Context(), a, in.Mode); err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (c *GameController) setTeams(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	var in struct {
		Teams []services.TeamInput `json:"teams"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	teams, err := c.service.SetTeams(r.Context(), a, in.Teams)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, http.StatusOK, map[string]any{"teams": teams})
}

func (c *GameController) randomizeTeams(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	var in struct {
		TeamCount int `json:"teamCount"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	teams, err := c.service.RandomizeTeams(r.Context(), a, in.TeamCount)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, http.StatusOK, map[string]any{"teams": teams})
}
