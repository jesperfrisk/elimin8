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

func (c *GameController) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/games", c.create)
	mux.HandleFunc("POST /api/access", c.login)
	mux.HandleFunc("GET /api/games/{id}", c.get)
	mux.HandleFunc("POST /api/games/{id}/players", c.addPlayers)
	mux.HandleFunc("POST /api/games/{id}/players/{player}/code", c.resetCode)
	mux.HandleFunc("POST /api/games/{id}/start", c.transition)
	mux.HandleFunc("POST /api/games/{id}/end", c.transition)
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
	case errors.Is(err, services.ErrConflict), errors.Is(err, services.ErrLobbyClosed):
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
	a, err := c.service.Login(r.Context(), in.InvitationCode, in.Code, in.Role)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 200, a)
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

func (c *GameController) get(w http.ResponseWriter, r *http.Request) {
	a, g, ok := c.authorized(w, r)
	if !ok {
		return
	}
	gameJSON(w, 200, services.View(g, a))
}

func (c *GameController) addPlayers(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	var in struct {
		Names []string `json:"names"`
	}
	if !decodeGame(w, r, &in) {
		return
	}
	issued, err := c.service.AddPlayers(r.Context(), a, in.Names)
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 201, map[string]any{"players": issued})
}

func (c *GameController) resetCode(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.authorized(w, r)
	if !ok {
		return
	}
	issued, err := c.service.ResetCode(r.Context(), a, r.PathValue("player"))
	if err != nil {
		gameError(w, err)
		return
	}
	gameJSON(w, 200, issued)
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
