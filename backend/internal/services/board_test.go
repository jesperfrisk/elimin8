package services_test

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func newTestService(t *testing.T) (*services.GameService, *testutil.Memory) {
	t.Helper()
	repo := testutil.NewMemory()
	return services.NewGameService(repo), repo
}

func boardKeys(t *testing.T, v any) []string {
	t.Helper()
	data, _ := json.Marshal(v)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestBoardExactKeysAndNoSecretsInEveryGameState(t *testing.T) {
	secret := regexp.MustCompile(`(?i)mission|victim|hunter|killer|team|kills|code|hash|withdrawnAt|eliminatedAt`)
	for _, status := range []string{"lobby", "active", "ended"} {
		now := time.Now().UTC()
		ends := now.Add(time.Hour)
		g := &models.Game{ID: "g1", Name: "Board", Status: status, Mode: models.ModeTeam, Teams: []models.Team{{ID: "t", Name: "SecretTeamName"}}, Timer: &models.Timer{DurationMinutes: 60, EndsAt: &ends}, WinnerID: "0",
			Players: []models.Player{
				{ID: "0", Name: "Ann", Included: true, Mission: "Hidden mission", VictimID: "1", TeamID: "t", ConfirmedKills: 3, CodeHash: "abc"},
				{ID: "1", Name: "Bo", Included: true, EliminatedAt: &now, TeamID: "t"},
				{ID: "2", Name: "Cy", Included: true, WithdrawnAt: &now},
			}}
		v := services.BuildBoard(g, now)
		if !reflect.DeepEqual(boardKeys(t, v), []string{"activeCount", "gameId", "name", "players", "status", "timer"}) {
			t.Fatalf("%s: unexpected keys %v", status, boardKeys(t, v))
		}
		data, _ := json.Marshal(v)
		var raw struct {
			Players []map[string]any `json:"players"`
			Timer   map[string]any   `json:"timer"`
		}
		_ = json.Unmarshal(data, &raw)
		for _, p := range raw.Players {
			keys := make([]string, 0)
			for k := range p {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, []string{"id", "name", "number", "status"}) {
				t.Fatalf("%s: player keys %v", status, keys)
			}
		}
		if raw.Timer != nil {
			keys := make([]string, 0)
			for k := range raw.Timer {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, []string{"endReason", "endsAt", "minutes", "remainingSeconds"}) {
				t.Fatalf("%s: timer keys %v", status, keys)
			}
		}
		// The only matches allowed are the documented key names themselves.
		body := strings.ReplaceAll(string(data), `"remainingSeconds"`, "")
		if m := secret.FindString(body); m != "" {
			t.Fatalf("%s: board leaks %q: %s", status, m, data)
		}
		if status == "lobby" && len(v.Players) != 0 {
			t.Fatalf("lobby lists players: %v", v.Players)
		}
		if status != "lobby" {
			if len(v.Players) != 3 || v.ActiveCount != 1 {
				t.Fatalf("%s: players=%d active=%d", status, len(v.Players), v.ActiveCount)
			}
			if v.Players[1].Status != "eliminated" || v.Players[2].Status != "withdrawn" {
				t.Fatalf("%s: statuses %+v", status, v.Players)
			}
		}
	}
}

func TestBoardOrderIsStableByDisplayNumber(t *testing.T) {
	s, repo, a := eliminationGame(t, 5)
	ctx := context.Background()
	numbers := func() []int {
		v, err := s.Board(ctx, a[0].GameID)
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		for _, p := range v.Players {
			out = append(out, p.Number)
		}
		return out
	}
	before := numbers()
	if !sort.IntsAreSorted(before) || before[0] != 1 {
		t.Fatalf("expected ascending numbers from 1, got %v", before)
	}
	kill(t, s, a[0], a[1])
	kill(t, s, a[0], a[2])
	if after := numbers(); !reflect.DeepEqual(before, after) {
		t.Fatalf("numbers changed after eliminations: %v -> %v", before, after)
	}
	_ = repo
}

func TestBoardUnknownGameIsNotFound(t *testing.T) {
	s, _, _ := eliminationGame(t, 3)
	if _, err := s.Board(context.Background(), "nope"); err != services.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAddedPlayersGetStableNeverReusedNumbers(t *testing.T) {
	ctx := context.Background()
	s, repo := newTestService(t)
	admission, err := s.Create(ctx, "Numbers", "")
	if err != nil {
		t.Fatal(err)
	}
	owner := models.Access{GameID: admission.Game.ID}
	if _, err = s.AddPlayers(ctx, owner, []string{"A", "B"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddPlayers(ctx, owner, []string{"C"}); err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, owner.GameID)
	for i, p := range g.Players {
		if p.Number != i+1 {
			t.Fatalf("player %d has number %d", i, p.Number)
		}
	}
}

func TestNewGameIDsHaveAtLeast128BitsOfEntropy(t *testing.T) {
	// 32-character alphabet: 5 bits per character.
	if services.GameIDLength*5 < 128 {
		t.Fatalf("game id too short: %d bits", services.GameIDLength*5)
	}
	s, _ := newTestService(t)
	a, err := s.Create(context.Background(), "Entropy", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Game.ID) != services.GameIDLength {
		t.Fatalf("id length %d, want %d", len(a.Game.ID), services.GameIDLength)
	}
}

func TestBoardDigestIgnoresTickingTimeButNotStatus(t *testing.T) {
	ends := time.Now().Add(time.Hour)
	now := time.Now().UTC()
	g := &models.Game{ID: "g", Status: "active", Timer: &models.Timer{DurationMinutes: 60, EndsAt: &ends},
		Players: []models.Player{{ID: "0", Name: "A", Included: true}, {ID: "1", Name: "B", Included: true}}}
	before := services.BoardDigest(g)
	if services.BoardDigest(g) != before {
		t.Fatal("digest not deterministic")
	}
	g.Players[0].VictimID, g.Players[0].Mission = "1", "private change"
	if services.BoardDigest(g) != before {
		t.Fatal("private change altered the board digest")
	}
	g.Players[1].EliminatedAt = &now
	if services.BoardDigest(g) == before {
		t.Fatal("status change did not alter the digest")
	}
}
