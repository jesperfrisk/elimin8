package controllers

import (
	"net/http"
)

func (c *GameController) registerPlayers(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/games/{id}/players", c.addPlayers)
	mux.HandleFunc("POST /api/games/{id}/players/{player}/code", c.resetCode)
}

func (c *GameController) addPlayers(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	var in struct {
		Names []string `json:"names"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	issued, err := c.service.AddPlayers(r.Context(), a, in.Names)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 201, map[string]any{"players": issued})
}

func (c *GameController) resetCode(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	issued, err := c.service.ResetCode(r.Context(), a, r.PathValue("player"))
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 200, issued)
}
