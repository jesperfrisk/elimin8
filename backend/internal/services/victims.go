package services

import (
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

var ErrTooFewPlayers = errors.New("at least two players must join the lobby before starting")

// A shuffled circle gives every participant exactly one victim and one hunter,
// with no self-targets or disconnected groups. Invitees do not enter the circle.
func assignVictims(g *models.Game) error {
	participants := make([]int, 0, len(g.Players))
	for i, p := range g.Players {
		if p.JoinedAt != nil {
			participants = append(participants, i)
		}
	}
	if len(participants) < 2 {
		return ErrTooFewPlayers
	}
	for i := len(participants) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		j := int(n.Int64())
		participants[i], participants[j] = participants[j], participants[i]
	}
	for i := range g.Players {
		g.Players[i].VictimID = ""
	}
	for i, index := range participants {
		next := participants[(i+1)%len(participants)]
		g.Players[index].VictimID = g.Players[next].ID
	}
	return nil
}
