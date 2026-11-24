package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

// teamGame turns an eliminationGame (a ring where player i targets i+1) into a
// team game. teams[i] is player i's team ID; teams get readable names.
func teamGame(t *testing.T, teams []string) (*services.GameService, models.Access, func() *models.Game, []models.Access) {
	t.Helper()
	ctx := context.Background()
	s, repo, a := eliminationGame(t, len(teams))
	g, _ := repo.Get(ctx, a[0].GameID)
	g.Mode = models.ModeTeam
	seen := map[string]bool{}
	for i, team := range teams {
		g.Players[i].TeamID = team
		if !seen[team] {
			seen[team] = true
			g.Teams = append(g.Teams, models.Team{ID: team, Name: "Team " + team})
		}
	}
	repo.Games[g.ID] = g
	return s, models.Access{GameID: g.ID}, func() *models.Game { g, _ := repo.Get(ctx, a[0].GameID); return g }, a
}

// confirmKill has the killer report their current victim and the victim accept.
func confirmKill(t *testing.T, s *services.GameService, killer, victim models.Access, victimID string) {
	t.Helper()
	ctx := context.Background()
	claim, err := s.ReportElimination(ctx, killer, victimID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RespondElimination(ctx, victim, claim.ID, true); err != nil {
		t.Fatal(err)
	}
}

func rawView(t *testing.T, g *models.Game, a models.Access) map[string]any {
	t.Helper()
	data, err := json.Marshal(services.View(g, a))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestIndividualLastPlayerWinsAndWinnerIsHiddenUntilTheEnd(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 3) // 0 -> 1 -> 2 -> 0
	organizer := models.Access{GameID: a[0].GameID}
	confirmKill(t, s, a[0], a[1], "1")
	g, _ := repo.Get(ctx, a[0].GameID)
	// Mid-game: no winner and no end reason for anybody.
	for _, actor := range append(a, organizer) {
		raw := rawView(t, g, actor)
		if v, present := raw["winner"]; !present || v != nil {
			t.Fatalf("winner must be null before the end: %v (present=%v)", v, present)
		}
		if raw["endReason"] != nil || raw["winnerId"] != nil {
			t.Fatalf("end state leaked mid-game: %v %v", raw["endReason"], raw["winnerId"])
		}
	}
	confirmKill(t, s, a[0], a[2], "2")
	g, _ = repo.Get(ctx, a[0].GameID)
	if g.Status != "ended" || g.EndedAt == nil || g.EndReason != "survival" || g.WinnerKind != "player" || g.WinnerID != "0" {
		t.Fatalf("unexpected end state: %+v", g)
	}
	for _, actor := range append(a, organizer) {
		view := services.View(g, actor)
		if view.Winner == nil || *view.Winner != (models.WinnerView{Kind: "player", ID: "0", Name: "Player 0"}) {
			t.Fatalf("winner view = %+v", view.Winner)
		}
		if view.WinnerID != "0" || view.EndReason == nil || *view.EndReason != "survival" {
			t.Fatalf("winnerId=%q endReason=%v", view.WinnerID, view.EndReason)
		}
	}
}

func TestTeamMajorityWinsImmediatelyAfterAnElimination(t *testing.T) {
	ctx := context.Background()
	s, organizer, load, a := teamGame(t, []string{"A", "B", "A", "B"}) // ring is team-safe
	// Player 0 (A) kills player 1 (B): A holds 2 of the 3 left, so no safe circle
	// can exist and team A has won, even though a B player is still alive.
	confirmKill(t, s, a[0], a[1], "1")
	g := load()
	if g.Status != "ended" || g.EndedAt == nil || g.EndReason != "survival" || g.WinnerKind != "team" || g.WinnerID != "A" {
		t.Fatalf("team A should win: %+v", g)
	}
	for _, p := range g.Players {
		if p.VictimID != "" || p.Mission != "" {
			t.Fatalf("assignments must be cleared when the game ends: %+v", p)
		}
	}
	for _, actor := range append(a, organizer) {
		view := services.View(g, actor)
		if view.Winner == nil || *view.Winner != (models.WinnerView{Kind: "team", ID: "A", Name: "Team A"}) {
			t.Fatalf("winner view = %+v", view.Winner)
		}
		if view.WinnerID != "" {
			t.Fatalf("winnerId is for player winners only, got %q", view.WinnerID)
		}
		// The payload carries exactly kind, id and name: no members, counts or kills.
		winner := rawView(t, g, actor)["winner"].(map[string]any)
		if len(winner) != 3 || winner["kind"] != "team" || winner["id"] != "A" || winner["name"] != "Team A" {
			t.Fatalf("winner payload = %v", winner)
		}
	}
	// The ended game accepts no further eliminations.
	if _, err := s.ReportElimination(ctx, a[2], "3"); !errors.Is(err, services.ErrElimination) {
		t.Fatalf("elimination after the end: %v", err)
	}
}

func TestTeamWinnerFromAWithdrawal(t *testing.T) {
	s, _, load, a := teamGame(t, []string{"A", "B", "A", "B"})
	withdraw(t, s, a[1]) // B player leaves: A holds 2 of 3
	g := load()
	if g.Status != "ended" || g.WinnerKind != "team" || g.WinnerID != "A" || g.EndReason != "survival" {
		t.Fatalf("withdrawal should end the game for team A: %+v", g)
	}
	// Nobody was credited for the withdrawal.
	for _, p := range g.Players {
		if p.ConfirmedKills != 0 {
			t.Fatal("withdrawal credited a kill")
		}
	}
}

func TestTeamGameContinuesWhileNoTeamHoldsAMajority(t *testing.T) {
	s, _, load, a := teamGame(t, []string{"A", "B", "C", "A", "B", "C"})
	confirmKill(t, s, a[0], a[1], "1") // living A,C,A,B,C: nobody above half
	g := load()
	if g.Status != "active" || g.WinnerID != "" || g.EndReason != "" {
		t.Fatalf("game ended too early: %+v", g)
	}
	assertCircle(t, g)
	view := services.View(g, a[0])
	if view.Winner != nil || view.EndReason != nil {
		t.Fatalf("winner exposed mid-game: %+v %v", view.Winner, view.EndReason)
	}
}

func TestTeamLastPlayerStandingStillWins(t *testing.T) {
	// A single survivor wins as a player even in team mode.
	s, _, load, a := teamGame(t, []string{"A", "B"})
	withdraw(t, s, a[1])
	g := load()
	if g.Status != "ended" || g.WinnerKind != "player" || g.WinnerID != "0" {
		t.Fatalf("last player should win: %+v", g)
	}
}

func TestConcurrentFinalEventsEndTheGameOnce(t *testing.T) {
	ctx := context.Background()
	for round := 0; round < 25; round++ {
		// Players 1 and 3 (both team B) withdraw at once. Whichever lands first
		// leaves A with 2 of 3 and ends the game; the other is rejected.
		s, _, load, a := teamGame(t, []string{"A", "B", "A", "B"})
		tokens := map[int]string{}
		for _, i := range []int{1, 3} {
			request, err := s.RequestWithdrawal(ctx, a[i])
			if err != nil {
				t.Fatal(err)
			}
			tokens[i] = request.Token
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		errs := map[int]error{}
		for _, i := range []int{1, 3} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := s.ConfirmWithdrawal(ctx, a[i], tokens[i])
				mu.Lock()
				errs[i] = err
				mu.Unlock()
			}()
		}
		wg.Wait()
		if (errs[1] == nil) == (errs[3] == nil) {
			t.Fatalf("round %d: exactly one withdrawal should win the race: %v / %v", round, errs[1], errs[3])
		}
		for _, err := range errs {
			if err != nil && !errors.Is(err, services.ErrWithdrawal) {
				t.Fatalf("unexpected error: %v", err)
			}
		}
		g := load()
		withdrawn := 0
		for _, p := range g.Players {
			if p.WithdrawnAt != nil {
				withdrawn++
			}
		}
		if g.Status != "ended" || g.WinnerKind != "team" || g.WinnerID != "A" || withdrawn != 1 || g.EndedAt == nil {
			t.Fatalf("round %d: %s", round, fmt.Sprintf("status=%s winner=%s/%s withdrawn=%d", g.Status, g.WinnerKind, g.WinnerID, withdrawn))
		}
	}
}
