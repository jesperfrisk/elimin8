package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

const (
	MinTimerMinutes = 1
	MaxTimerMinutes = 7 * 24 * 60
)

// errNoChange lets a mutate callback skip the save without reporting a failure.
var errNoChange = errors.New("no change")

func (s *GameService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// SetTimer sets (minutes != nil) or clears (minutes == nil) the time limit.
// Organizer only and only while the game is in the lobby.
func (s *GameService) SetTimer(ctx context.Context, a models.Access, minutes *int) error {
	if a.PlayerID != "" {
		return ErrForbidden
	}
	if minutes != nil && (*minutes < MinTimerMinutes || *minutes > MaxTimerMinutes) {
		return ErrInvalid
	}
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if g.Status == "ended" {
			return ErrGameEnded
		}
		if g.Status != "lobby" {
			return ErrAlreadyStarted
		}
		if minutes == nil {
			g.Timer = nil
		} else {
			g.Timer = &models.Timer{DurationMinutes: *minutes}
		}
		return nil
	})
	return err
}

// startTimer is called from the start path; it fixes the deadline.
func startTimer(g *models.Game, now time.Time) {
	if g.Timer != nil && g.Timer.DurationMinutes > 0 {
		ends := now.Add(time.Duration(g.Timer.DurationMinutes) * time.Minute)
		g.Timer.EndsAt = &ends
	}
}

// timerView fills the additive timer fields shared by all roles.
func timerView(v *models.GameView, g *models.Game) {
	if g.Timer != nil {
		minutes := g.Timer.DurationMinutes
		v.TimerMinutes = &minutes
		v.EndsAt = g.Timer.EndsAt
		if g.Status == "active" && g.Timer.EndsAt != nil {
			remaining := int64(time.Until(*g.Timer.EndsAt).Seconds())
			if remaining < 0 {
				remaining = 0
			}
			v.RemainingSeconds = &remaining
		}
	}
	reason := g.EndReason
	if reason == "" && g.Status == "ended" && g.WinnerID != "" {
		reason = "survival"
	}
	if reason != "" {
		v.EndReason = &reason
	}
}

// timerDue reports whether an active game's deadline has passed.
func timerDue(g *models.Game, now time.Time) bool {
	return g.Status == "active" && g.Timer != nil && g.Timer.EndsAt != nil && !now.Before(*g.Timer.EndsAt)
}

// expireTimer moves a due game to "ended" with EndReason "timer". Callers
// persist it with a revision-checked save, so it happens exactly once.
func (s *GameService) expireTimer(g *models.Game, now time.Time) bool {
	if !timerDue(g, now) {
		return false
	}
	ends := *g.Timer.EndsAt
	g.Status, g.EndedAt, g.EndReason = "ended", &ends, "timer"
	cancelEliminations(g, ends)
	s.OnTimerExpired(g)
	return true
}

// ExpireDue lazily expires a loaded game if its deadline has passed and
// returns the current state.
func (s *GameService) ExpireDue(ctx context.Context, g *models.Game) (*models.Game, error) {
	if !timerDue(g, s.now()) {
		return g, nil
	}
	return s.mutate(ctx, g.ID, func(*models.Game) error { return errNoChange })
}

// SweepTimers expires every due game. Run periodically so expiry is published
// even when nobody reads or writes the game, and after a restart.
func (s *GameService) SweepTimers(ctx context.Context) {
	ids, err := s.repo.DueTimers(ctx, s.now())
	if err != nil {
		slog.Warn("timer sweep failed")
		return
	}
	for _, id := range ids {
		if _, err := s.mutate(ctx, id, func(*models.Game) error { return errNoChange }); err != nil {
			slog.Warn("timer expiry failed")
		}
	}
}

// RunTimerSweep calls SweepTimers on an interval until ctx is cancelled.
func (s *GameService) RunTimerSweep(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.SweepTimers(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
