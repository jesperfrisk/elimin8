package controllers

import "net/http"

func (c *GameController) registerBoardRecap(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/board/{id}/recap", c.boardRecap)
}

// boardRecap serves the same payload as the authenticated recap with no token,
// for ended games only. It shares the board's per-IP budget and is never cached.
func (c *GameController) boardRecap(w http.ResponseWriter, r *http.Request) {
	if !c.allowN(r, "/api/board", boardRequestsPerMinute) {
		w.Header().Set("Retry-After", "60")
		gameJSON(w, 429, map[string]string{"error": "Too many requests. Wait a minute and try again."})
		return
	}
	recap, err := c.service.BoardRecap(r.Context(), r.PathValue("id"))
	if err != nil {
		gameError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	gameJSON(w, 200, recap)
}
