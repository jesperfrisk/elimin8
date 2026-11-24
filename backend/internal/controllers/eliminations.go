package controllers

import (
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func (c *GameController) reportElimination(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	var in struct {
		VictimID string `json:"victimId"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	claim, err := c.service.ReportElimination(r.Context(), a, in.VictimID)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, http.StatusCreated, claim)
}

func (c *GameController) respondElimination(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	var in struct {
		Accept *bool `json:"accept"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	if in.Accept == nil {
		gameError(w, services.ErrInvalid)
		return
	}
	if err := c.service.RespondElimination(r.Context(), a, r.PathValue("claim"), *in.Accept); err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
