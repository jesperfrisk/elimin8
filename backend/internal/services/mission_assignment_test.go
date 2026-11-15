package services

import (
	"fmt"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

func TestAssignmentUsesEveryEnabledMissionBeforeRepeating(t *testing.T) {
	for _, missionCount := range []int{1, 2, 3, 10} {
		for _, playerCount := range []int{0, 1, 2, 3, 7, 21, 100} {
			t.Run(fmt.Sprintf("%d_missions_%d_players", missionCount, playerCount), func(t *testing.T) {
				now := time.Now()
				g := &models.Game{}
				for i := 0; i < missionCount; i++ {
					g.Missions = append(g.Missions, models.LobbyMission{Text: fmt.Sprintf("Task %d", i), Enabled: true})
				}
				g.Missions = append(g.Missions, models.LobbyMission{Text: "Disabled"})
				for i := 0; i < playerCount; i++ {
					g.Players = append(g.Players, models.Player{JoinedAt: &now})
					// Invited players between joined players must not consume a task.
					g.Players = append(g.Players, models.Player{})
				}
				if err := assignMissions(g); err != nil {
					t.Fatal(err)
				}
				counts, round := map[string]int{}, map[string]bool{}
				assigned := 0
				for _, p := range g.Players {
					if p.JoinedAt == nil {
						if p.Mission != "" {
							t.Fatal("unjoined invitee received a mission")
						}
						continue
					}
					if p.Mission == "" || p.Mission == "Disabled" || round[p.Mission] {
						t.Fatalf("invalid assignment or reuse before exhausting round: %q", p.Mission)
					}
					round[p.Mission] = true
					counts[p.Mission]++
					assigned++
					if assigned%missionCount == 0 {
						if len(round) != missionCount {
							t.Fatal("round skipped an enabled mission")
						}
						clear(round)
					}
				}
				for _, m := range g.Missions[:missionCount] {
					if count := counts[m.Text]; count < playerCount/missionCount || count > (playerCount+missionCount-1)/missionCount {
						t.Fatalf("unbalanced reuse: %+v", counts)
					}
				}
			})
		}
	}
}

func TestRepeatedMissionTextRepresentsSeparateEntries(t *testing.T) {
	now := time.Now()
	g := &models.Game{Missions: []models.LobbyMission{
		{Text: "Repeated", Enabled: true}, {Text: "Repeated", Enabled: true}, {Text: "Other", Enabled: true},
	}}
	for i := 0; i < 12; i++ {
		g.Players = append(g.Players, models.Player{JoinedAt: &now})
	}
	if err := assignMissions(g); err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < len(g.Players); offset += 3 {
		counts := map[string]int{}
		for _, p := range g.Players[offset : offset+3] {
			counts[p.Mission]++
		}
		if counts["Repeated"] != 2 || counts["Other"] != 1 {
			t.Fatalf("round failed to use each separate entry: %+v", counts)
		}
	}
}
