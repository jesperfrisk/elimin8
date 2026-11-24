package services

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

func seedVictims(t *testing.T, seed int64) {
	t.Helper()
	r := rand.New(rand.NewSource(seed))
	old := victimIntn
	victimIntn = r.Intn
	t.Cleanup(func() { victimIntn = old })
}

func teamGame(sizes []int) *models.Game {
	now := time.Now()
	g := &models.Game{Mode: models.ModeTeam, Status: "lobby"}
	for ti, size := range sizes {
		g.Teams = append(g.Teams, models.Team{ID: fmt.Sprintf("t%d", ti)})
		for i := 0; i < size; i++ {
			g.Players = append(g.Players, models.Player{ID: fmt.Sprintf("t%d-p%d", ti, i), TeamID: fmt.Sprintf("t%d", ti), JoinedAt: &now})
		}
	}
	return g
}

func startActive(g *models.Game) {
	now := time.Now()
	g.Status, g.StartedAt = "active", &now
	for i := range g.Players {
		g.Players[i].Included = g.Players[i].JoinedAt != nil
	}
}

// assertTeamCircle checks one circle over the living players with no
// self-targets, no teammate victims and one hunter each.
func assertTeamCircle(t *testing.T, g *models.Game) {
	t.Helper()
	living := map[string]models.Player{}
	for _, p := range g.Players {
		if p.JoinedAt != nil && p.EliminatedAt == nil {
			living[p.ID] = p
		}
	}
	hunters := map[string]int{}
	for _, p := range living {
		v, ok := living[p.VictimID]
		if !ok || v.ID == p.ID {
			t.Fatalf("bad victim for %s: %q", p.ID, p.VictimID)
		}
		if v.TeamID == p.TeamID {
			t.Fatalf("%s targets teammate %s", p.ID, v.ID)
		}
		hunters[v.ID]++
	}
	for id := range living {
		if hunters[id] != 1 {
			t.Fatalf("%s has %d hunters", id, hunters[id])
		}
	}
	if len(livingCircle(g)) != len(living) {
		t.Fatal("victims do not form a single circle")
	}
}

func TestTeamSafeAssignmentShapes(t *testing.T) {
	shapes := [][]int{{2, 2}, {1, 1}, {3, 3}, {4, 4}, {2, 2, 1}, {5, 5, 1}, {4, 3, 2}, {2, 2, 2, 2}, {1, 1, 1, 1, 1, 1, 1}, {6, 1, 1, 1, 1, 1, 1}, {3, 3, 3}}
	for _, shape := range shapes {
		for seed := int64(0); seed < 50; seed++ {
			seedVictims(t, seed)
			g := teamGame(shape)
			if err := assignVictims(g); err != nil {
				t.Fatalf("%v seed %d: %v", shape, seed, err)
			}
			assertTeamCircle(t, g)
		}
	}
}

func TestTeamAssignmentImpossible(t *testing.T) {
	for _, shape := range [][]int{{3, 1}, {2, 1}, {5, 2}} {
		g := teamGame(shape)
		if ValidAssignmentExists(g) {
			t.Fatalf("%v reported valid", shape)
		}
		if err := assignVictims(g); !errors.Is(err, ErrNoValidAssignment) {
			t.Fatalf("%v: got %v", shape, err)
		}
		for _, p := range g.Players {
			if p.VictimID != "" {
				t.Fatal("state written despite error")
			}
		}
	}
}

func TestValidAssignmentExists(t *testing.T) {
	if !ValidAssignmentExists(teamGame([]int{2, 2})) || !ValidAssignmentExists(teamGame([]int{1, 1})) {
		t.Fatal("expected valid")
	}
	if ValidAssignmentExists(teamGame([]int{1})) {
		t.Fatal("single player cannot form a circle")
	}
	if !ValidAssignmentExists(&models.Game{Players: teamGame([]int{3}).Players}) {
		t.Fatal("individual mode ignores teams")
	}
}

func TestTransferKeepsTeamInvariantRandomShapes(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	old := victimIntn
	victimIntn = r.Intn
	t.Cleanup(func() { victimIntn = old })
	for round := 0; round < 300; round++ {
		sizes := make([]int, 2+r.Intn(4))
		for i := range sizes {
			sizes[i] = 1 + r.Intn(4)
		}
		g := teamGame(sizes)
		if !ValidAssignmentExists(g) {
			continue
		}
		if err := assignVictims(g); err != nil {
			t.Fatal(err)
		}
		startActive(g)
		for {
			living := livingCircle(g)
			if len(living) <= 2 || !ValidAssignmentExists(g) {
				break
			}
			killer := &g.Players[living[r.Intn(len(living))]]
			victim := livingPlayer(g, killer.VictimID)
			transferElimination(g, killer, victim, time.Now())
			if g.Status != "active" || !ValidAssignmentExists(g) {
				break
			}
			assertTeamCircle(t, g)
		}
	}
}

func TestTransferForcedRepair(t *testing.T) {
	now := time.Now()
	mk := func(id, team, victim string) models.Player {
		return models.Player{ID: id, TeamID: team, VictimID: victim, JoinedAt: &now}
	}
	// a1 -> b1 -> a2 -> c1 -> a3 -> d1 -> a1 ... killing b1 by a1 hands a1 a2 (teammate).
	g := &models.Game{Mode: models.ModeTeam, Players: []models.Player{
		mk("a1", "A", "b1"), mk("b1", "B", "a2"), mk("a2", "A", "c1"), mk("c1", "C", "d1"), mk("d1", "D", "e1"), mk("e1", "E", "a1"),
	}}
	startActive(g)
	seedVictims(t, 3)
	transferElimination(g, &g.Players[0], &g.Players[1], now)
	assertTeamCircle(t, g)
}

func TestTransferNoValidAssignmentLeavesCircle(t *testing.T) {
	now := time.Now()
	mk := func(id, team, victim string) models.Player {
		return models.Player{ID: id, TeamID: team, VictimID: victim, JoinedAt: &now}
	}
	// A,A,B,C: b1 killed by a1 leaves A,A,C: no safe circle.
	g := &models.Game{Mode: models.ModeTeam, Players: []models.Player{
		mk("a1", "A", "b1"), mk("b1", "B", "a2"), mk("a2", "A", "c1"), mk("c1", "C", "a1"),
	}}
	startActive(g)
	transferElimination(g, &g.Players[0], &g.Players[1], now)
	if ValidAssignmentExists(g) {
		t.Fatal("expected no valid assignment")
	}
	if err := repairTeamCircle(g); !errors.Is(err, ErrNoValidAssignment) {
		t.Fatalf("got %v", err)
	}
	if len(livingCircle(g)) != 3 || g.Status != "active" {
		t.Fatal("state corrupted")
	}
}
