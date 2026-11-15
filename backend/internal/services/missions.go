package services

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"strings"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

var ErrNoMissions = errors.New("enable at least one mission before starting a game with players")

// Each shuffled round consumes every enabled entry once before another round
// begins. Assignment and lifecycle changes are committed in one game write.
func assignMissions(g *models.Game) error {
	texts := make([]string, 0, len(g.Missions))
	for _, mission := range g.Missions {
		if mission.Enabled {
			texts = append(texts, mission.Text)
		}
	}
	assigned := 0
	for i := range g.Players {
		p := &g.Players[i]
		p.Mission = ""
		if p.JoinedAt == nil {
			continue
		}
		if len(texts) == 0 {
			return ErrNoMissions
		}
		if assigned%len(texts) == 0 {
			for j := len(texts) - 1; j > 0; j-- {
				index, err := rand.Int(rand.Reader, big.NewInt(int64(j+1)))
				if err != nil {
					return err
				}
				k := int(index.Int64())
				texts[j], texts[k] = texts[k], texts[j]
			}
		}
		p.Mission = texts[assigned%len(texts)]
		assigned++
	}
	return nil
}

func (s *GameService) AddMissions(ctx context.Context, a models.Access, texts []string) ([]models.LobbyMission, error) {
	if a.PlayerID != "" {
		return nil, ErrForbidden
	}
	if len(texts) == 0 || len(texts) > 100 {
		return nil, ErrInvalid
	}
	missions := make([]models.LobbyMission, 0, len(texts))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if !validText(text, 1000) || strings.ContainsAny(text, "\r\n") {
			return nil, ErrInvalid
		}
		id, err := RandomCode(20)
		if err != nil {
			return nil, err
		}
		// Repeated text is intentional: each entry can become one player's task.
		missions = append(missions, models.LobbyMission{ID: id, Text: text, Enabled: true})
	}
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if g.Status != "lobby" {
			return ErrLobbyClosed
		}
		g.Missions = append(g.Missions, missions...)
		return nil
	})
	return missions, err
}

func (s *GameService) SetMissionEnabled(ctx context.Context, a models.Access, id string, enabled bool) error {
	if a.PlayerID != "" {
		return ErrForbidden
	}
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if g.Status != "lobby" {
			return ErrLobbyClosed
		}
		for i := range g.Missions {
			if g.Missions[i].ID == id {
				g.Missions[i].Enabled = enabled
				return nil
			}
		}
		return ErrNotFound
	})
	return err
}
