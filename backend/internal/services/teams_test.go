package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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

// memberNames returns the names in a player's own-team view, sorted by the
// order the service lists them.
func memberNames(v models.GameView) []string {
	if v.Me == nil || v.Me.Team == nil {
		return nil
	}
	names := []string{}
	for _, m := range v.Me.Team.Members {
		names = append(names, m.Name)
	}
	return names
}

func TestStartedTeamViewListsOnlyIncludedTeammates(t *testing.T) {
	s, repo, game, owner, players := setup(t) // Alice(0), Bob(1)
	ctx := context.Background()
	more, err := s.AddPlayers(ctx, owner, []string{"Cara", "Dan", "Eve"}) // 2, 3, 4
	if err != nil {
		t.Fatal(err)
	}
	players = append(players, more...)
	// Everyone except Cara (player 2) joins. Cara is still assigned to team A.
	var joined []models.IssuedPlayer
	for i, p := range players {
		if i != 2 {
			joined = append(joined, p)
		}
	}
	joinPlayers(t, s, game, joined)
	if _, err = s.AddMissions(ctx, owner, []string{"Hide a spoon"}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetMode(ctx, owner, models.ModeTeam); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetTeams(ctx, owner, []services.TeamInput{
		{Name: "Crimson", PlayerIDs: []string{players[0].ID, players[2].ID, players[4].ID}}, // Alice, Cara, Eve
		{Name: "Azure", PlayerIDs: []string{players[1].ID, players[3].ID}},                  // Bob, Dan
	}); err != nil {
		t.Fatal(err)
	}
	access := func(i int) models.Access { return models.Access{GameID: game.Game.ID, PlayerID: players[i].ID} }
	load := func() *models.Game {
		g, err := repo.Get(ctx, game.Game.ID)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}

	// Lobby behaviour is preserved: setup lists everyone assigned to the team,
	// and the organizer sees the full configuration.
	if got := memberNames(services.View(load(), access(0))); len(got) != 3 {
		t.Fatalf("lobby team members = %v, want all 3 assigned", got)
	}

	if err = s.Transition(ctx, owner, "start"); err != nil {
		t.Fatalf("a valid four-player game should start: %v", err)
	}
	g := load()
	if got := services.View(g, access(0)).ParticipantCount; got != 4 {
		t.Fatalf("participants = %d, want 4", got)
	}

	assertStarted := func(label string, v models.GameView) {
		t.Helper()
		got := memberNames(v)
		if len(got) != 2 || got[0] != "Alice" || got[1] != "Eve" {
			t.Fatalf("%s: team members = %v, want [Alice Eve]", label, got)
		}
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), "Cara") {
			t.Fatalf("%s: excluded invitee Cara appears in the player response: %s", label, raw)
		}
		teamJSON, _ := json.Marshal(v.Me.Team)
		for _, other := range []string{"Azure", "Bob", "Dan"} {
			if strings.Contains(string(teamJSON), other) {
				t.Fatalf("%s: opposing team data %q in own-team view: %s", label, other, teamJSON)
			}
		}
		for _, secret := range []string{"mission", "victim", "codeHash", "Code"} {
			if strings.Contains(string(teamJSON), secret) {
				t.Fatalf("%s: own-team view carries %q: %s", label, secret, teamJSON)
			}
		}
	}
	assertStarted("active", services.View(g, access(0)))

	// Reconnect: a returning player's admission carries the same projection.
	back, err := s.Login(ctx, game.Game.InvitationCode, players[0].Code, "player")
	if err != nil {
		t.Fatal(err)
	}
	assertStarted("reconnect admission", back.Game)
	a, fresh, err := s.Authorize(ctx, back.Token)
	if err != nil {
		t.Fatal(err)
	}
	assertStarted("reconnect fetch", services.View(fresh, a))

	// The organizer keeps the full configuration, including the unjoined invitee.
	org := services.View(g, models.Access{GameID: g.ID})
	var crimson int
	for _, team := range org.Teams {
		if team.Name == "Crimson" {
			crimson = len(team.PlayerIDs)
		}
	}
	if crimson != 3 {
		t.Fatalf("organizer lost configuration: Crimson has %d players, want 3", crimson)
	}

	// Included teammates stay listed after elimination or withdrawal, and an
	// ended game keeps the same list. This filters on inclusion, not on life.
	now := time.Now().UTC()
	g.Players[4].EliminatedAt = &now // Eve is eliminated
	assertStarted("teammate eliminated", services.View(g, access(0)))
	g.Players[0].WithdrawnAt = &now // Alice withdraws; Eve's view must still list both
	if got := memberNames(services.View(g, access(4))); len(got) != 2 {
		t.Fatalf("withdrawn teammate dropped from the list: %v", got)
	}
	g.Status, g.EndedAt = "ended", &now
	if got := memberNames(services.View(g, access(4))); len(got) != 2 || strings.Contains(strings.Join(got, ","), "Cara") {
		t.Fatalf("ended game team members = %v", got)
	}
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
