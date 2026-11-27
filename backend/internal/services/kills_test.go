package services_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func kill(t *testing.T, s *services.GameService, killer, victim models.Access) {
	t.Helper()
	ctx := context.Background()
	claim, err := s.ReportElimination(ctx, killer, victim.PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RespondElimination(ctx, victim, claim.ID, true); err != nil {
		t.Fatal(err)
	}
}

func totals(t *testing.T, repo interface {
	Get(context.Context, string) (*models.Game, error)
}, id string) map[string]int {
	t.Helper()
	g, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return services.KillTotals(g)
}

func TestConfirmedKillsCountOnlyAcceptedEliminations(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 5)
	id := a[0].GameID

	// Legacy games without the field read as zero.
	if got := totals(t, repo, id); got["0"] != 0 || len(got) != 5 {
		t.Fatalf("expected zero totals, got %v", got)
	}

	// Declined claims do not count.
	claim, err := s.ReportElimination(ctx, a[0], "1")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RespondElimination(ctx, a[1], claim.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := totals(t, repo, id); got["0"] != 0 {
		t.Fatalf("declined claim counted: %v", got)
	}

	// An accepted claim counts once; replaying the response is rejected.
	claim, err = s.ReportElimination(ctx, a[0], "1")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RespondElimination(ctx, a[1], claim.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = s.RespondElimination(ctx, a[1], claim.ID, true); err == nil {
		t.Fatal("duplicate confirmation accepted")
	}
	if got := totals(t, repo, id); got["0"] != 1 || got["1"] != 0 {
		t.Fatalf("expected killer 1, victim 0, got %v", got)
	}

	// Kills keep counting up and survive the killer's own elimination.
	kill(t, s, a[0], a[2])
	kill(t, s, a[4], a[0])
	got := totals(t, repo, id)
	if got["0"] != 2 || got["4"] != 1 || got["1"] != 0 || got["2"] != 0 || got["3"] != 0 {
		t.Fatalf("unexpected totals: %v", got)
	}
}

func TestConfirmedKillsNotDoubleCountedUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	claim, err := s.ReportElimination(ctx, a[0], "1")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.RespondElimination(ctx, a[1], claim.ID, true)
		}()
	}
	wg.Wait()
	if got := totals(t, repo, a[0].GameID); got["0"] != 1 {
		t.Fatalf("expected exactly one kill, got %v", got)
	}
}

func TestTeamKillTotals(t *testing.T) {
	s, repo, a := eliminationGame(t, 4)
	ctx := context.Background()
	g, _ := repo.Get(ctx, a[0].GameID)
	g.Players[0].TeamID, g.Players[1].TeamID, g.Players[2].TeamID, g.Players[3].TeamID = "x", "y", "x", "y"
	g.Players[0].ConfirmedKills, g.Players[2].ConfirmedKills, g.Players[3].ConfirmedKills = 2, 1, 4
	prev := g.Revision
	g.Revision++
	if err := repo.Save(ctx, g, prev); err != nil {
		t.Fatal(err)
	}
	_ = s
	g, _ = repo.Get(ctx, a[0].GameID)
	if got := services.TeamKillTotals(g); got["x"] != 3 || got["y"] != 4 {
		t.Fatalf("unexpected team totals: %v", got)
	}
}

// Kill counts must be absent from every view for every role, mid-game and after
// kills have been recorded.
func TestKillCountsNeverInAnyAPIResponse(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	kill(t, s, a[0], a[1])
	g, _ := repo.Get(ctx, a[0].GameID)
	if services.KillTotals(g)["0"] != 1 {
		t.Fatal("setup: kill not recorded")
	}
	roles := append([]models.Access{{GameID: g.ID}}, a...) // organizer + every player
	for _, role := range roles {
		raw, err := json.Marshal(services.View(g, role))
		if err != nil {
			t.Fatal(err)
		}
		// me.elimination.confirmedAt is the victim's own receipt timestamp, not a kill count.
		lower := strings.ReplaceAll(strings.ToLower(string(raw)), `"confirmedat"`, "")
		for _, banned := range []string{"kills", "confirmed", "score"} {
			if strings.Contains(lower, banned) {
				t.Fatalf("role %q view contains %q: %s", role.PlayerID, banned, raw)
			}
		}
	}
}
