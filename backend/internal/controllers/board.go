package controllers

import "net/http"

// The public board is unauthenticated, so it gets its own per-IP budget that
// is shared across game ids (scanning many ids is bounded too) and sized for a
// venue where several screens sit behind one address.
const boardRequestsPerMinute = 240

func (c *GameController) registerBoard(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/board/{id}", c.board)
	c.registerBoardRecap(mux)
}

func (c *GameController) board(w http.ResponseWriter, r *http.Request) {
	if !c.allowN(r, "/api/board", boardRequestsPerMinute) {
		w.Header().Set("Retry-After", "60")
		gameJSON(w, 429, map[string]string{"error": "Too many requests. Wait a minute and try again."})
		return
	}
	view, err := c.service.Board(r.Context(), r.PathValue("id"))
	if err != nil {
		gameError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	gameJSON(w, 200, view)
}
