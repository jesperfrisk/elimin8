package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"golang.org/x/crypto/scrypt"
)

var (
	ErrNotFound     = errors.New("game not found")
	ErrConflict     = errors.New("game changed; please try again")
	ErrUnauthorized = errors.New("invalid or expired credentials")
	ErrForbidden    = errors.New("organizer access required")
	ErrLobbyClosed  = errors.New("this game is no longer accepting players")
	ErrInvalid      = errors.New("invalid input")
)

type GameRepository interface {
	Insert(context.Context, *models.Game) error
	Get(context.Context, string) (*models.Game, error)
	ByCode(context.Context, string) (*models.Game, error)
	Save(context.Context, *models.Game, int64) error
	PutAccess(context.Context, models.Access) error
	GetAccess(context.Context, string) (models.Access, error)
}

type GameService struct {
	repo GameRepository
	// A fixed lock pool bounds memory usage. All mutations of a given game take
	// the same mutex; MongoDB revision checks also serialize other API instances.
	locks  [256]sync.Mutex
	Notify func()
}

func NewGameService(repo GameRepository) *GameService { return &GameService{repo: repo} }

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

func (s *GameService) changed() {
	if s.Notify != nil {
		s.Notify()
	}
}

func (s *GameService) Create(ctx context.Context, name, description string) (models.Admission, error) {
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	if !validText(name, 100) || !utf8.ValidString(description) || utf8.RuneCountInString(description) > 2000 {
		return models.Admission{}, ErrInvalid
	}
	id, err := RandomCode(24)
	if err != nil {
		return models.Admission{}, err
	}
	code, err := RandomCode(24)
	if err != nil {
		return models.Admission{}, err
	}
	hash, err := hashCode(ctx, id, code)
	if err != nil {
		return models.Admission{}, err
	}
	g := &models.Game{ID: id, Name: name, Description: description, OrganizerHash: hash, Status: "lobby", Revision: 1, Players: []models.Player{}, CreatedAt: time.Now().UTC()}
	for attempt := 0; attempt < 5; attempt++ {
		g.InvitationCode, err = RandomCode(8)
		if err != nil {
			return models.Admission{}, err
		}
		err = s.repo.Insert(ctx, g)
		if !errors.Is(err, ErrConflict) {
			break
		}
	}
	if err != nil {
		return models.Admission{}, err
	}
	s.changed()
	a, err := s.admit(ctx, g, "", 0)
	a.ManagementCode = code
	return a, err
}

func (s *GameService) Login(ctx context.Context, invitation, code, role string) (models.Admission, error) {
	if len(invitation) > 32 || len(code) > 64 || (role != "organizer" && role != "player") {
		return models.Admission{}, ErrInvalid
	}
	g, err := s.repo.ByCode(ctx, NormalizeCode(invitation))
	if errors.Is(err, ErrNotFound) {
		return models.Admission{}, ErrUnauthorized
	}
	if err != nil {
		return models.Admission{}, err
	}
	hash, err := hashCode(ctx, g.ID, code)
	if err != nil {
		return models.Admission{}, err
	}
	if role == "organizer" {
		if !sameHash(hash, g.OrganizerHash) {
			return models.Admission{}, ErrUnauthorized
		}
		return s.admit(ctx, g, "", 0)
	}
	var playerID string
	var version int
	for _, p := range g.Players {
		if sameHash(hash, p.CodeHash) {
			playerID = p.ID
			version = p.CredentialVersion
			break
		}
	}
	if playerID == "" {
		return models.Admission{}, ErrUnauthorized
	}
	g, err = s.mutate(ctx, g.ID, func(current *models.Game) error {
		for i := range current.Players {
			p := &current.Players[i]
			if p.ID != playerID {
				continue
			}
			if p.CredentialVersion != version || !sameHash(p.CodeHash, hash) {
				return ErrUnauthorized
			}
			if current.Status != "lobby" && !p.Included && !(current.StartedAt == nil && p.JoinedAt != nil) {
				return ErrLobbyClosed
			}
			if p.JoinedAt == nil {
				now := time.Now().UTC()
				p.JoinedAt = &now
			}
			return nil
		}
		return ErrUnauthorized
	})
	if err != nil {
		return models.Admission{}, err
	}
	return s.admit(ctx, g, playerID, version)
}

func (s *GameService) admit(ctx context.Context, g *models.Game, playerID string, version int) (models.Admission, error) {
	token, err := RandomCode(48)
	if err != nil {
		return models.Admission{}, err
	}
	a := models.Access{Hash: HashToken(token), GameID: g.ID, PlayerID: playerID, Version: version, ExpiresAt: time.Now().UTC().Add(12 * time.Hour)}
	if err = s.repo.PutAccess(ctx, a); err != nil {
		return models.Admission{}, err
	}
	role := "organizer"
	if playerID != "" {
		role = "player"
	}
	return models.Admission{Token: token, ExpiresAt: a.ExpiresAt, Role: role, ClientPrefix: a.Hash[:24] + "-", Topic: Topic(g.ID, role), Game: View(g, a)}, nil
}

func Topic(id, role string) string { return "games/" + id + "/" + role + "/updates" }

func (s *GameService) Authorize(ctx context.Context, token string) (models.Access, *models.Game, error) {
	if len(token) != 48 {
		return models.Access{}, nil, ErrUnauthorized
	}
	a, err := s.repo.GetAccess(ctx, HashToken(token))
	if errors.Is(err, ErrNotFound) {
		return a, nil, ErrUnauthorized
	}
	if err != nil {
		return a, nil, err
	}
	if !time.Now().Before(a.ExpiresAt) {
		return a, nil, ErrUnauthorized
	}
	g, err := s.repo.Get(ctx, a.GameID)
	if err != nil {
		return a, nil, err
	}
	if a.PlayerID != "" {
		for _, p := range g.Players {
			if p.ID == a.PlayerID && p.CredentialVersion == a.Version && p.JoinedAt != nil && (g.Status == "lobby" || p.Included || g.StartedAt == nil) {
				return a, g, nil
			}
		}
		return a, nil, ErrUnauthorized
	}
	return a, g, nil
}

func View(g *models.Game, a models.Access) models.GameView {
	v := models.GameView{ID: g.ID, Name: g.Name, Description: g.Description, InvitationCode: g.InvitationCode, Status: g.Status, Revision: g.Revision, CreatedAt: g.CreatedAt, StartedAt: g.StartedAt, EndedAt: g.EndedAt, InvitedCount: len(g.Players)}
	for _, p := range g.Players {
		if p.JoinedAt != nil {
			v.JoinedCount++
		}
		if p.Included {
			v.ParticipantCount++
		}
		pv := models.PlayerView{ID: p.ID, Name: p.Name, JoinedAt: p.JoinedAt, Included: p.Included}
		if a.PlayerID == "" {
			v.Players = append(v.Players, pv)
		}
		if a.PlayerID == p.ID {
			v.Me = &pv
		}
	}
	return v
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
		if err = fn(g); err != nil {
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

func (s *GameService) Transition(ctx context.Context, a models.Access, action string) error {
	if a.PlayerID != "" {
		return ErrForbidden
	}
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		now := time.Now().UTC()
		switch action {
		case "start":
			if g.Status != "lobby" {
				return ErrConflict
			}
			g.Status = "active"
			g.StartedAt = &now
			for i := range g.Players {
				g.Players[i].Included = g.Players[i].JoinedAt != nil
			}
		case "end":
			if g.Status == "ended" {
				return ErrConflict
			}
			g.Status = "ended"
			g.EndedAt = &now
		default:
			return ErrInvalid
		}
		return nil
	})
	return err
}
