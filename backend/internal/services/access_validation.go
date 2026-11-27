package services

import (
	"context"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

// playerCredentialCurrent reports whether the player named by a is still the
// holder of that credential version in the freshly loaded game. It is the one
// rule shared by Authorize (at the HTTP boundary) and by protected mutations.
func playerCredentialCurrent(g *models.Game, a models.Access) bool {
	for _, p := range g.Players {
		if p.ID == a.PlayerID {
			return p.CredentialVersion == a.Version && p.JoinedAt != nil && (g.Status == "lobby" || p.Included || g.StartedAt == nil)
		}
	}
	return false
}

// revalidatePlayer must run inside a mutation callback, so it sees the game
// loaded for this attempt and repeats after every revision-conflict retry. A
// request authorized before a code reset, or one whose access expired while it
// waited, is rejected as unauthorized (expiry uses the wall clock, like
// Authorize, not the injectable game clock) without changing any state. Access values
// loaded from the repository always carry an expiry, so a zero ExpiresAt only
// occurs for values built directly by in-process callers.
func (s *GameService) revalidatePlayer(ctx context.Context, g *models.Game, a models.Access) error {
	if !a.ExpiresAt.IsZero() && !time.Now().Before(a.ExpiresAt) {
		return ErrUnauthorized
	}
	if !playerCredentialCurrent(g, a) {
		return ErrUnauthorized
	}
	return s.validateAccessSession(ctx, g, a)
}
