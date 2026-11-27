package services

import (
	"context"
	"errors"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

const RememberDuration = 30 * 24 * time.Hour

// PlayerSessionRepository is intentionally separate from GameRepository so
// unrelated game-service implementations do not gain session-storage methods.
type PlayerSessionRepository interface {
	PutPlayerSession(context.Context, models.PlayerSession) error
	GetPlayerSession(context.Context, string) (models.PlayerSession, error)
	DeletePlayerSession(context.Context, string) error
	RevokePlayerSession(context.Context, string, time.Time) error
	BindAccessSession(context.Context, string, string) error
}

// IssuePlayerSession requires a current player admission. Its secret is returned
// only to the cookie writer; storage and HTTP views never receive that secret.
func (s *GameService) IssuePlayerSession(ctx context.Context, admissionToken string) (string, models.PlayerSession, error) {
	store, ok := s.repo.(PlayerSessionRepository)
	if !ok {
		return "", models.PlayerSession{}, errors.New("remembered credential storage unavailable")
	}
	a, _, err := s.Authorize(ctx, admissionToken)
	if err != nil {
		return "", models.PlayerSession{}, err
	}
	if a.PlayerID == "" || a.SessionHash != "" {
		return "", models.PlayerSession{}, ErrUnauthorized
	}
	secret, err := RandomCode(48)
	if err != nil {
		return "", models.PlayerSession{}, err
	}
	now := s.now()
	session := models.PlayerSession{Hash: HashToken(secret), GameID: a.GameID, PlayerID: a.PlayerID, Version: a.Version, CreatedAt: now, ExpiresAt: now.Add(RememberDuration)}
	if err := store.PutPlayerSession(ctx, session); err != nil {
		return "", models.PlayerSession{}, err
	}
	// Bind the freshly created code-login admission too, so forgetting this
	// cookie revokes the browser's initial token as well as later renewals.
	if err := store.BindAccessSession(ctx, a.Hash, session.Hash); err != nil {
		_ = store.DeletePlayerSession(ctx, session.Hash)
		return "", models.PlayerSession{}, err
	}
	// A code reset or expiry during persistence must not issue a usable cookie.
	if _, err := s.ValidatePlayerSession(ctx, a.GameID, secret); err != nil {
		_ = store.DeletePlayerSession(ctx, session.Hash)
		return "", models.PlayerSession{}, err
	}
	return secret, session, nil
}

// ValidatePlayerSession does not renew expiry or issue bearer access; it is
// used by the narrowly scoped session exchange route.
func (s *GameService) ValidatePlayerSession(ctx context.Context, gameID, secret string) (models.PlayerSession, error) {
	if len(secret) != 48 || gameID == "" || len(gameID) > 128 {
		return models.PlayerSession{}, ErrUnauthorized
	}
	store, ok := s.repo.(PlayerSessionRepository)
	if !ok {
		return models.PlayerSession{}, errors.New("remembered credential storage unavailable")
	}
	hash := HashToken(secret)
	session, err := store.GetPlayerSession(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return models.PlayerSession{}, ErrUnauthorized
	}
	if err != nil {
		return models.PlayerSession{}, err
	}
	now := s.now()
	if !validPlayerSessionAt(session, gameID, hash, now) {
		return models.PlayerSession{}, ErrUnauthorized
	}
	g, err := s.repo.Get(ctx, gameID)
	if errors.Is(err, ErrNotFound) {
		return models.PlayerSession{}, ErrUnauthorized
	}
	if err != nil {
		return models.PlayerSession{}, err
	}
	if !playerCredentialCurrent(g, models.Access{PlayerID: session.PlayerID, Version: session.Version}) {
		return models.PlayerSession{}, ErrUnauthorized
	}
	return session, nil
}

func validPlayerSessionAt(session models.PlayerSession, gameID, hash string, now time.Time) bool {
	return session.Hash == hash && session.GameID == gameID && session.PlayerID != "" && session.RevokedAt == nil && now.Before(session.ExpiresAt) && !now.Before(session.CreatedAt) && session.ExpiresAt.Equal(session.CreatedAt.Add(RememberDuration))
}
