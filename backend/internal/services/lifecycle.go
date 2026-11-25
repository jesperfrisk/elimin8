package services

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

// GameIDLength is the length of newly generated game ids.
const GameIDLength = 26

func (s *GameService) Create(ctx context.Context, name, description string) (models.Admission, error) {
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	if !validText(name, 100) || !utf8.ValidString(description) || utf8.RuneCountInString(description) > 2000 {
		return models.Admission{}, ErrInvalid
	}
	// 26 characters x 5 bits = 130 bits: the id doubles as the public board
	// link, so it must be unguessable. Older 24-character (120-bit) ids stay valid.
	id, err := RandomCode(GameIDLength)
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
	if g, err = s.ExpireDue(ctx, g); err != nil {
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

func (s *GameService) Transition(ctx context.Context, a models.Access, action string) error {
	if a.PlayerID != "" {
		return ErrForbidden
	}
	_, err := s.mutate(ctx, a.GameID, func(g *models.Game) error {
		now := s.now()
		switch action {
		case "start":
			if g.Status == "ended" {
				return ErrGameEnded
			}
			if g.Status != "lobby" {
				return ErrAlreadyStarted
			}
			if g.Mode == models.ModeTeam {
				if err := validateTeamStart(g); err != nil {
					return err
				}
			}
			if err := assignVictims(g); err != nil {
				return err
			}
			if err := assignMissions(g); err != nil {
				return err
			}
			g.Status = "active"
			g.StartedAt = &now
			startTimer(g, now)
			for i := range g.Players {
				g.Players[i].Included = g.Players[i].JoinedAt != nil
			}
		case "end":
			if g.Status == "ended" {
				return ErrConflict
			}
			g.Status = "ended"
			g.EndedAt = &now
			cancelEliminations(g, now)
		default:
			return ErrInvalid
		}
		return nil
	})
	return err
}
