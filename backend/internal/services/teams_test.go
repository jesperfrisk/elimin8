package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func TestRandomTeamsBalancedAndDeterministic(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e", "f", "g"}
	zero := func(int) int { return 0 }
	first := services.RandomTeams(ids, 3, zero)
	second := services.RandomTeams(ids, 3, zero)
	if len(first) != 3 || strings.Join(flat(first), ",") != strings.Join(flat(second), ",") {
		t.Fatalf("not deterministic: %v vs %v", first, second)
	}
	seen := map[string]bool{}
	for _, team := range first {
		if len(team) < 2 || len(team) > 3 {
			t.Fatalf("unbalanced team sizes: %v", first)
		}
		for _, id := range team {
			if seen[id] {
				t.Fatalf("duplicate %s", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != len(ids) {
		t.Fatalf("not everyone assigned: %v", first)
	}
}

func flat(teams [][]string) []string {
	var out []string
	for _, t := range teams {
		out = append(out, t...)
	}
	return out
}

func TestTeamsManualRandomAndStartValidation(t *testing.T) {
	s, _, game, owner, players := setup(t)
	ctx := context.Background()
	more, err := s.AddPlayers(ctx, owner, []string{"Cara", "Dan"})
	if err != nil {
		t.Fatal(err)
	}
	players = append(players, more...)
	joinPlayers(t, s, game, players)
	if _, err := s.AddMissions(ctx, owner, []string{"Hide a spoon"}); err != nil {
		t.Fatal(err)
	}

	// Teams require team mode.
	if _, err := s.SetTeams(ctx, owner, nil); !errors.Is(err, services.ErrInvalid) {
		t.Fatalf("expected invalid in individual mode, got %v", err)
	}
	if err := s.SetMode(ctx, owner, "bogus"); !errors.Is(err, services.ErrInvalid) {
		t.Fatalf("expected invalid mode, got %v", err)
	}
	if err := s.SetMode(ctx, owner, models.ModeTeam); err != nil {
		t.Fatal(err)
	}

	// Start rejected: nobody on a team.
	if err := s.Transition(ctx, owner, "start"); !errors.Is(err, services.ErrTeamSetup) {
		t.Fatalf("expected team setup error, got %v", err)
	}

	// Manual: only one team populated, and a player left unassigned.
	ids := []string{players[0].ID, players[1].ID, players[2].ID, players[3].ID}
	teams, err := s.SetTeams(ctx, owner, []services.TeamInput{
		{Name: "Red", PlayerIDs: ids[:3]},
		{Name: "Blue"},
	})
	if err != nil || len(teams) != 2 || len(teams[0].PlayerIDs) != 3 {
		t.Fatalf("manual assign failed: %v %+v", err, teams)
	}
	if err := s.Transition(ctx, owner, "start"); !errors.Is(err, services.ErrTeamSetup) {
		t.Fatalf("expected team setup error with unassigned player, got %v", err)
	}

	// Invalid input: unknown player, duplicate assignment, blank name.
	for _, bad := range [][]services.TeamInput{
		{{Name: "X", PlayerIDs: []string{"nope"}}},
		{{Name: "X", PlayerIDs: []string{ids[0]}}, {Name: "Y", PlayerIDs: []string{ids[0]}}},
		{{Name: " ", PlayerIDs: nil}},
	} {
		if bad[0].Name == " " {
			bad[0].Name = ""
		}
		if _, err := s.SetTeams(ctx, owner, bad); !errors.Is(err, services.ErrInvalid) {
			t.Fatalf("expected invalid for %+v, got %v", bad, err)
		}
	}

	// Players cannot configure teams.
	_, g, _ := s.Authorize(ctx, game.Token)
	playerAccess := models.Access{GameID: g.ID, PlayerID: players[0].ID}
	if err := s.SetMode(ctx, playerAccess, models.ModeIndividual); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}

	// Random: injected randomness, persisted, everyone joined is assigned.
	s.Intn = func(int) int { return 0 }
	random, err := s.RandomizeTeams(ctx, owner, 2)
	if err != nil || len(random) != 2 || len(random[0].PlayerIDs) != 2 || len(random[1].PlayerIDs) != 2 {
		t.Fatalf("random failed: %v %+v", err, random)
	}
	if _, err := s.RandomizeTeams(ctx, owner, 5); !errors.Is(err, services.ErrInvalid) {
		t.Fatalf("expected invalid team count, got %v", err)
	}
	if err := s.Transition(ctx, owner, "start"); err != nil {
		t.Fatalf("start with valid teams: %v", err)
	}

	// No changes after start.
	if _, err := s.SetTeams(ctx, owner, nil); !errors.Is(err, services.ErrAlreadyStarted) {
		t.Fatalf("expected already started, got %v", err)
	}
	if err := s.SetMode(ctx, owner, models.ModeIndividual); !errors.Is(err, services.ErrAlreadyStarted) {
		t.Fatalf("expected already started, got %v", err)
	}
}

func TestTeamPrivacy(t *testing.T) {
	s, _, game, owner, players := setup(t)
	ctx := context.Background()
	more, _ := s.AddPlayers(ctx, owner, []string{"Cara", "Dan"})
	players = append(players, more...)
	joinPlayers(t, s, game, players)
	if _, err := s.AddMissions(ctx, owner, []string{"Hide a spoon"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMode(ctx, owner, models.ModeTeam); err != nil {
		t.Fatal(err)
	}
	_, err := s.SetTeams(ctx, owner, []services.TeamInput{
		{Name: "Crimson", PlayerIDs: []string{players[0].ID, players[1].ID}},
		{Name: "Azure", PlayerIDs: []string{players[2].ID, players[3].ID}},
	})
	if err != nil {
		t.Fatal(err)
	}

	check := func(label string) {
		t.Helper()
		_, g, _ := s.Authorize(ctx, game.Token)
		orgView := services.View(g, models.Access{GameID: g.ID})
		if orgView.Mode != "team" || len(orgView.Teams) != 2 {
			t.Fatalf("%s: organizer should see mode and teams: %+v", label, orgView)
		}
		for _, p := range players {
			view := services.View(g, models.Access{GameID: g.ID, PlayerID: p.ID})
			if view.Mode != "" || view.Teams != nil {
				t.Fatalf("%s: player view leaks setup", label)
			}
			if view.Me == nil || view.Me.Team == nil || len(view.Me.Team.Members) != 2 {
				t.Fatalf("%s: player %s should see own team: %+v", label, p.Name, view.Me)
			}
			own := view.Me.Team.Name
			other := map[string]string{"Crimson": "Azure", "Azure": "Crimson"}[own]
			raw, _ := json.Marshal(view)
			if strings.Contains(string(raw), other) {
				t.Fatalf("%s: player %s sees other team %s: %s", label, p.Name, other, raw)
			}
			for _, m := range view.Participants {
				raw, _ := json.Marshal(m)
				if strings.Contains(string(raw), "team") {
					t.Fatalf("%s: participant entry has team data: %s", label, raw)
				}
			}
		}
	}
	check("lobby")
	if err := s.Transition(ctx, owner, "start"); err != nil {
		t.Fatal(err)
	}
	check("active")
}

func TestIndividualModeUnchanged(t *testing.T) {
	s, _, game, owner, players := setup(t)
	ctx := context.Background()
	joinPlayers(t, s, game, players)
	if _, err := s.AddMissions(ctx, owner, []string{"Hide a spoon"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Transition(ctx, owner, "start"); err != nil {
		t.Fatalf("individual start should not need teams: %v", err)
	}
	_, g, _ := s.Authorize(ctx, game.Token)
	view := services.View(g, models.Access{GameID: g.ID, PlayerID: players[0].ID})
	if view.Me.Team != nil {
		t.Fatal("individual game exposed a team")
	}
}
