package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func eliminationGame(t *testing.T, count int) (*services.GameService, *testutil.Memory, []models.Access) {
	t.Helper()
	now := time.Now().UTC()
	g := &models.Game{ID: "elimination-game", Status: "active", Revision: 1, StartedAt: &now}
	var access []models.Access
	for i := 0; i < count; i++ {
		id := fmt.Sprint(i)
		g.Players = append(g.Players, models.Player{ID: id, Name: "Player " + id, Included: true, JoinedAt: &now, Mission: "Mission " + id, VictimID: fmt.Sprint((i + 1) % count)})
		access = append(access, models.Access{GameID: g.ID, PlayerID: id})
	}
	repo := testutil.NewMemory()
	if err := repo.Insert(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	return services.NewGameService(repo), repo, access
}

func TestEliminationDeclinePrivacyAndReconnect(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	notifications := 0
	s.Notify = func() { notifications++ }
	before, _ := repo.Get(ctx, a[0].GameID)
	claim, err := s.ReportElimination(ctx, a[0], a[1].PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, a[0].GameID)
	if g.Players[0].VictimID != before.Players[0].VictimID || g.Players[1].EliminatedAt != nil {
		t.Fatal("pending claim changed assignments")
	}
	for i, actor := range a {
		view := services.View(g, actor)
		if (i < 2 && len(view.Eliminations) != 1) || (i >= 2 && len(view.Eliminations) != 0) {
			t.Fatal("claim visibility incorrect")
		}
		if view.Players != nil || view.Missions != nil || len(view.Participants) != 4 {
			t.Fatal("roster privacy incorrect")
		}
		data, _ := json.Marshal(view.Participants)
		var fields []map[string]any
		_ = json.Unmarshal(data, &fields)
		for _, p := range fields {
			if len(p) != 3 || p["id"] == nil || p["name"] == nil || p["status"] != "active" {
				t.Fatalf("roster leaks assignments: %s", data)
			}
		}
	}
	if len(services.View(g, models.Access{}).Eliminations) != 1 {
		t.Fatal("organizer cannot inspect claim")
	}
	if err = s.RespondElimination(ctx, a[0], claim.ID, true); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("killer accepted own claim: %v", err)
	}
	if err = s.RespondElimination(ctx, a[2], claim.ID, false); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("unrelated player responded: %v", err)
	}
	if _, err = s.ReportElimination(ctx, a[0], a[1].PlayerID); !errors.Is(err, services.ErrPendingElimination) {
		t.Fatalf("duplicate outgoing allowed: %v", err)
	}
	if _, err = s.ReportElimination(ctx, a[2], a[1].PlayerID); !errors.Is(err, services.ErrElimination) {
		t.Fatalf("non-target report allowed: %v", err)
	}
	if err = s.RespondElimination(ctx, a[1], claim.ID, false); err != nil {
		t.Fatal(err)
	}
	g, _ = repo.Get(ctx, a[0].GameID)
	x, _ := json.Marshal(before.Players)
	y, _ := json.Marshal(g.Players)
	if string(x) != string(y) || g.Eliminations[0].Status != "declined" || notifications != 2 {
		t.Fatal("decline changed assignments or failed to notify")
	}
	s = services.NewGameService(repo)
	claim, err = s.ReportElimination(ctx, a[0], a[1].PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	s = services.NewGameService(repo)
	if err = s.RespondElimination(ctx, a[1], claim.ID, true); err != nil {
		t.Fatal(err)
	}
	g, _ = repo.Get(ctx, a[0].GameID)
	if g.Players[0].VictimID != "2" || g.Players[0].Mission != "Mission 1" || g.Players[1].EliminatedAt == nil || g.Players[1].VictimID != "" || g.Players[1].Mission != "" {
		t.Fatal("accept did not transfer assignments")
	}
	if services.View(g, a[1]).Me.Victim != nil {
		t.Fatal("eliminated player retains assignment")
	}
	revision := g.Revision
	if err = s.RespondElimination(ctx, a[1], claim.ID, true); !errors.Is(err, services.ErrElimination) {
		t.Fatalf("duplicate acceptance: %v", err)
	}
	g, _ = repo.Get(ctx, a[0].GameID)
	if g.Revision != revision {
		t.Fatal("duplicate acceptance mutated game")
	}
}

