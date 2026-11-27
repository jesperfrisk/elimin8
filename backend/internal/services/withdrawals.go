package services

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

// withdrawalTTL is how long an unconfirmed request stays valid. An unconfirmed
// request changes nothing and is simply dropped once it expires.
const withdrawalTTL = 2 * time.Minute

var ErrWithdrawal = errors.New("this withdrawal is no longer available; refresh the game")

// PlayerStatus is the public status of a participant of a started game.
func PlayerStatus(p models.Player) string {
	switch {
	case p.WithdrawnAt != nil:
		return "withdrawn"
	case p.EliminatedAt != nil:
		return "eliminated"
	default:
		return "active"
	}
}

// RequestWithdrawal starts the two-step withdrawal. It only records a pending
// request (replacing any earlier one by the same player); nothing about the
// game's assignments changes until ConfirmWithdrawal.
func (s *GameService) RequestWithdrawal(ctx context.Context, a models.Access) (models.WithdrawalRequest, error) {
	if a.PlayerID == "" {
		return models.WithdrawalRequest{}, ErrForbidden
	}
	token, err := RandomCode(32)
	if err != nil {
		return models.WithdrawalRequest{}, err
	}
	request := models.WithdrawalRequest{Token: token, ExpiresAt: time.Now().UTC().Add(withdrawalTTL)}
	_, err = s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if err := s.revalidatePlayer(g, a); err != nil {
			return err
		}
		if g.Status != "active" || livingPlayer(g, a.PlayerID) == nil {
			return ErrWithdrawal
		}
		g.Withdrawals = append(pendingWithdrawalsExcept(g, a.PlayerID, time.Now().UTC()), models.Withdrawal{
			PlayerID: a.PlayerID, TokenHash: HashToken(token), ExpiresAt: request.ExpiresAt,
		})
		return nil
	})
	return request, err
}

// ConfirmWithdrawal withdraws the player if token matches their live request.
// The hunter inherits the victim and mission, no kill is credited, and the
// last remaining participant wins exactly as after an elimination.
func (s *GameService) ConfirmWithdrawal(ctx context.Context, a models.Access, token string) error {
	if a.PlayerID == "" {
		return ErrForbidden
	}
	if token == "" {
		return ErrInvalid
	}
	hash := HashToken(token)
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if err := s.revalidatePlayer(g, a); err != nil {
			return err
		}
		now := time.Now().UTC()
		confirmed := false
		for _, w := range g.Withdrawals {
			if w.PlayerID == a.PlayerID && w.ExpiresAt.After(now) && subtle.ConstantTimeCompare([]byte(w.TokenHash), []byte(hash)) == 1 {
				confirmed = true
			}
		}
		withdrawer := livingPlayer(g, a.PlayerID)
		if g.Status != "active" || withdrawer == nil || !confirmed {
			return ErrWithdrawal
		}
		var hunter *models.Player
		for i := range g.Players {
			if p := &g.Players[i]; stillPlaying(p) && p.VictimID == withdrawer.ID {
				hunter = p
			}
		}
		if hunter == nil {
			return ErrWithdrawal
		}
		g.Withdrawals = pendingWithdrawalsExcept(g, a.PlayerID, now)
		withdrawPlayer(g, hunter, withdrawer, now)
		return nil
	})
	return err
}

// withdrawPlayer removes withdrawer from the circle like an elimination does,
// but records no kill and credits nobody.
func withdrawPlayer(g *models.Game, hunter, withdrawer *models.Player, now time.Time) {
	hunter.VictimID, hunter.Mission = withdrawer.VictimID, withdrawer.Mission
	withdrawer.WithdrawnAt = &now
	withdrawer.VictimID, withdrawer.Mission = "", ""
	settleAfterTransfer(g, now)
	cancelEliminations(g, now)
}

// pendingWithdrawalsExcept returns the unexpired requests of other players.
func pendingWithdrawalsExcept(g *models.Game, playerID string, now time.Time) []models.Withdrawal {
	var kept []models.Withdrawal
	for _, w := range g.Withdrawals {
		if w.PlayerID != playerID && w.ExpiresAt.After(now) {
			kept = append(kept, w)
		}
	}
	return kept
}
