package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func TestMissionAssignmentPrivacyAndPersistence(t *testing.T) {
	s, repo, admission, owner, players := setup(t)
	ctx := context.Background()
	if _, err := s.AddMissions(ctx, owner, []string{"High-five", "Pineapple"}); err != nil {
		t.Fatal(err)
	}
	logins := make([]models.Admission, 2)
	for i, p := range players {
		var err error
		logins[i], err = s.Login(ctx, admission.Game.InvitationCode, p.Code, "player")
		if err != nil || logins[i].Game.Me.Mission != "" {
			t.Fatalf("lobby admission leaked a mission or failed: %v", err)
		}
	}
	if err := s.Transition(ctx, owner, "start"); err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, owner.GameID)
	if g.Players[0].Mission == "" || g.Players[1].Mission == "" || g.Players[0].Mission == g.Players[1].Mission {
		t.Fatal("two distinct tasks were not assigned")
	}
	want := []string{g.Players[0].Mission, g.Players[1].Mission}
	// Each assignment owns its text; it survives removal of the lobby candidates.
	previous := g.Revision
	g.Missions = nil
	g.Revision++
	if err := repo.Save(ctx, g, previous); err != nil {
		t.Fatal(err)
	}
	s = services.NewGameService(repo)
	for i, login := range logins {
		a, current, err := s.Authorize(ctx, login.Token)
		if err != nil {
			t.Fatal(err)
		}
		view := services.View(current, a)
		data, _ := json.Marshal(view)
		if view.Me.Mission != want[i] || view.Missions != nil || len(view.Players) != 0 || strings.Contains(string(data), want[1-i]) {
			t.Fatalf("incorrect private assignment: %s", data)
		}
		rejoined, err := s.Login(ctx, admission.Game.InvitationCode, players[i].Code, "player")
		if err != nil || rejoined.Game.Me.Mission != want[i] {
			t.Fatalf("mission changed on login: %v", err)
		}
	}
	_, current, _ := s.Authorize(ctx, admission.Token)
	view := services.View(current, owner)
	if view.Players[0].Mission != want[0] || view.Players[1].Mission != want[1] {
		t.Fatal("organizer cannot see assignments")
	}
	if err := s.Transition(ctx, owner, "start"); !errors.Is(err, services.ErrConflict) {
		t.Fatalf("second start rerolled missions: %v", err)
	}
	if err := s.Transition(ctx, owner, "end"); err != nil {
		t.Fatal(err)
	}
	a, current, _ := s.Authorize(ctx, logins[0].Token)
	if services.View(current, a).Me.Mission != want[0] {
		t.Fatal("ending lost assignment")
	}
}

func TestStartRequiresEnabledMissionForJoinedPlayers(t *testing.T) {
	s, repo, admission, owner, players := setup(t)
	ctx := context.Background()
	if _, err := s.Login(ctx, admission.Game.InvitationCode, players[0].Code, "player"); err != nil {
		t.Fatal(err)
	}
	for _, disabled := range []bool{false, true} {
		if disabled {
			missions, err := s.AddMissions(ctx, owner, []string{"Disabled"})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SetMissionEnabled(ctx, owner, missions[0].ID, false); err != nil {
				t.Fatal(err)
			}
		}
		before, _ := repo.Get(ctx, owner.GameID)
		if err := s.Transition(ctx, owner, "start"); !errors.Is(err, services.ErrNoMissions) {
			t.Fatalf("start without enabled missions: %v", err)
		}
		after, _ := repo.Get(ctx, owner.GameID)
		if after.Status != "lobby" || after.StartedAt != nil || after.Players[0].Included || after.Revision != before.Revision || after.Players[0].Mission != "" {
			t.Fatal("failed start partially changed game")
		}
	}
}

func TestStartRetriesAssignmentWithLatestMissionConfiguration(t *testing.T) {
	for _, disableAll := range []bool{false, true} {
		s, repo, admission, owner, players := setup(t)
		ctx := context.Background()
		if _, err := s.Login(ctx, admission.Game.InvitationCode, players[0].Code, "player"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AddMissions(ctx, owner, []string{"Before", "After"}); err != nil {
			t.Fatal(err)
		}
		repo.BeforeSave = func(g *models.Game) {
			repo.BeforeSave = nil
			other, _ := repo.Get(ctx, g.ID)
			previous := other.Revision
			other.Missions[0].Enabled = false
			other.Missions[1].Enabled = !disableAll
			other.Revision++
			if err := repo.Save(ctx, other, previous); err != nil {
				t.Fatal(err)
			}
		}
		err := s.Transition(ctx, owner, "start")
		g, _ := repo.Get(ctx, owner.GameID)
		if disableAll {
			if !errors.Is(err, services.ErrNoMissions) || g.Status != "lobby" || g.Players[0].Mission != "" {
				t.Fatalf("retry ignored disabled missions: %v", err)
			}
		} else if err != nil || g.Players[0].Mission != "After" || g.Status != "active" {
			t.Fatalf("retry assigned stale mission: %+v %v", g, err)
		}
	}
}
