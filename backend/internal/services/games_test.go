package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func setup(t *testing.T) (*services.GameService, *testutil.Memory, models.Admission, models.Access, []models.IssuedPlayer) {
	t.Helper()
	repo := testutil.NewMemory()
	s := services.NewGameService(repo)
	ctx := context.Background()
	a, err := s.Create(ctx, "Friday game", "Bring friends")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := s.Authorize(ctx, a.Token)
	if err != nil {
		t.Fatal(err)
	}
	players, err := s.AddPlayers(ctx, owner, []string{"Alice", "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	return s, repo, a, owner, players
}

func TestLifecycleAndPrivacy(t *testing.T) {
	s, _, game, owner, players := setup(t)
	ctx := context.Background()
	joined, err := s.Login(ctx, strings.ToLower(game.Game.InvitationCode), players[0].Code, "player")
	if err != nil {
		t.Fatal(err)
	}
	a, g, err := s.Authorize(ctx, joined.Token)
	if err != nil {
		t.Fatal(err)
	}
	view := services.View(g, a)
	if view.Me == nil || view.Me.Name != "Alice" || len(view.Players) != 0 || view.JoinedCount != 1 {
		t.Fatalf("unexpected player view: %+v", view)
	}
	data, _ := json.Marshal(view)
	for _, secret := range []string{players[0].Code, g.OrganizerHash, g.Players[0].CodeHash, "codeHash", "organizerHash"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("private data exposed")
		}
	}
	if err = s.Transition(ctx, a, "start"); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("player started game: %v", err)
	}
	if _, err = s.AddPlayers(ctx, a, []string{"Mallory"}); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("player edited roster: %v", err)
	}
	if err = s.Transition(ctx, owner, "start"); err != nil {
		t.Fatal(err)
	}
	_, g, err = s.Authorize(ctx, joined.Token)
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != "active" || !g.Players[0].Included || g.Players[1].Included {
		t.Fatal("incorrect frozen roster")
	}
	if _, err = s.Login(ctx, game.Game.InvitationCode, players[1].Code, "player"); !errors.Is(err, services.ErrLobbyClosed) {
		t.Fatalf("late player joined: %v", err)
	}
	if _, err = s.Login(ctx, game.Game.InvitationCode, players[0].Code, "player"); err != nil {
		t.Fatalf("participant could not reconnect: %v", err)
	}
	if _, err = s.AddPlayers(ctx, owner, []string{"Late"}); !errors.Is(err, services.ErrLobbyClosed) {
		t.Fatalf("roster changed after start: %v", err)
	}
	if err = s.Transition(ctx, owner, "end"); err != nil {
		t.Fatal(err)
	}
	_, g, err = s.Authorize(ctx, joined.Token)
	if err != nil || g.Status != "ended" || g.EndedAt == nil {
		t.Fatal("ended game inaccessible")
	}
	if err = s.Transition(ctx, owner, "start"); !errors.Is(err, services.ErrConflict) {
		t.Fatal("ended game restarted")
	}
}

func TestPrivateCodeResetAndOrganizerLogin(t *testing.T) {
	s, _, game, owner, players := setup(t)
	ctx := context.Background()
	if _, err := s.Login(ctx, game.Game.InvitationCode, game.ManagementCode, "organizer"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, game.Game.InvitationCode, players[0].Code, "organizer"); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("player code granted organizer access")
	}
	joined, err := s.Login(ctx, game.Game.InvitationCode, players[0].Code, "player")
	if err != nil {
		t.Fatal(err)
	}
	reset, err := s.ResetCode(ctx, owner, players[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Authorize(ctx, joined.Token); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("reset did not revoke access")
	}
	if _, err = s.Login(ctx, game.Game.InvitationCode, players[0].Code, "player"); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatal("old code still accepted")
	}
	if _, err = s.Login(ctx, game.Game.InvitationCode, reset.Code, "player"); err != nil {
		t.Fatal(err)
	}
}

func TestStartHoldsLockUntilCommitted(t *testing.T) {
	s, repo, game, owner, players := setup(t)
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	repo.BeforeSave = func(g *models.Game) {
		if g.Status == "active" {
			once.Do(func() { close(entered); <-release })
		}
	}
	started := make(chan error, 1)
	go func() { started <- s.Transition(ctx, owner, "start") }()
	<-entered
	joined := make(chan error, 1)
	go func() { _, err := s.Login(ctx, game.Game.InvitationCode, players[0].Code, "player"); joined <- err }()
	select {
	case err := <-joined:
		t.Fatalf("join finished while start held lock: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	if err := <-joined; !errors.Is(err, services.ErrLobbyClosed) {
		t.Fatalf("join slipped past start: %v", err)
	}
	g, _ := repo.Get(ctx, game.Game.ID)
	if g.Players[0].JoinedAt != nil || g.Players[0].Included {
		t.Fatal("late player included")
	}
}

func TestOtherInstanceJoinForcesStartToRetry(t *testing.T) {
	s, repo, game, owner, players := setup(t)
	other := services.NewGameService(repo)
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	repo.BeforeSave = func(g *models.Game) {
		if g.Status == "active" {
			once.Do(func() { close(entered); <-release })
		}
	}
	started := make(chan error, 1)
	go func() { started <- s.Transition(ctx, owner, "start") }()
	<-entered
	if _, err := other.Login(ctx, game.Game.InvitationCode, players[0].Code, "player"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, game.Game.ID)
	if !g.Players[0].Included || g.Players[1].Included {
		t.Fatal("start failed to retry with latest joined roster")
	}
}

func TestEmptyGameAndCancelledLobby(t *testing.T) {
	ctx := context.Background()
	s := services.NewGameService(testutil.NewMemory())
	game, err := s.Create(ctx, "Empty", "")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, _ := s.Authorize(ctx, game.Token)
	if err = s.Transition(ctx, owner, "start"); err != nil {
		t.Fatal(err)
	}
	if err = s.Transition(ctx, owner, "end"); err != nil {
		t.Fatal(err)
	}
	s, _, game, owner, players := setup(t)
	joined, err := s.Login(ctx, game.Game.InvitationCode, players[0].Code, "player")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Transition(ctx, owner, "end"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Authorize(ctx, joined.Token); err != nil {
		t.Fatal("joined player cannot see cancelled lobby")
	}
	if _, err = s.Login(ctx, game.Game.InvitationCode, players[1].Code, "player"); !errors.Is(err, services.ErrLobbyClosed) {
		t.Fatal("new player joined ended lobby")
	}
}