func TestEliminationOnlyAssignedTargetAndPreservesCircle(t *testing.T) {
	ctx := context.Background()
	for count := 2; count <= 7; count++ {
		for killer := 0; killer < count; killer++ {
			for victim := 0; victim < count; victim++ {
				if killer == victim {
					continue
				}
				t.Run(fmt.Sprintf("%d/%d-kills-%d", count, killer, victim), func(t *testing.T) {
					s, repo, a := eliminationGame(t, count)
					claim, err := s.ReportElimination(ctx, a[killer], a[victim].PlayerID)
					if victim != (killer+1)%count {
						if !errors.Is(err, services.ErrElimination) {
							t.Fatalf("non-target report allowed: %v", err)
						}
						g, _ := repo.Get(ctx, a[0].GameID)
						if g.Revision != 1 || len(g.Eliminations) != 0 {
							t.Fatal("rejected report changed game")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if err = s.RespondElimination(ctx, a[victim], claim.ID, true); err != nil {
						t.Fatal(err)
					}
					g, _ := repo.Get(ctx, a[0].GameID)
					if count == 2 {
						if g.Status != "ended" || g.WinnerID != a[killer].PlayerID || g.EndedAt == nil || g.Players[killer].VictimID != "" {
							t.Fatal("final elimination did not end game")
						}
						return
					}
					if g.Players[killer].Mission != fmt.Sprint("Mission ", victim) {
						t.Fatal("mission not inherited")
					}
					seen := map[string]bool{}
					id := a[killer].PlayerID
					for i := 0; i < count-1; i++ {
						if seen[id] || id == a[victim].PlayerID || id == "" {
							t.Fatalf("invalid circle: %+v", g.Players)
						}
						seen[id] = true
						for _, p := range g.Players {
							if p.ID == id {
								id = p.VictimID
								break
							}
						}
					}
					if id != a[killer].PlayerID {
						t.Fatal("circle not closed")
					}
				})
			}
		}
	}
}

func TestEliminationConcurrentResponsesAndStaleClaims(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	claim, _ := s.ReportElimination(ctx, a[0], "1")
	other, err := s.ReportElimination(ctx, a[1], "2")
	if err != nil {
		t.Fatal(err)
	}
	// Separate service instances exercise optimistic repository retries too.
	second := services.NewGameService(repo)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, service := range []*services.GameService{s, second} {
		wg.Add(1)
		go func(service *services.GameService) {
			defer wg.Done()
			errs <- service.RespondElimination(ctx, a[1], claim.ID, true)
		}(service)
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, services.ErrElimination) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d times", successes)
	}
	if err = s.RespondElimination(ctx, a[2], other.ID, true); !errors.Is(err, services.ErrElimination) {
		t.Fatalf("eliminated killer's claim remained valid: %v", err)
	}
	g, _ := repo.Get(ctx, a[0].GameID)
	if g.Eliminations[1].Status != "cancelled" {
		t.Fatal("stale claim not cancelled")
	}
	if _, err = s.ReportElimination(ctx, a[1], "2"); !errors.Is(err, services.ErrElimination) {
		t.Fatal("eliminated player reported a kill")
	}
	if _, err = s.ReportElimination(ctx, a[2], "1"); !errors.Is(err, services.ErrElimination) {
		t.Fatal("eliminated victim selected")
	}
	if _, err = s.ReportElimination(ctx, a[2], "2"); !errors.Is(err, services.ErrInvalid) {
		t.Fatal("self report allowed")
	}
	if _, err = s.ReportElimination(ctx, models.Access{GameID: a[0].GameID}, "2"); !errors.Is(err, services.ErrForbidden) {
		t.Fatal("organizer reported kill")
	}
	if _, err = s.ReportElimination(ctx, a[2], "missing"); !errors.Is(err, services.ErrElimination) {
		t.Fatal("unknown victim accepted")
	}
	claim, err = s.ReportElimination(ctx, a[2], "3")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Transition(ctx, models.Access{GameID: a[0].GameID}, "end"); err != nil {
		t.Fatal(err)
	}
	g, _ = repo.Get(ctx, a[0].GameID)
	if g.Eliminations[2].Status != "cancelled" {
		t.Fatal("ending left pending claims")
	}
	if err = s.RespondElimination(ctx, a[3], claim.ID, true); !errors.Is(err, services.ErrElimination) {
		t.Fatal("accepted after game ended")
	}
}

func TestEliminationCannotAcceptLegacyNonTargetClaim(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	g, _ := repo.Get(ctx, a[0].GameID)
	g.Eliminations = append(g.Eliminations, models.Elimination{ID: "legacy", KillerID: "0", VictimID: "2", Status: "pending", CreatedAt: time.Now().UTC()})
	g.Revision++
	if err := repo.Save(ctx, g, g.Revision-1); err != nil {
		t.Fatal(err)
	}
	if err := s.RespondElimination(ctx, a[2], "legacy", true); !errors.Is(err, services.ErrElimination) {
		t.Fatalf("accepted a non-target claim: %v", err)
	}
	after, _ := repo.Get(ctx, a[0].GameID)
	if after.Revision != g.Revision || after.Players[2].EliminatedAt != nil || after.Players[0].VictimID != "1" {
		t.Fatal("invalid acceptance changed game")
	}
	if err := s.RespondElimination(ctx, a[2], "legacy", false); err != nil {
		t.Fatalf("cannot dismiss legacy claim: %v", err)
	}
}
