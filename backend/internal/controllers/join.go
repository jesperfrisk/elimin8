package controllers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func (c *GameController) registerJoin(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/join", c.join)
}

// joinFailure maps self-join errors to the published contract: 409 with a
// machine-readable code for the two cases a person can act on, and the code of a
// registration that committed but could not finish signing in.
func joinFailure(w http.ResponseWriter, err error) {
	var incomplete *services.RegistrationIncompleteError
	switch {
	case errors.As(err, &incomplete):
		gameJSON(w, http.StatusInternalServerError, map[string]string{
			"error":        "You are registered, but signing you in did not finish. Sign in with your recovery code.",
			"code":         "registration_incomplete",
			"recoveryCode": incomplete.RecoveryCode,
		})
	case errors.Is(err, services.ErrNameTaken):
		gameJSON(w, http.StatusConflict, map[string]string{"error": "That name is already taken in this game. Choose another.", "code": "name_taken"})
	case errors.Is(err, services.ErrLobbyClosed):
		gameJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "lobby_closed"})
	default:
		gameError(w, err)
	}
}

// join registers a new player in an open lobby (201, with the private
// recoveryCode and the remember cookie), or, when a valid player bearer of the
// same game is supplied, returns that player's fresh admission (200) without
// creating anyone, without a recovery code and without touching the cookie. The
// remember cookie is scoped to /api/games/{id}/session, so it is never sent here.
func (c *GameController) join(w http.ResponseWriter, r *http.Request) {
	if c.limited(w, r) {
		return
	}
	var in struct {
		InvitationCode string `json:"invitationCode"`
		Name           string `json:"name"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	if !c.playerLoginOriginAllowed(r) {
		gameJSON(w, http.StatusForbidden, map[string]string{"error": "Use the same origin as the game page."})
		return
	}
	if header := r.Header.Get("Authorization"); header != "" {
		admission, err := c.service.JoinResume(r.Context(), in.InvitationCode, strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			joinFailure(w, err)
			return
		}
		gameJSON(w, http.StatusOK, admission)
		return
	}
	result, err := c.service.Join(r.Context(), in.InvitationCode, in.Name)
	if err != nil {
		joinFailure(w, err)
		return
	}
	secret, session, err := c.service.IssuePlayerSession(r.Context(), result.Token)
	if err == nil {
		if err = c.setPlayerSessionCookie(w, r, result.Game.ID, secret, session.ExpiresAt); err != nil {
			_ = c.service.ForgetPlayerSession(r.Context(), result.Game.ID, secret) // best effort: no orphaned credential
		}
	}
	if err != nil {
		// The player exists: say so honestly and hand back the code so they can sign in.
		joinFailure(w, &services.RegistrationIncompleteError{RecoveryCode: result.RecoveryCode, Err: err})
		return
	}
	gameJSON(w, http.StatusCreated, result)
}
