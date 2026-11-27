package services

import (
	"context"
	"errors"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

// ResumePlayerSession issues ordinary short-lived access, never a new remember
// cookie or a fresh 30-day period. The source hash makes revocation effective
// for all admissions exchanged from this credential, including MQTT access.
func (s *GameService) ResumePlayerSession(ctx context.Context, gameID, secret string) (models.Admission, error) {
	session, err := s.ValidatePlayerSession(ctx, gameID, secret)
	if err != nil {
		return models.Admission{}, err
	}
	token, err := RandomCode(48)
	if err != nil {
		return models.Admission{}, err
	}
	expires := time.Now().UTC().Add(12 * time.Hour)
	if session.ExpiresAt.Before(expires) {
		expires = session.ExpiresAt
	}
	a := models.Access{Hash: HashToken(token), GameID: gameID, PlayerID: session.PlayerID, Version: session.Version, ExpiresAt: expires, SessionHash: session.Hash}
	if err := s.repo.PutAccess(ctx, a); err != nil {
		return models.Admission{}, err
	}
	// Re-read after the write: a reset or revocation may have won the race.
	a, g, err := s.Authorize(ctx, token)
	if err != nil {
		return models.Admission{}, err
	}
	return models.Admission{Token: token, ExpiresAt: a.ExpiresAt, Role: "player", ClientPrefix: a.Hash[:24] + "-", Topic: Topic(gameID, "player"), Game: View(g, a)}, nil
}

// ForgetPlayerSession is idempotent and cannot revoke another game's cookie.
// Mark first, then fence the game revision before acknowledging success. Any
// mutation validated before the marker must either commit before this fence or
// retry and see revocation. A failed fence leaves a durable marker for retries;
// it never reactivates the credential. No game-domain state spans documents.
func (s *GameService) ForgetPlayerSession(ctx context.Context, gameID, secret string) error {
	if len(secret) != 48 || gameID == "" || len(gameID) > 128 {
		return nil
	}
	store, ok := s.repo.(PlayerSessionRepository)
	if !ok {
		return errors.New("remembered credential storage unavailable")
	}
	hash := HashToken(secret)
	session, err := store.GetPlayerSession(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if session.Hash != hash || session.GameID != gameID || session.PlayerID == "" {
		return nil
	}
	_, err = s.mutate(ctx, gameID, func(*models.Game) error {
		return store.RevokePlayerSession(ctx, hash, s.now())
	})
	if errors.Is(err, ErrNotFound) {
		return store.RevokePlayerSession(ctx, hash, s.now())
	}
	return err
}

func (s *GameService) validateAccessSession(ctx context.Context, g *models.Game, a models.Access) error {
	if a.SessionHash == "" {
		return nil
	}
	store, ok := s.repo.(PlayerSessionRepository)
	if !ok {
		return errors.New("remembered credential storage unavailable")
	}
	session, err := store.GetPlayerSession(ctx, a.SessionHash)
	if errors.Is(err, ErrNotFound) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if !validPlayerSessionAt(session, g.ID, a.SessionHash, s.now()) || session.PlayerID != a.PlayerID || session.Version != a.Version {
		return ErrUnauthorized
	}
	return nil
}
