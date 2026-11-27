package services

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/storage"
)

// PhotoService keeps object bytes outside MongoDB and changes references with the
// same revision checks and notifications as every other game mutation.
type PhotoService struct {
	games     *GameService
	store     storage.Store
	Retention time.Duration
	Now       func() time.Time
}

func NewPhotoService(games *GameService, store storage.Store) *PhotoService {
	return &PhotoService{games: games, store: store, Retention: 24 * time.Hour}
}

func (s *PhotoService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return s.games.now()
}

func PhotoURL(gameID, photoID string) string {
	if photoID == "" {
		return ""
	}
	return "/api/games/" + url.PathEscape(gameID) + "/photos/" + url.PathEscape(photoID)
}

func photoKey(gameID, photoID string) string {
	return "games/" + url.PathEscape(gameID) + "/photos/" + url.PathEscape(photoID)
}

// validateAccess checks both the persisted credential and the freshly loaded
// game, including on mutation retries after code reset or a queued expiry.
func (s *PhotoService) validateAccess(ctx context.Context, g *models.Game, a models.Access) error {
	if a.Hash == "" || g.ID != a.GameID || !s.now().Before(a.ExpiresAt) {
		return ErrUnauthorized
	}
	current, err := s.games.repo.GetAccess(ctx, a.Hash)
	if errors.Is(err, ErrNotFound) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if current.GameID != a.GameID || current.PlayerID != a.PlayerID || current.Version != a.Version || !s.now().Before(current.ExpiresAt) {
		return ErrUnauthorized
	}
	if a.PlayerID == "" {
		return nil
	}
	if err := s.games.validateAccessSession(ctx, g, current); err != nil {
		return err
	}
	for _, p := range g.Players {
		if p.ID == a.PlayerID && p.CredentialVersion == a.Version && p.JoinedAt != nil && (g.Status == "lobby" || p.Included || g.StartedAt == nil) {
			return nil
		}
	}
	return ErrUnauthorized
}

func (s *PhotoService) writablePlayer(ctx context.Context, g *models.Game, a models.Access, playerID string) (*models.Player, error) {
	if err := s.validateAccess(ctx, g, a); err != nil {
		return nil, err
	}
	if a.PlayerID != "" && a.PlayerID != playerID {
		return nil, ErrForbidden
	}
	if g.Status == "ended" {
		return nil, ErrGameEnded
	}
	if g.Status != "lobby" && g.Status != "active" {
		return nil, ErrConflict
	}
	for i := range g.Players {
		if g.Players[i].ID == playerID {
			return &g.Players[i], nil
		}
	}
	return nil, ErrNotFound
}

func (s *PhotoService) Put(ctx context.Context, a models.Access, playerID string, data []byte) (string, error) {
	g, err := s.games.repo.Get(ctx, a.GameID)
	if err != nil {
		return "", err
	}
	if _, err = s.writablePlayer(ctx, g, a, playerID); err != nil {
		return "", err
	}
	encoded, err := preparePhoto(ctx, data)
	if err != nil {
		return "", err
	}
	id, err := RandomCode(32)
	if err != nil {
		return "", err
	}
	key := photoKey(g.ID, id)
	if err = s.store.Put(ctx, key, encoded); err != nil {
		s.discard(ctx, key)
		return "", err
	}
	var previous string
	_, err = s.games.mutate(ctx, a.GameID, func(current *models.Game) error {
		p, err := s.writablePlayer(ctx, current, a, playerID)
		if err != nil {
			return err
		}
		previous, p.PhotoID = p.PhotoID, id
		return nil
	})
	if err != nil {
		// A database timeout can mean the revision-checked write committed but
		// its acknowledgement was lost. Deleting this key would then break the
		// persisted reference. Only semantic precommit failures are definitive;
		// leave other staged objects for the durable post-game prefix sweep.
		switch {
		case errors.Is(err, ErrUnauthorized), errors.Is(err, ErrForbidden),
			errors.Is(err, ErrGameEnded), errors.Is(err, ErrLobbyClosed),
			errors.Is(err, ErrInvalid), errors.Is(err, ErrNotFound):
			s.discard(ctx, key)
		}
		return "", err
	}
	if previous != "" {
		s.discard(ctx, photoKey(g.ID, previous))
	}
	return PhotoURL(g.ID, id), nil
}

func (s *PhotoService) Delete(ctx context.Context, a models.Access, playerID string) error {
	var previous string
	_, err := s.games.mutate(ctx, a.GameID, func(g *models.Game) error {
		p, err := s.writablePlayer(ctx, g, a, playerID)
		if err != nil {
			return err
		}
		previous, p.PhotoID = p.PhotoID, ""
		if previous == "" {
			return errNoChange
		}
		return nil
	})
	if err == nil && previous != "" {
		s.discard(ctx, photoKey(a.GameID, previous))
	}
	return err
}

func (s *PhotoService) discard(ctx context.Context, key string) {
	// A failed immediate deletion is harmless to reads, which resolve current
	// references only. The post-game sweep also deletes stale/orphan objects.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = s.store.Delete(cleanupCtx, key)
}

func (s *PhotoService) readable(ctx context.Context, g *models.Game, photoID string, a *models.Access) error {
	retention := s.Retention
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	if g.Status == "ended" && (g.EndedAt == nil || !s.now().Before(g.EndedAt.Add(retention))) {
		return ErrNotFound
	}
	if a != nil {
		if g.ID != a.GameID {
			return ErrForbidden
		}
		if err := s.validateAccess(ctx, g, *a); err != nil {
			return err
		}
	}
	for _, p := range g.Players {
		if photoID == "" || p.PhotoID != photoID {
			continue
		}
		public := p.Included || (g.Status == "lobby" && p.JoinedAt != nil)
		if public || (a != nil && (a.PlayerID == "" || a.PlayerID == p.ID)) {
			return nil
		}
	}
	return ErrNotFound
}

func (s *PhotoService) Get(ctx context.Context, gameID, photoID string, a *models.Access) ([]byte, error) {
	g, err := s.games.repo.Get(ctx, gameID)
	if err != nil {
		return nil, err
	}
	g, err = s.games.ExpireDue(ctx, g)
	if err != nil {
		return nil, err
	}
	if err = s.readable(ctx, g, photoID, a); err != nil {
		return nil, err
	}
	data, err := s.store.Get(ctx, photoKey(gameID, photoID))
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// Recheck after I/O: a concurrent replacement, code reset or retention
	// boundary must not turn a stale reference into a public object endpoint.
	g, err = s.games.repo.Get(ctx, gameID)
	if err != nil {
		return nil, err
	}
	if err = s.readable(ctx, g, photoID, a); err != nil {
		return nil, err
	}
	return data, nil
}
