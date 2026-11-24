package services

import (
	"context"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

func livingPlayer(g *models.Game, id string) *models.Player {
	for i := range g.Players {
		p := &g.Players[i]
		if p.ID == id && p.Included && p.EliminatedAt == nil {
			return p
		}
	}
	return nil
}

func (s *GameService) ReportElimination(ctx context.Context, a models.Access, victimID string) (models.Elimination, error) {
	if a.PlayerID == "" {
		return models.Elimination{}, ErrForbidden
	}
	if victimID == "" || victimID == a.PlayerID {
		return models.Elimination{}, ErrInvalid
	}
	id, err := RandomCode(20)
	if err != nil {
		return models.Elimination{}, err
	}
	claim := models.Elimination{ID: id, KillerID: a.PlayerID, VictimID: victimID, Status: "pending", CreatedAt: time.Now().UTC()}
	_, err = s.mutate(ctx, a.GameID, func(g *models.Game) error {
		killer := livingPlayer(g, a.PlayerID)
		if g.Status != "active" || killer == nil || livingPlayer(g, victimID) == nil || killer.VictimID != victimID {
			return ErrElimination
		}
		for _, existing := range g.Eliminations {
			if existing.Status == "pending" && (existing.KillerID == a.PlayerID || existing.VictimID == victimID) {
				return ErrPendingElimination
			}
		}
		g.Eliminations = append(g.Eliminations, claim)
		return nil
	})
	return claim, err
}

func (s *GameService) RespondElimination(ctx context.Context, a models.Access, id string, accept bool) error {
	if a.PlayerID == "" {
		return ErrForbidden
	}
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		for i := range g.Eliminations {
			claim := &g.Eliminations[i]
			if claim.ID != id {
				continue
			}
			if claim.VictimID != a.PlayerID {
				return ErrForbidden
			}
			if g.Status != "active" || claim.Status != "pending" {
				return ErrElimination
			}
			killer, victim := livingPlayer(g, claim.KillerID), livingPlayer(g, claim.VictimID)
			if killer == nil || victim == nil || (accept && killer.VictimID != victim.ID) {
				return ErrElimination
			}
			now := time.Now().UTC()
			claim.Status, claim.ResolvedAt = "declined", &now
			if !accept {
				return nil
			}
			claim.Status = "accepted"
			transferElimination(g, killer, victim, now)
			cancelEliminations(g, now)
			return nil
		}
		return ErrNotFound
	})
	return err
}

func transferElimination(g *models.Game, killer, victim *models.Player, now time.Time) {
	// Removing the assigned target preserves the circle for all survivors.
	killer.VictimID, killer.Mission = victim.VictimID, victim.Mission
	recordKill(killer)
	victim.EliminatedAt = &now
	victim.VictimID, victim.Mission = "", ""
	remaining := 0
	for _, p := range g.Players {
		if p.Included && p.EliminatedAt == nil {
			remaining++
		}
	}
	if remaining == 1 {
		killer.VictimID, killer.Mission = "", ""
		g.Status, g.EndedAt, g.WinnerID = "ended", &now, killer.ID
		return
	}
	// Team mode: never leave a hunter targeting a teammate. When no safe circle
	// exists the transfer stands and ValidAssignmentExists reports it (#19).
	_ = repairTeamCircle(g)
}

// Claims survive reconnects but cannot be acted on once a participant is out.
func cancelEliminations(g *models.Game, now time.Time) {
	for i := range g.Eliminations {
		claim := &g.Eliminations[i]
		if claim.Status == "pending" && (g.Status != "active" || livingPlayer(g, claim.KillerID) == nil || livingPlayer(g, claim.VictimID) == nil) {
			claim.Status, claim.ResolvedAt = "cancelled", &now
		}
	}
}
