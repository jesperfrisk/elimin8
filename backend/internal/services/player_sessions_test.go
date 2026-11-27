package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func TestPlayerSessionFixedExpiryIsolationAndNoBearerAccess(t *testing.T) {
	s, repo, game, _, players := setup(t)
	ctx := context.Background()
	login, err := s.Login(ctx, game.Game.InvitationCode, players[0].Code, "player")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	issued := now
	s.Now = func() time.Time { return now }
	secret, record, err := s.IssuePlayerSession(ctx, login.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 48 || record.Hash == secret || record.Hash != services.HashToken(secret) || record.PlayerID != players[0].ID || record.GameID != game.Game.ID || !record.ExpiresAt.Equal(issued.Add(30*24*time.Hour)) {
		t.Fatal("invalid remembered credential")
	}
	stored, err := repo.GetPlayerSession(ctx, services.HashToken(secret))
	if err != nil || stored != record {
		t.Fatal("hash-only credential not persisted")
	}
	data, err := json.Marshal(record)
	if err != nil || string(data) != "{}" {
		t.Fatal("session metadata must not serialize to JSON")
	}
	if _, _, err := s.Authorize(ctx, secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("remember cookie authorized as bearer")
	}
	for _, value := range []string{"", strings.Repeat("a", 49), strings.Repeat("a", 48), login.Token} {
		if _, err := s.ValidatePlayerSession(ctx, game.Game.ID, value); !errors.Is(err, services.ErrUnauthorized) {
			t.Fatal("invalid credential accepted")
		}
	}
	if _, err := s.ValidatePlayerSession(ctx, "another-game", secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("credential crossed games")
	}
	// A new service instance models a process restart; the repository owns state.
	restarted := services.NewGameService(repo)
	restarted.Now = func() time.Time { return now }
	now = issued.Add(services.RememberDuration - time.Nanosecond)
	if validated, err := restarted.ValidatePlayerSession(ctx, game.Game.ID, secret); err != nil || validated.ExpiresAt != record.ExpiresAt {
		t.Fatal("validation changed expiry or lost persisted credential")
	}
	now = record.ExpiresAt
	if _, err := restarted.ValidatePlayerSession(ctx, game.Game.ID, secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("expiry boundary accepted")
	}
	if _, err := repo.GetPlayerSession(ctx, record.Hash); err != nil {
		t.Fatal("test requires delayed TTL deletion")
	}
	now = issued.Add(-time.Second)
	if _, err := restarted.ValidatePlayerSession(ctx, game.Game.ID, secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("future issuance accepted")
	}
}

func TestPlayerSessionRegenerationAndAdmissionRules(t *testing.T) {
	s, repo, game, owner, players := setup(t)
	ctx := context.Background()
	login, err := s.Login(ctx, game.Game.InvitationCode, players[0].Code, "player")
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := s.IssuePlayerSession(ctx, login.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.IssuePlayerSession(ctx, game.Token); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("organizer received remembered player identity")
	}
	reset, err := s.ResetCode(ctx, owner, players[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidatePlayerSession(ctx, game.Game.ID, secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("old version survived code reset")
	}
	if _, _, err := s.IssuePlayerSession(ctx, login.Token); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("revoked admission issued cookie")
	}
	recovered, err := s.Login(ctx, game.Game.InvitationCode, reset.Code, "player")
	if err != nil || recovered.Game.Me.ID != players[0].ID {
		t.Fatal("recovery did not retain player")
	}
	secret, _, err = s.IssuePlayerSession(ctx, recovered.Token)
	if err != nil {
		t.Fatal(err)
	}
	setGame := func(change func(*models.Game)) {
		t.Helper()
		g, err := repo.Get(ctx, game.Game.ID)
		if err != nil {
			t.Fatal(err)
		}
		previous := g.Revision
		change(g)
		g.Revision++
		if err := repo.Save(ctx, g, previous); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	for _, state := range []string{"active", "eliminated", "withdrawn", "ended", "cancelled-lobby"} {
		setGame(func(g *models.Game) {
			g.Status, g.StartedAt = "active", &now
			g.Players[0].Included = true
			if state == "eliminated" {
				g.Players[0].EliminatedAt = &now
			}
			if state == "withdrawn" {
				g.Players[0].WithdrawnAt = &now
			}
			if state == "ended" {
				g.Status = "ended"
			}
			if state == "cancelled-lobby" {
				g.Status, g.StartedAt, g.Players[0].Included = "ended", nil, false
			}
		})
		if _, err := s.ValidatePlayerSession(ctx, game.Game.ID, secret); err != nil {
			t.Fatalf("admitted %s player could not return", state)
		}
	}
	setGame(func(g *models.Game) { g.Status, g.StartedAt, g.Players[0].Included = "active", &now, false })
	if _, err := s.ValidatePlayerSession(ctx, game.Game.ID, secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("excluded legacy invitee accepted")
	}
	setGame(func(g *models.Game) { g.Status, g.StartedAt, g.Players[0].JoinedAt = "lobby", nil, nil })
	if _, err := s.ValidatePlayerSession(ctx, game.Game.ID, secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("unjoined player accepted")
	}
}

type sessionRaceStore struct {
	*testutil.Memory
	afterPut func()
	putError error
}

func (r *sessionRaceStore) PutPlayerSession(ctx context.Context, session models.PlayerSession) error {
	if r.putError != nil {
		return r.putError
	}
	if err := r.Memory.PutPlayerSession(ctx, session); err != nil {
		return err
	}
	if r.afterPut != nil {
		r.afterPut()
	}
	return nil
}

func TestPlayerSessionIssuanceRechecksResetAndStorageFailure(t *testing.T) {
	s, memory, game, owner, players := setup(t)
	ctx := context.Background()
	login, err := s.Login(ctx, game.Game.InvitationCode, players[0].Code, "player")
	if err != nil {
		t.Fatal(err)
	}
	repo := &sessionRaceStore{Memory: memory, putError: errors.New("private storage failure")}
	issuer := services.NewGameService(repo)
	if secret, _, err := issuer.IssuePlayerSession(ctx, login.Token); err == nil || secret != "" || len(memory.Sessions) != 0 {
		t.Fatal("failed storage issued a credential")
	}
	repo.putError = nil
	repo.afterPut = func() {
		if _, err := s.ResetCode(ctx, owner, players[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	if secret, _, err := issuer.IssuePlayerSession(ctx, login.Token); !errors.Is(err, services.ErrUnauthorized) || secret != "" || len(memory.Sessions) != 0 {
		t.Fatal("reset during issuance left a usable credential")
	}
}
