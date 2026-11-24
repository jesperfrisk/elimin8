package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func TestResultEndpoint(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo := testutil.NewMemory()
	game := func(id string) *models.Game {
		return &models.Game{
			ID: id, InvitationCode: strings.ToUpper(id), Status: "active", Revision: 1, StartedAt: &now,
			Players: []models.Player{
				{ID: "p0", Name: "Alice", JoinedAt: &now, Included: true, ConfirmedKills: 2},
				{ID: "p1", Name: "Bob", JoinedAt: &now, Included: true},
			},
		}
	}
	for _, id := range []string{"g1", "g2"} {
		if err := repo.Insert(ctx, game(id)); err != nil {
			t.Fatal(err)
		}
	}
	token := func(prefix string) string { return prefix + strings.Repeat("x", 48-len(prefix)) }
	access := func(prefix, gameID, playerID string) string {
		tok := token(prefix)
		err := repo.PutAccess(ctx, models.Access{Hash: services.HashToken(tok), GameID: gameID, PlayerID: playerID, ExpiresAt: now.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	organizer, player, stranger := access("org", "g1", ""), access("ply", "g1", "p0"), access("oth", "g2", "")

	mux := http.NewServeMux()
	NewGameController(services.NewGameService(repo)).Register(mux)
	get := func(tok string) (int, map[string]any) {
		req := httptest.NewRequest("GET", "/api/games/g1/result", nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	// Before the game ends nobody gets a result, including the organizer.
	for _, tok := range []string{organizer, player} {
		if code, _ := get(tok); code != http.StatusConflict {
			t.Fatalf("active game result = %d, want 409", code)
		}
	}
	if code, _ := get(""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", code)
	}
	if code, _ := get(stranger); code != http.StatusForbidden {
		t.Fatalf("other game's organizer = %d, want 403", code)
	}

	// Ended by the organizer: neither survival nor timer, so no result (404).
	g, _ := repo.Get(ctx, "g1")
	g.Status, g.EndedAt = "ended", &now
	repo.Games["g1"] = g
	if code, body := get(organizer); code != http.StatusNotFound || body["error"] == nil {
		t.Fatalf("organizer-ended game = %d %v, want 404", code, body)
	}

	// Ended by timer expiry with a stored result: organizer and players read it.
	g, _ = repo.Get(ctx, "g1")
	g.EndReason = "timer"
	g.Result = &models.GameResult{
		Reason:    "timer",
		Winners:   []models.ResultWinner{{Kind: "player", ID: "p0", Name: "Alice"}},
		Standings: []models.ResultStanding{{Kind: "player", ID: "p0", Name: "Alice", Kills: 2}, {Kind: "player", ID: "p1", Name: "Bob"}},
	}
	repo.Games["g1"] = g
	for _, tok := range []string{organizer, player} {
		code, body := get(tok)
		if code != http.StatusOK {
			t.Fatalf("ended game result = %d %v, want 200", code, body)
		}
		if len(body) != 4 || body["reason"] != "timer" || body["tie"] != false {
			t.Fatalf("unexpected result body: %v", body)
		}
		standings := body["standings"].([]any)
		first := standings[0].(map[string]any)
		if len(standings) != 2 || first["name"] != "Alice" || first["kills"] != float64(2) || len(first) != 4 {
			t.Fatalf("unexpected standings: %v", standings)
		}
	}
}
