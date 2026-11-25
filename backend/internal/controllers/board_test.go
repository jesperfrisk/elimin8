package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func boardServer(t *testing.T, status string) (*http.ServeMux, string) {
	t.Helper()
	repo := testutil.NewMemory()
	now := time.Now().UTC()
	g := &models.Game{ID: "board-game", Name: "Board", Status: status, Revision: 1, StartedAt: &now,
		Players: []models.Player{
			{ID: "a", Name: "Ann", Number: 1, Included: true, Mission: "secret mission", VictimID: "b", TeamID: "red", ConfirmedKills: 2, CodeHash: "hash"},
			{ID: "b", Name: "Bo", Number: 2, Included: true, EliminatedAt: &now, TeamID: "blue"},
		}}
	if err := repo.Insert(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewGameController(services.NewGameService(repo)).Register(mux)
	return mux, g.ID
}

func TestBoardEndpointIsPublicWithSameShapeInEveryState(t *testing.T) {
	for _, status := range []string{"lobby", "active", "ended"} {
		mux, id := boardServer(t, status)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/board/"+id, nil)) // no Authorization header
		if w.Code != 200 {
			t.Fatalf("%s: status %d: %s", status, w.Code, w.Body)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"gameId", "name", "status", "activeCount", "timer", "players"} {
			if _, ok := body[k]; !ok {
				t.Fatalf("%s: missing %q in %s", status, k, w.Body)
			}
		}
		if len(body) != 6 {
			t.Fatalf("%s: unexpected extra keys: %s", status, w.Body)
		}
		for _, secret := range []string{"secret mission", "red", "blue", "hash", "victim", "mission"} {
			if containsString(w.Body.String(), secret) {
				t.Fatalf("%s: response leaks %q: %s", status, secret, w.Body)
			}
		}
	}
}

func TestBoardEndpointUnknownGameIs404(t *testing.T) {
	mux, _ := boardServer(t, "active")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/board/does-not-exist", nil))
	if w.Code != 404 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestBoardEndpointIsRateLimitedPerClient(t *testing.T) {
	mux, id := boardServer(t, "active")
	limited := false
	for i := 0; i < boardRequestsPerMinute+5; i++ {
		r := httptest.NewRequest("GET", "/api/board/"+id, nil)
		r.RemoteAddr = "203.0.113.9:4000"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code == 429 {
			limited = true
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("missing Retry-After")
			}
			break
		}
	}
	if !limited {
		t.Fatal("expected a 429 after exceeding the board budget")
	}
	// Another client is unaffected.
	r := httptest.NewRequest("GET", "/api/board/"+id, nil)
	r.RemoteAddr = "198.51.100.7:4000"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("other client got %d", w.Code)
	}
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
