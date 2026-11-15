package services

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

func TestVictimsFormOneCircleOfJoinedPlayers(t *testing.T) {
	for _, count := range []int{2, 3, 4, 7, 20, 100} {
		t.Run(fmt.Sprintf("%d_players", count), func(t *testing.T) {
			now := time.Now()
			g := &models.Game{}
			for i := 0; i < count; i++ {
				g.Players = append(g.Players, models.Player{ID: fmt.Sprintf("joined-%d", i), JoinedAt: &now})
				g.Players = append(g.Players, models.Player{ID: fmt.Sprintf("invited-%d", i)})
			}
			if err := assignVictims(g); err != nil {
				t.Fatal(err)
			}
			byID, hunters := map[string]models.Player{}, map[string]int{}
			for _, p := range g.Players {
				if p.JoinedAt == nil {
					if p.VictimID != "" {
						t.Fatal("unjoined invitee assigned a victim")
					}
					continue
				}
				if p.VictimID == "" || p.VictimID == p.ID {
					t.Fatalf("missing victim or self-target: %+v", p)
				}
				byID[p.ID] = p
				hunters[p.VictimID]++
			}
			for id := range byID {
				if hunters[id] != 1 {
					t.Fatalf("%s has %d hunters", id, hunters[id])
				}
			}
			visited := map[string]bool{}
			id := g.Players[0].ID
			for i := 0; i < count; i++ {
				if visited[id] {
					t.Fatal("circle closed before including every participant")
				}
				p, ok := byID[id]
				if !ok {
					t.Fatal("victim is not a participant")
				}
				visited[id] = true
				id = p.VictimID
			}
			if id != g.Players[0].ID {
				t.Fatal("circle does not close")
			}
		})
	}
}

func TestVictimsRequireTwoJoinedPlayers(t *testing.T) {
	now := time.Now()
	for _, players := range [][]models.Player{nil, {{ID: "solo", JoinedAt: &now}, {ID: "invited"}}} {
		if err := assignVictims(&models.Game{Players: players}); !errors.Is(err, ErrTooFewPlayers) {
			t.Fatalf("accepted fewer than two joined players: %v", err)
		}
	}
}
