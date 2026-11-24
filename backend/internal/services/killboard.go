package services

import (
	"sort"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

// KillBoard builds the public Kill Board for a game: every participant's id,
// name and status, and nothing else. Entries are sorted by name (then id) so
// the order never reveals who was eliminated or withdrew first.
func KillBoard(g *models.Game) models.KillBoardView {
	entries := []models.KillBoardEntry{}
	if g.Status != "lobby" {
		for _, p := range g.Players {
			if !p.Included {
				continue
			}
			entries = append(entries, models.KillBoardEntry{ID: p.ID, Name: p.Name, Status: killBoardStatus(p)})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].ID < entries[j].ID
	})
	return models.KillBoardView{Entries: entries}
}

func killBoardStatus(p models.Player) string {
	switch {
	case p.WithdrawnAt != nil:
		return models.KillBoardWithdrawn
	case p.EliminatedAt != nil:
		return models.KillBoardEliminated
	default:
		return models.KillBoardActive
	}
}
