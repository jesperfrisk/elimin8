package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func TestRecapEndpoint(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo := testutil.NewMemory()
	for _, id := range []string{"g1", "g2"} {
		err := repo.Insert(ctx, &models.Game{
			ID: id, InvitationCode: strings.ToUpper(id), Status: "active", Revision: 1, StartedAt: &now,
			Players: []models.Player{{ID: "p0", Name: "Alice", JoinedAt: &now, Included: true}, {ID: "p1", Name: "Bob", JoinedAt: &now, Included: true}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	access := func(prefix, gameID, playerID string) string {
		tok := prefix + strings.Repeat("x", 48-len(prefix))
		if err := repo.PutAccess(ctx, models.Access{Hash: services.HashToken(tok), GameID: gameID, PlayerID: playerID, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		return tok
	}
	organizer, player, stranger := access("org", "g1", ""), access("ply", "g1", "p0"), access("oth", "g2", "")
	mux := http.NewServeMux()
	NewGameController(services.NewGameService(repo)).Register(mux)
	get := func(tok string) (int, string) {
		req := httptest.NewRequest("GET", "/api/games/g1/recap", nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}

	for _, tok := range []string{organizer, player} {
		if code, _ := get(tok); code != http.StatusConflict {
			t.Fatalf("active game recap = %d, want 409", code)
		}
	}
	if code, _ := get(""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", code)
	}
	if code, _ := get(stranger); code != http.StatusForbidden {
		t.Fatalf("other game = %d", code)
	}

	g, _ := repo.Get(ctx, "g1")
	g.Status, g.EndedAt = "ended", &now
	g.Players[1].EliminatedAt = &now
	repo.Games["g1"] = g
	for _, tok := range []string{organizer, player} {
		code, body := get(tok)
		if code != http.StatusOK {
			t.Fatalf("ended recap = %d %s", code, body)
		}
		for _, key := range []string{`"summary"`, `"leaderboard"`, `"timeline"`, `"awards"`, `"reason":"organizer"`} {
			if !strings.Contains(body, key) {
				t.Fatalf("missing %s in %s", key, body)
			}
		}
	}
}
