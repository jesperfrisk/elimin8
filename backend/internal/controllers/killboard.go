package controllers

import (
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func (c *GameController) registerKillBoard(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/games/{id}/killboard", c.killBoard)
}

// killBoard is open to the organizer and any authenticated participant of the
// game; authorized() already rejects other games' tokens.
func (c *GameController) killBoard(w http.ResponseWriter, r *http.Request) {
	_, g, ok := c.authorized(w, r)
	if !ok {
		return
	}
	gameJSON(w, 200, services.KillBoard(g))
}
