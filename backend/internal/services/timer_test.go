package services_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func intp(i int) *int { return &i }

func TestTimerSetClearBoundsAndLobbyOnly(t *testing.T) {
	s, repo, admission, owner, players := setup(t)
	ctx := context.Background()
	for _, bad := range []int{0, -5, services.MaxTimerMinutes + 1} {
		if err := s.SetTimer(ctx, owner, intp(bad)); !errors.Is(err, services.ErrInvalid) {
			t.Fatalf("%d accepted: %v", bad, err)
		}
	}
	if err := s.SetTimer(ctx, owner, intp(30)); err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, owner.GameID)
	if g.Timer == nil || g.Timer.DurationMinutes != 30 || g.Timer.EndsAt != nil {
		t.Fatalf("unexpected lobby timer: %+v", g.Timer)
	}
	if err := s.SetTimer(ctx, owner, nil); err != nil {
		t.Fatal(err)
	}
	g, _ = repo.Get(ctx, owner.GameID)
	if g.Timer != nil {
		t.Fatal("timer not cleared")
	}
	joined, _ := s.Login(ctx, admission.Game.InvitationCode, players[0].Code, "player")
	pa, _, _ := s.Authorize(ctx, joined.Token)
	if err := s.SetTimer(ctx, pa, intp(5)); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("player set timer: %v", err)
	}
	if _, err := s.AddMissions(ctx, owner, []string{"Task"}); err != nil {
		t.Fatal(err)
	}
	joinPlayers(t, s, admission, players)
	if err := s.Transition(ctx, owner, "start"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimer(ctx, owner, intp(5)); !errors.Is(err, services.ErrAlreadyStarted) {
		t.Fatalf("timer set after start: %v", err)
	}
	g, _ = repo.Get(ctx, owner.GameID)
	if g.Timer != nil || g.Status != "active" {
		t.Fatal("game without timer changed behaviour")
	}
}

func TestTimerExpiresOnceLazilyAndBlocksEliminations(t *testing.T) {
	s, repo, admission, owner, players := setup(t)
	ctx := context.Background()
	clock := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return clock }
	notifications := 0
	s.Notify = func() { notifications++ }
	if _, err := s.AddMissions(ctx, owner, []string{"Task"}); err != nil {
		t.Fatal(err)
	}
	joinPlayers(t, s, admission, players)
	if err := s.SetTimer(ctx, owner, intp(10)); err != nil {
		t.Fatal(err)
	}
	if err := s.Transition(ctx, owner, "start"); err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, owner.GameID)
	want := clock.Add(10 * time.Minute)
	if g.Timer.EndsAt == nil || !g.Timer.EndsAt.Equal(want) {
		t.Fatalf("EndsAt = %v, want %v", g.Timer.EndsAt, want)
	}
	view := services.View(g, owner)
	if view.EndsAt == nil || view.RemainingSeconds == nil || view.EndReason != nil {
		t.Fatalf("unexpected active view: %+v", view)
	}

	// Not yet due: nothing changes.
	clock = clock.Add(9 * time.Minute)
	s.SweepTimers(ctx)
	g, _ = repo.Get(ctx, owner.GameID)
	if g.Status != "active" {
		t.Fatal("expired early")
	}

	// A "restarted" service (new instance, same repo) detects expiry.
	clock = clock.Add(2 * time.Minute)
	restarted := services.NewGameService(repo)
	restarted.Now = func() time.Time { return clock }
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); restarted.SweepTimers(ctx) }()
	}
	wg.Wait()
	g, _ = repo.Get(ctx, owner.GameID)
	if g.Status != "ended" || g.EndReason != "timer" || g.EndedAt == nil || !g.EndedAt.Equal(want) {
		t.Fatalf("not expired: %+v", g)
	}
	revision := g.Revision
	restarted.SweepTimers(ctx)
	if g, _ = repo.Get(ctx, owner.GameID); g.Revision != revision {
		t.Fatal("expiry applied twice")
	}
	if view = services.View(g, owner); view.EndReason == nil || *view.EndReason != "timer" || view.RemainingSeconds != nil {
		t.Fatalf("unexpected ended view: %+v", view)
	}

	joined, _ := restarted.Login(ctx, admission.Game.InvitationCode, players[0].Code, "player")
	pa, _, _ := restarted.Authorize(ctx, joined.Token)
	if _, err := restarted.ReportElimination(ctx, pa, g.Players[0].VictimID); !errors.Is(err, services.ErrElimination) {
		t.Fatalf("elimination after expiry: %v", err)
	}
}

func TestTimerLazyExpiryOnReadPublishesUpdate(t *testing.T) {
	s, repo, admission, owner, players := setup(t)
	ctx := context.Background()
	clock := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return clock }
	if _, err := s.AddMissions(ctx, owner, []string{"Task"}); err != nil {
		t.Fatal(err)
	}
	joinPlayers(t, s, admission, players)
	_ = s.SetTimer(ctx, owner, intp(1))
	if err := s.Transition(ctx, owner, "start"); err != nil {
		t.Fatal(err)
	}
	notified := 0
	s.Notify = func() { notified++ }
	clock = clock.Add(time.Minute)
	_, g, err := s.Authorize(ctx, admission.Token)
	if err != nil || g.Status != "ended" || g.EndReason != "timer" || notified != 1 {
		t.Fatalf("lazy expiry failed: %v %+v notified=%d", err, g, notified)
	}
	if stored, _ := repo.Get(ctx, owner.GameID); stored.Status != "ended" {
		t.Fatal("expiry not persisted")
	}
}
