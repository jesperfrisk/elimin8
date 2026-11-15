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

func TestLobbyMissionsPersistenceIsolationAndPrivacy(t *testing.T) {
	s, repo, admission, owner, players := setup(t)
	ctx := context.Background()
	missions, err := s.AddMissions(ctx, owner, []string{"  Get a high-five.  ", "Say pineapple.", "Get a high-five."})
	if err != nil || len(missions) != 3 {
		t.Fatalf("add missions: %+v %v", missions, err)
	}
	if missions[0].Text != "Get a high-five." || missions[0].ID == missions[2].ID {
		t.Fatal("text should be trimmed and repeated tasks should have independent IDs")
	}
	for _, m := range missions {
		if !m.Enabled || m.ID == "" {
			t.Fatal("new mission must have an ID and be enabled")
		}
	}
	if err = s.SetMissionEnabled(ctx, owner, missions[0].ID, false); err != nil {
		t.Fatal(err)
	}
	// Configuration persists across service recreation and organizer login.
	s = services.NewGameService(repo)
	reopened, err := s.Login(ctx, admission.Game.InvitationCode, admission.ManagementCode, "organizer")
	if err != nil || reopened.Game.Missions == nil {
		t.Fatalf("reopen missions: %v", err)
	}
	saved := *reopened.Game.Missions
	if len(saved) != 3 || saved[0].Enabled || !saved[2].Enabled || saved[0].Text != missions[0].Text {
		t.Fatalf("disabled task was lost or affected another entry: %+v", saved)
	}
	if err = s.SetMissionEnabled(ctx, owner, missions[0].ID, true); err != nil {
		t.Fatal(err)
	}
	other, err := s.Create(ctx, "Another lobby", "")
	if err != nil || other.Game.Missions == nil || len(*other.Game.Missions) != 0 {
		t.Fatal("a new lobby must not inherit missions")
	}
	otherOwner, _, _ := s.Authorize(ctx, other.Token)
	if err = s.SetMissionEnabled(ctx, otherOwner, missions[0].ID, false); !errors.Is(err, services.ErrNotFound) {
		t.Fatalf("another game could toggle this mission: %v", err)
	}
	joined, err := s.Login(ctx, admission.Game.InvitationCode, players[0].Code, "player")
	if err != nil {
		t.Fatal(err)
	}
	player, g, _ := s.Authorize(ctx, joined.Token)
	data, _ := json.Marshal(services.View(g, player))
	if strings.Contains(string(data), "missions") || strings.Contains(string(data), missions[0].Text) {
		t.Fatalf("player saw lobby mission configuration: %s", data)
	}
	if _, err = s.AddMissions(ctx, player, []string{"Forbidden"}); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("player added missions: %v", err)
	}
	if err = s.SetMissionEnabled(ctx, player, missions[0].ID, false); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("player toggled mission: %v", err)
	}
}

func TestLobbyMissionsValidationIsAtomicAndSelectionLocks(t *testing.T) {
	s, repo, game, owner, players := setup(t)
	ctx := context.Background()
	joinPlayers(t, s, game, players)
	before, _ := repo.Get(ctx, owner.GameID)
	for _, texts := range [][]string{
		nil, {}, {"Valid", " "}, {"Valid", strings.Repeat("x", 1001)},
		{string([]byte{0xff})}, {"First\nSecond"}, {"First\rSecond"}, make([]string, 101),
	} {
		if _, err := s.AddMissions(ctx, owner, texts); !errors.Is(err, services.ErrInvalid) {
			t.Fatalf("accepted invalid mission batch: %v", err)
		}
	}
	g, _ := repo.Get(ctx, owner.GameID)
	if len(g.Missions) != 0 || g.Revision != before.Revision {
		t.Fatal("invalid batch partially saved")
	}
	texts := make([]string, 100)
	for i := range texts {
		texts[i] = strings.Repeat("å", 1000)
	}
	batch, err := s.AddMissions(ctx, owner, texts)
	if err != nil || len(batch) != 100 {
		t.Fatalf("maximum batch with Unicode text rejected: %v", err)
	}
	if _, err = s.AddMissions(ctx, owner, []string{"An additional mission"}); err != nil {
		t.Fatalf("total mission count was incorrectly limited by batch size: %v", err)
	}
	if err = s.SetMissionEnabled(ctx, owner, batch[0].ID, false); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"start", "end"} {
		if err = s.Transition(ctx, owner, action); err != nil {
			t.Fatal(err)
		}
		if _, err = s.AddMissions(ctx, owner, []string{"Late mission"}); !errors.Is(err, services.ErrLobbyClosed) {
			t.Fatalf("missions added after %s: %v", action, err)
		}
		if err = s.SetMissionEnabled(ctx, owner, batch[0].ID, true); !errors.Is(err, services.ErrLobbyClosed) {
			t.Fatalf("mission toggled after %s: %v", action, err)
		}
		g, _ = repo.Get(ctx, owner.GameID)
		if len(g.Missions) != 101 || g.Missions[0].Enabled {
			t.Fatal("closed lobby configuration changed")
		}
	}
}

func TestMissionMutationRetriesPreserveConcurrentChanges(t *testing.T) {
	s, repo, _, owner, _ := setup(t)
	ctx := context.Background()
	batch, _ := s.AddMissions(ctx, owner, []string{"Existing"})
	repo.BeforeSave = func(g *models.Game) {
		repo.BeforeSave = nil
		other, _ := repo.Get(ctx, g.ID)
		previous := other.Revision
		other.Missions[0].Enabled = false
		other.Revision++
		if err := repo.Save(ctx, other, previous); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddMissions(ctx, owner, []string{"New"}); err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, owner.GameID)
	if len(g.Missions) != 2 || g.Missions[0].Enabled || g.Missions[0].ID != batch[0].ID {
		t.Fatalf("retry lost disable or duplicated batch: %+v", g.Missions)
	}
	// A competing start must also prevent a stale toggle from being committed.
	repo.BeforeSave = func(g *models.Game) {
		repo.BeforeSave = nil
		other, _ := repo.Get(ctx, g.ID)
		previous := other.Revision
		other.Status = "active"
		other.Revision++
		if err := repo.Save(ctx, other, previous); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetMissionEnabled(ctx, owner, batch[0].ID, true); !errors.Is(err, services.ErrLobbyClosed) {
		t.Fatalf("toggle won a stale race with start: %v", err)
	}
}
