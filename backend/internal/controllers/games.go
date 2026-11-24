package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

type attemptWindow struct {
	count int
	until time.Time
}
type GameController struct {
	service  *services.GameService
	mu       sync.Mutex
	attempts map[string]attemptWindow
}

func NewGameController(s *services.GameService) *GameController {
	return &GameController{service: s, attempts: map[string]attemptWindow{}}
}

// Register wires every domain's routes. To add a domain, create
// <domain>.go with a register<Domain>(mux) method and add one line here.
func (c *GameController) Register(mux *http.ServeMux) {
	c.registerLifecycle(mux)
	c.registerPlayers(mux)
	c.registerMissions(mux)
	c.registerEliminations(mux)
	c.registerTeams(mux)
	c.registerWithdrawals(mux)
	c.registerTimer(mux)
	c.registerResults(mux)
}

func gameJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func gameError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "Something went wrong. Please try again."
	switch {
	case errors.Is(err, services.ErrInvalid):
		status = 400
		message = "Check the supplied values and their lengths."
	case errors.Is(err, services.ErrUnauthorized):
		status = 401
		message = err.Error()
	case errors.Is(err, services.ErrForbidden):
		status = 403
		message = err.Error()
	case errors.Is(err, services.ErrNotFound):
		status = 404
		message = err.Error()
	case errors.Is(err, services.ErrTooFewPlayers):
		status = 409
		message = "At least two players must join the lobby before you can start the game. Invited players who have not joined do not count."
	case errors.Is(err, services.ErrNoMissions):
		status = 409
		message = "Add or enable at least one mission before starting with joined players."
	case errors.Is(err, services.ErrAlreadyStarted):
		status = 409
		message = "This game has already started. Refresh the game to see its current state."
	case errors.Is(err, services.ErrGameEnded):
		status = 409
		message = "This game has ended and cannot be started again. Create a new game to play again."
	case errors.Is(err, services.ErrConflict):
		status = 409
		message = "The game changed while you were working. Refresh the game and try again."
	case errors.Is(err, services.ErrElimination), errors.Is(err, services.ErrPendingElimination), errors.Is(err, services.ErrWithdrawal):
		status = 409
		message = err.Error()
	case errors.Is(err, services.ErrTeamSetup), errors.Is(err, services.ErrNoValidAssignment):
		status = 409
		message = err.Error()
	case errors.Is(err, services.ErrLobbyClosed):
		status = 409
		message = err.Error()
	default:
		slog.Error("game request failed")
	}
	gameJSON(w, status, map[string]string{"error": message})
}

func decodeGame(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		gameError(w, services.ErrInvalid)
		return false
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		gameError(w, services.ErrInvalid)
		return false
	}
	return true
}

// Bound both expensive code hashing and anonymous game creation. RemoteAddr is
// deliberately not replaced with untrusted forwarding headers.
func (c *GameController) allow(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := host + ":" + r.URL.Path
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.attempts {
		if now.After(v.until) {
			delete(c.attempts, k)
		}
	}
	v := c.attempts[key]
	if v.until.IsZero() {
		v.until = now.Add(time.Minute)
	}
	if v.count >= 30 || len(c.attempts) >= 10000 {
		return false
	}
	v.count++
	c.attempts[key] = v
	return true
}

func (c *GameController) limited(w http.ResponseWriter, r *http.Request) bool {
	if c.allow(r) {
		return false
	}
	w.Header().Set("Retry-After", "60")
	gameJSON(w, 429, map[string]string{"error": "Too many attempts. Wait a minute and try again."})
	return true
}
func (c *GameController) authorized(w http.ResponseWriter, r *http.Request) (models.Access, *models.Game, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	a, g, err := c.service.Authorize(r.Context(), token)
	if err != nil {
		gameError(w, err)
		return a, nil, false
	}
	if g.ID != r.PathValue("id") {
		gameError(w, services.ErrForbidden)
		return a, nil, false
	}
	return a, g, true
}
