package services_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func TestKillBoardExposesOnlyIDNameStatus(t *testing.T) {
	s, repo, a := eliminationGame(t, 4)
	ctx := context.Background()
	kill(t, s, a[0], a[1])
	g, _ := repo.Get(ctx, a[0].GameID)
	g.Players[2].WithdrawnAt = ptrTime(time.Now())
	g.Players[3].TeamID = "secret-team"
	g.Players[3].ConfirmedKills = 5

	data, err := json.Marshal(services.KillBoard(g))
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Entries []map[string]any `json:"entries"`
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(raw.Entries))
	}
	want := map[string]string{"0": "active", "1": "eliminated", "2": "withdrawn", "3": "active"}
	for _, e := range raw.Entries {
		keys := make([]string, 0, len(e))
		for k := range e {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if !reflect.DeepEqual(keys, []string{"id", "name", "status"}) {
			t.Fatalf("unexpected keys %v", keys)
		}
		if e["status"] != want[e["id"].(string)] {
			t.Fatalf("player %v: got %v want %v", e["id"], e["status"], want[e["id"].(string)])
		}
	}
	for _, leak := range []string{"secret-team", "Mission", "victim", "killer", "At\""} {
		if containsFold(string(data), leak) {
			t.Fatalf("board leaks %q: %s", leak, data)
		}
	}
}

func TestKillBoardOrderDoesNotRevealEliminationOrder(t *testing.T) {
	s, repo, a := eliminationGame(t, 5)
	ctx := context.Background()
	names := func() []string {
		g, _ := repo.Get(ctx, a[0].GameID)
		var out []string
		for _, e := range services.KillBoard(g).Entries {
			out = append(out, e.Name)
		}
		return out
	}
	before := names()
	if !sort.StringsAreSorted(before) {
		t.Fatalf("not sorted by name: %v", before)
	}
	kill(t, s, a[0], a[1])
	kill(t, s, a[0], a[2])
	if after := names(); !reflect.DeepEqual(before, after) {
		t.Fatalf("order changed after eliminations: %v -> %v", before, after)
	}
}

func TestKillBoardOmitsLobbyAndExcludedPlayers(t *testing.T) {
	g := &models.Game{Status: "lobby", Players: []models.Player{{ID: "1", Name: "A", Included: true}}}
	if got := services.KillBoard(g).Entries; len(got) != 0 {
		t.Fatalf("lobby board should be empty, got %v", got)
	}
	g = &models.Game{Status: "active", Players: []models.Player{{ID: "1", Name: "A", Included: true}, {ID: "2", Name: "B"}}}
	if got := services.KillBoard(g).Entries; len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("excluded player listed: %v", got)
	}
	data, _ := json.Marshal(services.KillBoard(&models.Game{Status: "active"}))
	if string(data) != `{"entries":[]}` {
		t.Fatalf("empty board must serialize as an empty array, got %s", data)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func containsFold(s, sub string) bool {
	return len(sub) > 0 && indexFold(s, sub) >= 0
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 32
		}
		if y >= 'A' && y <= 'Z' {
			y += 32
		}
		if x != y {
			return false
		}
	}
	return true
}
