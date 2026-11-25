package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"golang.org/x/crypto/scrypt"
)

func RandomCode(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])&31]
	}
	return string(b), nil
}

func NormalizeCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code)))
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

var hashSlots = make(chan struct{}, 4)

func hashCode(ctx context.Context, gameID, code string) (string, error) {
	select {
	case hashSlots <- struct{}{}:
		defer func() { <-hashSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	b, err := scrypt.Key([]byte(NormalizeCode(code)), []byte(gameID), 32768, 8, 1, 32)
	return hex.EncodeToString(b), err
}

func sameHash(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func validText(s string, max int) bool {
	return s != "" && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max
}

func (s *GameService) AddPlayers(ctx context.Context, a models.Access, names []string) ([]models.IssuedPlayer, error) {
	if a.PlayerID != "" {
		return nil, ErrForbidden
	}
	if len(names) == 0 || len(names) > 50 {
		return nil, ErrInvalid
	}
	players := make([]models.Player, 0, len(names))
	issued := make([]models.IssuedPlayer, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !validText(name, 80) {
			return nil, ErrInvalid
		}
		id, err := RandomCode(20)
		if err != nil {
			return nil, err
		}
		code, err := RandomCode(8)
		if err != nil {
			return nil, err
		}
		hash, err := hashCode(ctx, a.GameID, code)
		if err != nil {
			return nil, err
		}
		players = append(players, models.Player{ID: id, Name: name, CodeHash: hash, CredentialVersion: 1})
		issued = append(issued, models.IssuedPlayer{ID: id, Name: name, Code: code})
	}
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if g.Status != "lobby" {
			return ErrLobbyClosed
		}
		seen := map[string]bool{}
		for _, p := range g.Players {
			seen[p.CodeHash] = true
		}
		for _, p := range players {
			if seen[p.CodeHash] {
				return ErrConflict
			}
			seen[p.CodeHash] = true
		}
		for i := range players {
			players[i].Number = len(g.Players) + i + 1
		}
		g.Players = append(g.Players, players...)
		return nil
	})
	return issued, err
}

func (s *GameService) ResetCode(ctx context.Context, a models.Access, id string) (models.IssuedPlayer, error) {
	if a.PlayerID != "" {
		return models.IssuedPlayer{}, ErrForbidden
	}
	code, err := RandomCode(8)
	if err != nil {
		return models.IssuedPlayer{}, err
	}
	hash, err := hashCode(ctx, a.GameID, code)
	if err != nil {
		return models.IssuedPlayer{}, err
	}
	var issued models.IssuedPlayer
	_, err = s.mutate(ctx, a.GameID, func(g *models.Game) error {
		if g.Status == "ended" {
			return ErrLobbyClosed
		}
		for _, p := range g.Players {
			if p.CodeHash == hash {
				return ErrConflict
			}
		}
		for i := range g.Players {
			p := &g.Players[i]
			if p.ID == id {
				p.CodeHash = hash
				p.CredentialVersion++
				issued = models.IssuedPlayer{ID: p.ID, Name: p.Name, Code: code}
				return nil
			}
		}
		return ErrNotFound
	})
	return issued, err
}
