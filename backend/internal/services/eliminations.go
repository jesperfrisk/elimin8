package services

import (
	"context"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

// stillPlaying reports whether a participant is neither eliminated nor withdrawn.
func stillPlaying(p *models.Player) bool {
	return p.Included && p.EliminatedAt == nil && p.WithdrawnAt == nil
}

func livingPlayer(g *models.Game, id string) *models.Player {
	for i := range g.Players {
		p := &g.Players[i]
		if p.ID == id && stillPlaying(p) {
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
	settleAfterTransfer(g, now)
}

// Claims survive reconnects but cannot be acted on once a participant is out or
// the killer's assignment moved to someone else (team-circle repair can change
// a living hunter's target). A stale pending claim would otherwise block the
// killer from reporting the new target.
func cancelEliminations(g *models.Game, now time.Time) {
	for i := range g.Eliminations {
		claim := &g.Eliminations[i]
		if claim.Status != "pending" {
			continue
		}
		killer, victim := livingPlayer(g, claim.KillerID), livingPlayer(g, claim.VictimID)
		if g.Status != "active" || killer == nil || victim == nil || killer.VictimID != claim.VictimID {
			claim.Status, claim.ResolvedAt = "cancelled", &now
		}
	}
}
