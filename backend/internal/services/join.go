package services

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Self-join: a player registers in an open lobby with the shared invitation code
// and a display name that is unique in that game. Registration is one
// revision-checked write to the single game document (no multi-document
// transaction), so the uniqueness check, the lobby check and the numbering are
// atomic across API instances. A name never authenticates or reclaims anyone:
// recovery is the private code (POST /api/access) or an organizer reset.

var (
	// ErrNameTaken means another player in the game already has an equivalent name.
	ErrNameTaken = errors.New("that name is already taken in this game")
)

// RegistrationIncompleteError means the player was registered (the name is now
// taken and the private code exists) but the sign-in could not be completed, so
// a retry would only report name_taken. The code is returned to the same caller
// that would have received it on success, who can sign in with it through the
// normal code login.
type RegistrationIncompleteError struct {
	RecoveryCode string
	Err          error
}

func (e *RegistrationIncompleteError) Error() string {
	return "registered, but the sign-in could not be completed"
}
func (e *RegistrationIncompleteError) Unwrap() error { return e.Err }

// JoinResult is the new player's admission plus their private recovery code,
// which is shown once and is never part of any later response.
type JoinResult struct {
	models.Admission
	RecoveryCode string `json:"recoveryCode,omitempty"`
}

// NameKey is the comparison key for "the same name". It applies Unicode NFKC
// normalization, then case folding, then NFKC again (the standard NFKC_Casefold
// identifier match), drops control and invisible format characters, and collapses
// every run of whitespace to one space. So "Alex", " alex ", "ALEX", "Ａｌｅｘ"
// (full width), "Alex  B" and "Alex B", and é written as one character or as e
// plus an accent all collide. Look-alike letters from other alphabets (Cyrillic а
// for Latin a) are not detected. The display name keeps its original spelling.
func NameKey(name string) string {
	s := norm.NFKC.String(cases.Fold().String(norm.NFKC.String(name)))
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' ' // tabs and newlines are separators, not invisible
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func nameTaken(g *models.Game, key string) bool {
	for _, p := range g.Players {
		if NameKey(p.Name) == key {
			return true
		}
	}
	return false
}

// Join registers a new player in an open lobby and issues their ordinary
// admission. The caller (the HTTP layer) then issues the remember cookie.
func (s *GameService) Join(ctx context.Context, invitation, name string) (JoinResult, error) {
	if len(invitation) > 32 {
		return JoinResult{}, ErrInvalid
	}
	name = strings.TrimSpace(name)
	if !validText(name, 80) {
		return JoinResult{}, ErrInvalid
	}
	key := NameKey(name)
	if key == "" {
		return JoinResult{}, ErrInvalid // nothing visible to tell this player apart
	}
	g, err := s.repo.ByCode(ctx, NormalizeCode(invitation))
	if err != nil {
		return JoinResult{}, err // ErrNotFound for an unknown lobby
	}
	// Refuse early, before the deliberately expensive hash, when it cannot succeed.
	if g.Status != "lobby" {
		return JoinResult{}, ErrLobbyClosed
	}
	if nameTaken(g, key) {
		return JoinResult{}, ErrNameTaken
	}
	id, err := RandomCode(20)
	if err != nil {
		return JoinResult{}, err
	}
	code, err := RandomCode(8)
	if err != nil {
		return JoinResult{}, err
	}
	hash, err := hashCode(ctx, g.ID, code)
	if err != nil {
		return JoinResult{}, err
	}
	// The authoritative checks run inside the revision-checked write, so a
	// concurrent same-name join, a start or an end is decided exactly once: this
	// registration either commits into the lobby roster or is refused.
	g, err = s.mutate(ctx, g.ID, func(current *models.Game) error {
		if current.Status != "lobby" {
			return ErrLobbyClosed
		}
		for _, p := range current.Players {
			if NameKey(p.Name) == key {
				return ErrNameTaken
			}
			if sameHash(p.CodeHash, hash) {
				return ErrConflict // an astronomically unlikely code collision: try again
			}
		}
		now := time.Now().UTC()
		current.Players = append(current.Players, models.Player{
			ID: id, Name: name, CodeHash: hash, CredentialVersion: 1, JoinedAt: &now,
			// Players are only ever appended, so the number is stable and never reused.
			Number: len(current.Players) + 1,
		})
		return nil
	})
	if err != nil {
		return JoinResult{}, err
	}
	admission, err := s.admit(ctx, g, id, 1)
	if err != nil {
		return JoinResult{RecoveryCode: code}, &RegistrationIncompleteError{RecoveryCode: code, Err: err}
	}
	return JoinResult{Admission: admission, RecoveryCode: code}, nil
}

// JoinResume lets a valid player bearer of this same game get a fresh admission
// without creating another player or seeing the recovery code again. A missing,
// invalid, organizer or other-game bearer never claims a name.
func (s *GameService) JoinResume(ctx context.Context, invitation, token string) (models.Admission, error) {
	if len(invitation) > 32 {
		return models.Admission{}, ErrInvalid
	}
	a, _, err := s.Authorize(ctx, token)
	if err != nil {
		return models.Admission{}, err
	}
	g, err := s.repo.ByCode(ctx, NormalizeCode(invitation))
	if err != nil {
		return models.Admission{}, err
	}
	if a.PlayerID == "" || a.GameID != g.ID {
		return models.Admission{}, ErrForbidden
	}
	return s.reissueAdmission(ctx, a)
}

// reissueAdmission gives the same player a fresh short-lived admission. An
// admission that came from a remembered credential stays linked to it (so
// revoking the credential revokes this one too) and never outlives it; the
// 30-day deadline is never extended and no new cookie is issued.
func (s *GameService) reissueAdmission(ctx context.Context, a models.Access) (models.Admission, error) {
	expires := time.Now().UTC().Add(12 * time.Hour)
	if a.SessionHash != "" {
		store, ok := s.repo.(PlayerSessionRepository)
		if !ok {
			return models.Admission{}, errors.New("remembered credential storage unavailable")
		}
		session, err := store.GetPlayerSession(ctx, a.SessionHash)
		if errors.Is(err, ErrNotFound) {
			return models.Admission{}, ErrUnauthorized
		}
		if err != nil {
			return models.Admission{}, err
		}
		if session.ExpiresAt.Before(expires) {
			expires = session.ExpiresAt
		}
	}
	token, err := RandomCode(48)
	if err != nil {
		return models.Admission{}, err
	}
	fresh := models.Access{Hash: HashToken(token), GameID: a.GameID, PlayerID: a.PlayerID, Version: a.Version, ExpiresAt: expires, SessionHash: a.SessionHash}
	if err := s.repo.PutAccess(ctx, fresh); err != nil {
		return models.Admission{}, err
	}
	// Re-read after the write: a reset or revocation may have won the race.
	checked, g, err := s.Authorize(ctx, token)
	if err != nil {
		return models.Admission{}, err
	}
	return models.Admission{Token: token, ExpiresAt: checked.ExpiresAt, Role: "player", ClientPrefix: checked.Hash[:24] + "-", Topic: Topic(a.GameID, "player"), Game: View(g, checked)}, nil
}
