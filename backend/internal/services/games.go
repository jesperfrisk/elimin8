package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

var (
	ErrNotFound           = errors.New("game not found")
	ErrConflict           = errors.New("game changed; please try again")
	ErrUnauthorized       = errors.New("invalid or expired credentials")
	ErrForbidden          = errors.New("organizer access required")
	ErrLobbyClosed        = errors.New("this game is no longer accepting players")
	ErrInvalid            = errors.New("invalid input")
	ErrAlreadyStarted     = fmt.Errorf("game has already started: %w", ErrConflict)
	ErrGameEnded          = fmt.Errorf("game has ended: %w", ErrConflict)
	ErrElimination        = errors.New("this elimination is no longer available; refresh the game")
	ErrPendingElimination = errors.New("a confirmation is already pending for this killer or victim")
)

type GameRepository interface {
	Insert(context.Context, *models.Game) error
	Get(context.Context, string) (*models.Game, error)
	ByCode(context.Context, string) (*models.Game, error)
	Save(context.Context, *models.Game, int64) error
	PutAccess(context.Context, models.Access) error
	GetAccess(context.Context, string) (models.Access, error)
	// DueTimers lists active games whose timer deadline is at or before now.
	DueTimers(context.Context, time.Time) ([]string, error)
}

type GameService struct {
	repo GameRepository
	// A fixed lock pool bounds memory usage. All mutations of a given game take
	// the same mutex; MongoDB revision checks also serialize other API instances.
	locks  [256]sync.Mutex
	Notify func()
	// Intn overrides random selection for team randomization (tests); nil uses crypto/rand.
	Intn func(int) int
	// Now overrides the clock for timer logic (tests); nil uses time.Now.
	Now func() time.Time
}

func NewGameService(repo GameRepository) *GameService { return &GameService{repo: repo} }

func (s *GameService) changed() {
	if s.Notify != nil {
		s.Notify()
	}
}

func (s *GameService) mutate(ctx context.Context, id string, fn func(*models.Game) error) (*models.Game, error) {
	h := sha256.Sum256([]byte(id))
	lock := &s.locks[h[0]]
	lock.Lock()
	defer lock.Unlock()
	for attempt := 0; attempt < 16; attempt++ {
		g, err := s.repo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		previous := g.Revision
		// Lazy expiry: persist a due timer first so fn sees the ended game.
		if s.expireTimer(g, s.now()) {
			g.Revision++
			err = s.repo.Save(ctx, g, previous)
			if errors.Is(err, ErrConflict) {
				continue
			}
			if err != nil {
				return nil, err
			}
			s.changed()
			continue
		}
		if err = fn(g); errors.Is(err, errNoChange) {
			return g, nil
		} else if err != nil {
			return nil, err
		}
		g.Revision++
		err = s.repo.Save(ctx, g, previous)
		if errors.Is(err, ErrConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		s.changed()
		return g, nil
	}
	return nil, ErrConflict
}
