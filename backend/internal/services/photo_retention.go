package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/storage"
)

const (
	photoRetentionBatchSize   = 100
	photoRetentionGameTimeout = 30 * time.Second
)

// PhotoRetentionRepository discovers persisted expired games using keyset pages.
// It is separate from GameRepository so unrelated game stores need not implement it.
type PhotoRetentionRepository interface {
	DuePhotoGames(ctx context.Context, endedBefore time.Time, afterID string, limit int) ([]string, error)
}

type PhotoRetentionService struct {
	games     *GameService
	repo      PhotoRetentionRepository
	store     storage.Store
	retention time.Duration
	Now       func() time.Time
}

func NewPhotoRetentionService(games *GameService, repo PhotoRetentionRepository, store storage.Store, retention time.Duration) *PhotoRetentionService {
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	return &PhotoRetentionService{games: games, repo: repo, store: store, retention: retention}
}

func (s *PhotoRetentionService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func photoGameExpired(g *models.Game, endedBefore time.Time) bool {
	return g.Status == "ended" && g.EndedAt != nil && !g.EndedAt.After(endedBefore)
}

// Sweep deletes every object of expired games, including replacements and failed
// uploads. Revisit prefixes on future sweeps: an already in-flight put may leave
// an orphan after a previous sweep. A permanent cleanup marker would miss it.
func (s *PhotoRetentionService) Sweep(ctx context.Context) error {
	endedBefore := s.now().Add(-s.retention)
	afterID := ""
	var failure error
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(failure, err)
		}
		ids, err := s.repo.DuePhotoGames(ctx, endedBefore, afterID, photoRetentionBatchSize)
		if err != nil {
			if ctx.Err() != nil {
				return errors.Join(failure, ctx.Err())
			}
			return errors.Join(failure, errors.New("photo retention discovery failed"))
		}
		for _, id := range ids {
			// Even a broken discovery implementation must not loop indefinitely.
			if id <= afterID {
				return errors.Join(failure, errors.New("photo retention discovery did not advance"))
			}
			afterID = id
			// One slow or failing prefix must not monopolize the sweep. The
			// shorter parent deadline, if any, is preserved by WithTimeout.
			gameCtx, cancel := context.WithTimeout(ctx, photoRetentionGameTimeout)
			err := s.purge(gameCtx, id, endedBefore)
			cancel()
			if err != nil {
				failure = errors.New("photo retention cleanup failed")
			}
			if err := ctx.Err(); err != nil {
				return errors.Join(failure, err)
			}
		}
		if len(ids) < photoRetentionBatchSize {
			return failure
		}
	}
}

func (s *PhotoRetentionService) purge(ctx context.Context, id string, endedBefore time.Time) error {
	g, err := s.games.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	// Stale/bad discovery results must never delete unexpired or active photos.
	if !photoGameExpired(g, endedBefore) {
		return nil
	}
	prefix := "games/" + g.ID + "/photos/"
	objects, err := s.store.List(ctx, prefix)
	if err != nil {
		return err
	}
	var failed bool
	for _, object := range objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.HasPrefix(object.Key, prefix) {
			failed = true
			continue
		}
		if err := s.store.Delete(ctx, object.Key); err != nil && !errors.Is(err, storage.ErrNotFound) {
			failed = true
		}
	}
	if failed {
		return errors.New("photo object cleanup failed")
	}
	// List returns a bounded batch. Keep the references until a fresh listing
	// confirms the whole prefix is empty; later sweeps process remaining batches
	// without blocking cleanup of other games in this sweep.
	if len(objects) != 0 {
		remaining, err := s.store.List(ctx, prefix)
		if err != nil {
			return err
		}
		if len(remaining) != 0 {
			return nil
		}
	}
	_, err = s.games.mutate(ctx, id, func(current *models.Game) error {
		if !photoGameExpired(current, endedBefore) {
			return errNoChange
		}
		changed := false
		for i := range current.Players {
			if current.Players[i].PhotoID != "" {
				current.Players[i].PhotoID = ""
				changed = true
			}
		}
		if !changed {
			return errNoChange
		}
		return nil
	})
	return err
}

// Run sweeps at startup and retries periodically. Logs never contain IDs, object
// paths, credentials or untrusted storage errors.
func (s *PhotoRetentionService) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Minute
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("photo retention sweep failed; will retry")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
