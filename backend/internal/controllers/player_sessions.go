package controllers

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func (c *GameController) registerPlayerSessions(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/games/{id}/session", c.playerSession)
	mux.HandleFunc("DELETE /api/games/{id}/session", c.playerSession)
}

func (c *GameController) playerSession(w http.ResponseWriter, r *http.Request) {
	// Cookie-backed requests require Origin, unlike code login's CLI contract.
	if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") == "" || !c.playerLoginOriginAllowed(r) {
		gameJSON(w, http.StatusForbidden, map[string]string{"error": "Use the same origin as the game page."})
		return
	}
	// Use one bounded budget per method across all game IDs, preventing scans
	// from creating a separate allowance for every guessed game.
	if !c.allowN(r, "player-session:"+r.Method, 0) {
		w.Header().Set("Retry-After", "60")
		gameJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Too many attempts. Wait a minute and try again."})
		return
	}
	gameID := r.PathValue("id")
	if gameID == "" || len(gameID) > 128 || (&http.Cookie{Name: playerSessionCookieName(gameID)}).Valid() != nil {
		gameError(w, services.ErrInvalid)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil || (len(strings.TrimSpace(string(body))) != 0 && strings.TrimSpace(string(body)) != "{}") {
		gameError(w, services.ErrInvalid)
		return
	}
	var secret string
	// Reject ambiguous duplicate cookies instead of selecting an arbitrary one.
	cookies := r.CookiesNamed(playerSessionCookieName(gameID))
	if len(cookies) > 1 {
		gameError(w, services.ErrUnauthorized)
		return
	}
	if len(cookies) == 1 {
		secret = cookies[0].Value
	}
	if r.Method == http.MethodPost {
		admission, err := c.service.ResumePlayerSession(r.Context(), gameID, secret)
		if err != nil {
			gameError(w, err)
			return
		}
		gameJSON(w, http.StatusOK, admission)
		return
	}
	if err := c.service.ForgetPlayerSession(r.Context(), gameID, secret); err != nil {
		gameError(w, err)
		return
	}
	// Match issuance scope and flags; do not touch another game's cookie.
	http.SetCookie(w, &http.Cookie{Name: playerSessionCookieName(gameID), Path: playerSessionCookiePath(gameID), Value: "", Expires: time.Unix(1, 0).UTC(), MaxAge: -1, HttpOnly: true, Secure: c.sessionHTTPS(r), SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
