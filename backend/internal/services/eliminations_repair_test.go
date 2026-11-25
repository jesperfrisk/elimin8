package services_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

// staleReportGame is the #49 fixture: circle 0(A) -> 1(B) -> 2(A) -> 3(C) -> 4(B)
// -> 5(C) -> 0. Removing player 1 leaves 0(A) -> 2(A), so team repair must
// rearrange the living circle.
func staleReportGame(t *testing.T) (*services.GameService, *testutil.Memory, []models.Access) {
	t.Helper()
	now := time.Now().UTC()
	teams := []string{"A", "B", "A", "C", "B", "C"}
	g := &models.Game{ID: "stale-report-game", Mode: models.ModeTeam, Status: "active", Revision: 1, StartedAt: &now}
	for _, team := range []string{"A", "B", "C"} {
		g.Teams = append(g.Teams, models.Team{ID: team, Name: "Team " + team})
	}
	var access []models.Access
	for i, team := range teams {
		id := fmt.Sprint(i)
		g.Players = append(g.Players, models.Player{ID: id, Name: "Player " + id, TeamID: team, Included: true, JoinedAt: &now, Mission: "Mission " + id, VictimID: fmt.Sprint((i + 1) % len(teams))})
		access = append(access, models.Access{GameID: g.ID, PlayerID: id})
	}
	repo := testutil.NewMemory()
	if err := repo.Insert(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	return services.NewGameService(repo), repo, access
}

// assertNoStalePending fails when a pending report no longer matches its living
// killer's current victim, and returns how many pending reports remain.
func assertNoStalePending(t *testing.T, g *models.Game) int {
	t.Helper()
	byID := map[string]models.Player{}
	for _, p := range g.Players {
		byID[p.ID] = p
	}
	pending := 0
	for _, claim := range g.Eliminations {
		if claim.Status != "pending" {
			continue
		}
		pending++
		killer := byID[claim.KillerID]
		if killer.EliminatedAt != nil || killer.WithdrawnAt != nil || killer.VictimID != claim.VictimID {
			t.Fatalf("stale pending report %s: %s -> %s but killer now targets %q", claim.ID, claim.KillerID, claim.VictimID, killer.VictimID)
		}
	}
	return pending
}

func claimStatus(g *models.Game, id string) string {
	for _, c := range g.Eliminations {
		if c.ID == id {
			return c.Status
		}
	}
	return ""
}

func TestRepairCancelsReportsWhoseTargetChanged(t *testing.T) {
	ctx := context.Background()
	for _, path := range []string{"elimination", "withdrawal"} {
		t.Run(path, func(t *testing.T) {
			s, repo, a := staleReportGame(t)
			stale, err := s.ReportElimination(ctx, a[3], "4")
			if err != nil {
				t.Fatal(err)
			}
			if path == "elimination" {
				first, err := s.ReportElimination(ctx, a[0], "1")
				if err != nil {
					t.Fatal(err)
				}
				if err = s.RespondElimination(ctx, a[1], first.ID, true); err != nil {
					t.Fatal(err)
				}
			} else {
				request, err := s.RequestWithdrawal(ctx, a[1])
				if err != nil {
					t.Fatal(err)
				}
				if err = s.ConfirmWithdrawal(ctx, a[1], request.Token); err != nil {
					t.Fatal(err)
				}
			}
			g, _ := repo.Get(ctx, a[0].GameID)
			assertNoStalePending(t, g)
			for _, p := range g.Players {
				if p.EliminatedAt != nil || p.WithdrawnAt != nil {
					continue
				}
				for _, q := range g.Players {
					if q.ID == p.VictimID && q.TeamID == p.TeamID {
						t.Fatalf("%s targets teammate %s", p.ID, q.ID)
					}
				}
			}
			moved := g.Players[3].VictimID != "4"
			if moved && claimStatus(g, stale.ID) != "cancelled" {
				t.Fatalf("report 3 -> 4 is %q after 3's target changed", claimStatus(g, stale.ID))
			}
			if !moved && claimStatus(g, stale.ID) != "pending" {
				t.Fatalf("unchanged report 3 -> 4 is %q, want pending", claimStatus(g, stale.ID))
			}
			if moved {
				if err = s.RespondElimination(ctx, models.Access{GameID: g.ID, PlayerID: "4"}, stale.ID, true); !errors.Is(err, services.ErrElimination) {
					t.Fatalf("cancelled report accepted: %v", err)
				}
				for _, p := range g.Players {
					if p.ID == "3" && p.ConfirmedKills != 0 {
						t.Fatal("cancelled report earned a kill")
					}
				}
				// The hunter can report the replacement target at once.
				if _, err = s.ReportElimination(ctx, a[3], g.Players[3].VictimID); err != nil {
					t.Fatalf("replacement target blocked: %v", err)
				}
			}
		})
	}
}

func TestRepairKeepsUnchangedReportsPending(t *testing.T) {
	ctx := context.Background()
	s, repo, a := staleReportGame(t)
	// 2 -> 3 is not touched by the repair after 1 leaves unless 2's target moves;
	// record every report and verify only moved targets are cancelled.
	reports := map[string]string{}
	for _, k := range []int{2, 3, 4} {
		claim, err := s.ReportElimination(ctx, a[k], fmt.Sprint((k+1)%6))
		if err != nil {
			t.Fatal(err)
		}
		reports[claim.ID] = fmt.Sprint(k)
	}
	first, err := s.ReportElimination(ctx, a[0], "1")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RespondElimination(ctx, a[1], first.ID, true); err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, a[0].GameID)
	assertNoStalePending(t, g)
	for id, killer := range reports {
		var p models.Player
		for _, q := range g.Players {
			if q.ID == killer {
				p = q
			}
		}
		for _, c := range g.Eliminations {
			if c.ID != id {
				continue
			}
			want := "pending"
			if p.VictimID != c.VictimID {
				want = "cancelled"
			}
			if c.Status != want {
				t.Fatalf("report by %s is %q, want %q", killer, c.Status, want)
			}
		}
	}
}
