package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func recapBoardGame(kind string) *models.Game {
	start := time.Now().UTC().Add(-time.Hour)
	at := func(s int) *time.Time { t := start.Add(time.Duration(s) * time.Second); return &t }
	end := *at(600)
	g := &models.Game{
		ID: "rg", Name: "Recap", InvitationCode: "INVITECODE", Status: "ended", Revision: 1, StartedAt: &start, EndedAt: &end,
		Players: []models.Player{
			{ID: "a", Name: "Alice", Included: true, VictimID: "SECRETVICTIM", Mission: "SECRET MISSION", CodeHash: "SECRETHASH", ConfirmedKills: 1},
			{ID: "b", Name: "Bob", Included: true, EliminatedAt: at(60), Mission: "SECRET MISSION"},
			{ID: "c", Name: "Cara", Included: true},
		},
		Eliminations: []models.Elimination{{ID: "e1", KillerID: "KILLERID", VictimID: "b", Status: "accepted"}},
	}
	switch kind {
	case "organizer":
		g.Players[0].ConfirmedKills = 0
	case "zero":
		g.EndReason = "timer"
		g.Players[0].ConfirmedKills = 0
		g.Players[1].EliminatedAt = nil
		g.Eliminations = nil
	case "tie":
		g.EndReason = "timer"
		g.Result = &models.GameResult{Reason: "timer", Tie: true, Standings: []models.ResultStanding{
			{Kind: "player", ID: "a", Name: "Alice", Kills: 1}, {Kind: "player", ID: "c", Name: "Cara", Kills: 1}}}
	case "team":
		g.EndReason = "timer"
		g.Mode = models.ModeTeam
		g.Teams = []models.Team{{ID: "t1", Name: "Red"}, {ID: "t2", Name: "Blue"}}
		g.Players[0].TeamID, g.Players[1].TeamID, g.Players[2].TeamID = "t1", "t2", "t2"
		g.Result = &models.GameResult{Reason: "timer", Standings: []models.ResultStanding{
			{Kind: "team", ID: "t1", Name: "Red", Kills: 1}, {Kind: "team", ID: "t2", Name: "Blue", Kills: 0}}}
	default: // individual
		g.EndReason = "timer"
		g.Result = &models.GameResult{Reason: "timer", Standings: []models.ResultStanding{
			{Kind: "player", ID: "a", Name: "Alice", Kills: 1}}}
	}
	return g
}

func recapBoardMux(t *testing.T, g *models.Game) *http.ServeMux {
	t.Helper()
	repo := testutil.NewMemory()
	if err := repo.Insert(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewGameController(services.NewGameService(repo)).Register(mux)
	return mux
}

func assertKeys(t *testing.T, what string, raw json.RawMessage, want ...string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s keys = %v, want %v", what, got, want)
	}
}

func TestBoardRecapIsPublicExactShapeAndLeaksNothing(t *testing.T) {
	for _, kind := range []string{"individual", "team", "tie", "zero", "organizer"} {
		mux := recapBoardMux(t, recapBoardGame(kind))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/board/rg/recap", nil)) // no token
		if w.Code != 200 {
			t.Fatalf("%s: status %d: %s", kind, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: Cache-Control = %q", kind, w.Header().Get("Cache-Control"))
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &top); err != nil {
			t.Fatal(err)
		}
		assertKeys(t, kind+" top", w.Body.Bytes(), "summary", "leaderboard", "timeline", "awards")
		assertKeys(t, kind+" summary", top["summary"], "durationSeconds", "players", "eliminations", "withdrawals", "declinedReports", "reason", "tie")
		assertKeys(t, kind+" awards", top["awards"], "longestSurvivor", "firstElimination", "fastestElimination", "mostKills", "lastElimination")
		var board, tl []json.RawMessage
		_ = json.Unmarshal(top["leaderboard"], &board)
		for _, row := range board {
			assertKeys(t, kind+" leaderboard row", row, "kind", "id", "name", "kills")
		}
		_ = json.Unmarshal(top["timeline"], &tl)
		for _, row := range tl {
			assertKeys(t, kind+" timeline row", row, "playerId", "name", "event", "atSeconds")
		}
		body := strings.ToLower(w.Body.String())
		for _, secret := range []string{"secret", "killerid", "invitecode", "killer", "victim", "mission", "target", "code", "teamid", "members"} {
			if strings.Contains(body, secret) {
				t.Fatalf("%s: response leaks %q: %s", kind, secret, w.Body)
			}
		}
		if kind != "team" {
			for _, team := range []string{"red", "blue"} {
				if strings.Contains(body, team) {
					t.Fatalf("%s: unexpected team name %q", kind, team)
				}
			}
		}
	}
}

func TestBoardRecapMatchesAuthenticatedPayload(t *testing.T) {
	g := recapBoardGame("team")
	want, err := services.RecapOf(g)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	w := httptest.NewRecorder()
	recapBoardMux(t, g).ServeHTTP(w, httptest.NewRequest("GET", "/api/board/rg/recap", nil))
	if strings.TrimSpace(w.Body.String()) != string(wantJSON) {
		t.Fatalf("payload differs:\n%s\n%s", wantJSON, w.Body)
	}
}

func TestBoardRecapBeforeEndIs409AndUnknownIs404(t *testing.T) {
	g := recapBoardGame("individual")
	g.Status, g.EndedAt = "active", nil
	mux := recapBoardMux(t, g)
	for path, want := range map[string]int{"/api/board/rg/recap": 409, "/api/board/nope/recap": 404} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatalf("%s = %d, want %d", path, w.Code, want)
		}
		if !strings.Contains(w.Body.String(), `"error"`) {
			t.Fatalf("%s: body %s lacks error shape", path, w.Body)
		}
	}
}

func TestBoardRecapIsRateLimitedWithBoardBudget(t *testing.T) {
	mux := recapBoardMux(t, recapBoardGame("individual"))
	limited := false
	for i := 0; i < boardRequestsPerMinute+5; i++ {
		r := httptest.NewRequest("GET", "/api/board/rg/recap", nil)
		r.RemoteAddr = "203.0.113.9:4000"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code == 429 {
			limited = w.Header().Get("Retry-After") != ""
			break
		}
	}
	if !limited {
		t.Fatal("expected a 429 with Retry-After")
	}
}
