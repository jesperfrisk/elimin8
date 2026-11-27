package services

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

// Enumerate every team-size partition up to 14 players,
// independently check feasibility, and play mixed elimination/withdrawal games.
func TestAuditTeammateVictims(t *testing.T) {
	r := rand.New(rand.NewSource(20261124))
	old := victimIntn
	victimIntn = r.Intn
	t.Cleanup(func() { victimIntn = old })
	starts, transfers, impossible := 0, 0, 0
	check := func(g *models.Game, context string) {
		t.Helper()
		living := map[string]*models.Player{}
		for i := range g.Players {
			p := &g.Players[i]
			if stillPlaying(p) {
				living[p.ID] = p
			}
		}
		if g.Status == "ended" {
			counts := map[string]int{}
			for _, p := range living {
				counts[p.TeamID]++
			}
			// Team mode always ends with a winning team, even when a single player
			// is left: that player's team holds all of the remaining players.
			if g.WinnerKind != winnerTeam || counts[g.WinnerID]*2 <= len(living) {
				t.Fatalf("%s: game ended without an unbeatable winning team (kind=%q id=%q)", context, g.WinnerKind, g.WinnerID)
			}
			for _, p := range living {
				if p.VictimID != "" {
					t.Fatalf("%s: ended player %s retains victim %s", context, p.ID, p.VictimID)
				}
			}
			return
		}
		hunters := map[string]int{}
		for _, p := range living {
			v := living[p.VictimID]
			if v == nil || p.ID == v.ID {
				t.Fatalf("%s: invalid victim %s -> %s", context, p.ID, p.VictimID)
			}
			if p.TeamID == v.TeamID {
				t.Fatalf("%s: TEAMMATE VICTIM %s -> %s, team %s", context, p.ID, v.ID, p.TeamID)
			}
			hunters[v.ID]++
			view := View(g, models.Access{GameID: g.ID, PlayerID: p.ID})
			if view.Me == nil || view.Me.Victim == nil || view.Me.Victim.ID != v.ID {
				t.Fatalf("%s: player view disagrees with assignment", context)
			}
		}
		var first string
		for id := range living {
			first = id
			if hunters[id] != 1 {
				t.Fatalf("%s: %s has %d hunters", context, id, hunters[id])
			}
		}
		seen := map[string]bool{}
		for id := first; !seen[id]; id = living[id].VictimID {
			seen[id] = true
		}
		if len(seen) != len(living) {
			t.Fatalf("%s: disconnected circles", context)
		}
	}
	var visit func(int, int, []int)
	visit = func(left, min int, sizes []int) {
		if left != 0 {
			for size := min; size <= left; size++ {
				visit(left-size, size, append(append([]int(nil), sizes...), size))
			}
			return
		}
		total, largest := 0, 0
		for _, size := range sizes {
			total += size
			if size > largest {
				largest = size
			}
		}
		feasible := total >= 2 && 2*largest <= total
		for seed := 0; seed < 20; seed++ {
			g := teamGame(sizes)
			r.Shuffle(len(g.Players), func(i, j int) { g.Players[i], g.Players[j] = g.Players[j], g.Players[i] })
			err := assignVictims(g)
			if !feasible {
				if err != ErrNoValidAssignment {
					t.Fatalf("%v: impossible setup accepted: %v", sizes, err)
				}
				for _, p := range g.Players {
					if p.VictimID != "" {
						t.Fatalf("%v: rejected start wrote a victim", sizes)
					}
				}
				impossible++
				continue
			}
			if err != nil {
				t.Fatalf("%v: feasible setup rejected: %v", sizes, err)
			}
			starts++
			startActive(g)
			context := fmt.Sprintf("teams=%v run=%d", sizes, seed)
			check(g, context+" at start")
			for step := 0; g.Status == "active"; step++ {
				if step >= total {
					t.Fatalf("%s: game failed to end", context)
				}
				var living []*models.Player
				for i := range g.Players {
					if stillPlaying(&g.Players[i]) {
						living = append(living, &g.Players[i])
					}
				}
				hunter := living[r.Intn(len(living))]
				victim := livingPlayer(g, hunter.VictimID)
				if victim == nil {
					t.Fatalf("%s: no living target for %s", context, hunter.ID)
				}
				if seed%3 == 0 || (seed%3 == 2 && step%2 == 0) {
					withdrawPlayer(g, hunter, victim, time.Now())
				} else {
					transferElimination(g, hunter, victim, time.Now())
				}
				transfers++
				check(g, fmt.Sprintf("%s after transfer %d", context, step))
			}
		}
	}
	for total := 2; total <= 14; total++ {
		visit(total, 1, nil)
	}
	t.Logf("checked %d valid starts, %d elimination/withdrawal transfers, %d impossible starts", starts, transfers, impossible)
}
