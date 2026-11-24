package controllers

import (
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

// getRecap serves the post-game recap to the organizer and any participant
// (authorized already checks the caller belongs to this game).
func (c *GameController) getRecap(w http.ResponseWriter, r *http.Request) {
	_, g, ok := c.authorized(w, r)
	if !ok {
		return
	}
	recap, err := services.RecapOf(g)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, http.StatusOK, recap)
}

func (c *GameController) registerRecap(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/games/{id}/recap", c.getRecap)
}
