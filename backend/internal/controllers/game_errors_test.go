package controllers

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func TestGameStartErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"too few joined players", services.ErrTooFewPlayers, 409, "At least two players must join the lobby before you can start the game. Invited players who have not joined do not count."},
		{"no enabled missions", services.ErrNoMissions, 409, "Add or enable at least one mission before starting with joined players."},
		{"already started", services.ErrAlreadyStarted, 409, "This game has already started. Refresh the game to see its current state."},
		{"ended", services.ErrGameEnded, 409, "This game has ended and cannot be started again. Create a new game to play again."},
		{"concurrent change", services.ErrConflict, 409, "The game changed while you were working. Refresh the game and try again."},
		{"expired access", services.ErrUnauthorized, 401, services.ErrUnauthorized.Error()},
		{"wrong role", services.ErrForbidden, 403, services.ErrForbidden.Error()},
		{"missing game", services.ErrNotFound, 404, services.ErrNotFound.Error()},
		{"invalid data", services.ErrInvalid, 400, "Check the supplied values and their lengths."},
		{"internal failure", errors.New("private database connection details"), 500, "Something went wrong. Please try again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			gameError(w, tc.err)
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || body["error"] != tc.message {
				t.Fatalf("got %d %+v", w.Code, body)
			}
		})
	}
}
