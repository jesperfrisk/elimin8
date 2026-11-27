package controllers

import (
	"net/http"
	"strings"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func (c *GameController) registerLifecycle(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/games", c.create)
	mux.HandleFunc("POST /api/access", c.login)
	mux.HandleFunc("GET /api/games/{id}", c.get)
	mux.HandleFunc("POST /api/games/{id}/start", c.transition)
	mux.HandleFunc("POST /api/games/{id}/end", c.transition)
}

func (c *GameController) create(w http.ResponseWriter, r *http.Request) {
	if c.limited(w, r) {
		return
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	a, err := c.service.Create(r.Context(), in.Name, in.Description)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 201, a)
}

func (c *GameController) login(w http.ResponseWriter, r *http.Request) {
	if c.limited(w, r) {
		return
	}
	var in struct {
		InvitationCode string `json:"invitationCode"`
		Code           string `json:"code"`
		Role           string `json:"role"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	if in.Role == "player" && !c.playerLoginOriginAllowed(r) {
		gameJSON(w, http.StatusForbidden, map[string]string{"error": "Use the same origin as the game page."})
		return
	}
	a, err := c.service.Login(r.Context(), in.InvitationCode, in.Code, in.Role)
	if err != nil {
		gameError(w, err)
		return
	}
	if a.Role == "player" {
		secret, session, err := c.service.IssuePlayerSession(r.Context(), a.Token)
		if err != nil {
			gameError(w, err)
			return
		}
		if err := c.setPlayerSessionCookie(w, r, a.Game.ID, secret, session.ExpiresAt); err != nil {
			gameError(w, err)
			return
		}
	}
	gameJSON(w, 200, a)
}
func (c *GameController) get(w http.ResponseWriter, r *http.Request) {
	a, g, ok := c.authorized(w, r)
	if !ok {
		return
	}
	gameJSON(w, 200, services.View(g, a))
}
func (c *GameController) transition(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if err := c.service.Transition(r.Context(), a, action); err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 200, map[string]string{"status": "ok"})
}
