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
	s, repo, a := teamGameRepo(t, teams)
	load := func() *models.Game {
		g, _ := repo.Get(context.Background(), a[0].GameID)
		return g
	}
	return s, models.Access{GameID: a[0].GameID}, load, a
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

// assertSingletonTeamWin checks a team-mode game that ended with one player
// left: the surviving player's team wins, consistently in the persisted state,
// every role's view and the final result.
func assertSingletonTeamWin(t *testing.T, g *models.Game, organizer models.Access, a []models.Access, teamID, teamName string) {
	t.Helper()
	if g.Status != "ended" || g.EndedAt == nil || g.EndReason != "survival" || g.WinnerKind != "team" || g.WinnerID != teamID {
		t.Fatalf("team %s should win: status=%s kind=%q id=%q reason=%q", teamID, g.Status, g.WinnerKind, g.WinnerID, g.EndReason)
	}
	for _, p := range g.Players {
		if p.VictimID != "" || p.Mission != "" {
			t.Fatalf("assignments must be cleared when the game ends: %+v", p)
		}
	}
	for _, actor := range append(append([]models.Access{}, a...), organizer) {
		view := services.View(g, actor)
		if view.Winner == nil || *view.Winner != (models.WinnerView{Kind: "team", ID: teamID, Name: teamName}) {
			t.Fatalf("winner view = %+v", view.Winner)
		}
		if view.WinnerID != "" {
			t.Fatalf("winnerId is for player winners only, got %q", view.WinnerID)
		}
	}
	result, err := services.ResultOf(g)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "survival" || result.Tie || len(result.Winners) != 1 || result.Winners[0] != (models.ResultWinner{Kind: "team", ID: teamID, Name: teamName}) {
		t.Fatalf("result winners = %+v", result)
	}
	for _, st := range result.Standings {
		if st.Kind != "team" {
			t.Fatalf("standings must use the same kind as the winner: %+v", result.Standings)
		}
	}
}

func TestTeamLastPlayerStandingWinsForTheirTeamByWithdrawal(t *testing.T) {
	// Two one-player teams: a confirmed withdrawal leaves one survivor, and the
	// winner is their team, not the player.
	s, organizer, load, a := teamGame(t, []string{"A", "B"})
	withdraw(t, s, a[1])
	assertSingletonTeamWin(t, load(), organizer, a, "A", "Team A")
}

func TestTeamLastPlayerStandingWinsForTheirTeamByElimination(t *testing.T) {
	s, organizer, load, a := teamGame(t, []string{"A", "B"})
	confirmKill(t, s, a[0], a[1], "1")
	assertSingletonTeamWin(t, load(), organizer, a, "A", "Team A")
}

func TestTeamSingletonTeamsPlayOnUntilOneTeamIsLeft(t *testing.T) {
	// Three one-player teams: two survivors on different teams is still a valid
	// circle, so the game continues until a single team remains.
	s, organizer, load, a := teamGame(t, []string{"A", "B", "C"}) // 0 -> 1 -> 2 -> 0
	confirmKill(t, s, a[0], a[1], "1")
	g := load()
	if g.Status != "active" || g.WinnerID != "" || g.WinnerKind != "" {
		t.Fatalf("game ended with two teams left: %+v", g)
	}
	assertCircle(t, g)
	confirmKill(t, s, a[0], a[2], "2")
	assertSingletonTeamWin(t, load(), organizer, a, "A", "Team A")
}

func TestTeamLastPlayerWinnerIsNotLeakedBeforeTheEnd(t *testing.T) {
	s, organizer, load, a := teamGame(t, []string{"A", "B", "C"})
	confirmKill(t, s, a[0], a[1], "1")
	g := load()
	for _, actor := range append(append([]models.Access{}, a...), organizer) {
		raw := rawView(t, g, actor)
		if v, present := raw["winner"]; !present || v != nil {
			t.Fatalf("winner must be null before the end: %v", v)
		}
		if raw["endReason"] != nil || raw["winnerId"] != nil {
			t.Fatalf("end state leaked mid-game: %v %v", raw["endReason"], raw["winnerId"])
		}
	}
	// Players never see another team, and organizers-only data stays organizer-only.
	for i := range a {
		if rawView(t, g, a[i])["teams"] != nil {
			t.Fatalf("player %d received team setup", i)
		}
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
