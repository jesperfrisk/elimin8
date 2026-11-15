package services

import (
	"context"
	"strings"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

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
