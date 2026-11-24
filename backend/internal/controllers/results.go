package controllers

import (
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

// getResult serves the final result to the organizer and to any participant of
// the game (authorized already checks the caller belongs to this game).
func (c *GameController) getResult(w http.ResponseWriter, r *http.Request) {
	_, g, ok := c.authorized(w, r)
	if !ok {
		return
	}
	result, err := services.ResultOf(g)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, http.StatusOK, result)
}

func (c *GameController) registerResults(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/games/{id}/result", c.getResult)
}
