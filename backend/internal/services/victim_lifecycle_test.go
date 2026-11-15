package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func TestStartRejectsZeroOrOneJoinedPlayerWithoutChangingGame(t *testing.T) {
	s, repo, admission, owner, players := setup(t)
	ctx := context.Background()
	if _, err := s.AddMissions(ctx, owner, []string{"Task"}); err != nil {
		t.Fatal(err)
	}
	for joined := 0; joined < 2; joined++ {
		if joined == 1 {
			joinPlayers(t, s, admission, players[:1])
		}
		before, _ := repo.Get(ctx, owner.GameID)
		if err := s.Transition(ctx, owner, "start"); !errors.Is(err, services.ErrTooFewPlayers) {
			t.Fatalf("started with %d joined: %v", joined, err)
		}
		after, _ := repo.Get(ctx, owner.GameID)
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if string(beforeJSON) != string(afterJSON) {
			t.Fatal("rejected start changed game or assignments")
		}
	}
	joinPlayers(t, s, admission, players[1:])
	if err := s.Transition(ctx, owner, "start"); err != nil {
		t.Fatal(err)
	}
	g, _ := repo.Get(ctx, owner.GameID)
	if g.Players[0].VictimID != g.Players[1].ID || g.Players[1].VictimID != g.Players[0].ID {
		t.Fatal("two-player game must assign players to each other")
	}
}

func TestVictimPrivacyPersistenceAndCodeReset(t *testing.T) {
	s, repo, admission, owner, players := setup(t)
	ctx := context.Background()
	extra, err := s.AddPlayers(ctx, owner, []string{"Alice", "Unjoined"})
	if err != nil {
		t.Fatal(err)
	}
	players = append(players, extra[0]) // Duplicate names must still be distinct identities.
	joinPlayers(t, s, admission, players)
	if _, err = s.AddMissions(ctx, owner, []string{"Task A", "Task B", "Task C"}); err != nil {
		t.Fatal(err)
	}
	_, lobby, _ := s.Authorize(ctx, admission.Token)
	for _, p := range services.View(lobby, owner).Players {
		if p.Victim != nil {
			t.Fatal("victim revealed in lobby")
		}
	}
	if err = s.Transition(ctx, owner, "start"); err != nil {
		t.Fatal(err)
	}
	s = services.NewGameService(repo)
	_, g, _ := s.Authorize(ctx, admission.Token)
	view := services.View(g, owner)
	for i, p := range players {
		login, err := s.Login(ctx, admission.Game.InvitationCode, p.Code, "player")
		if err != nil {
			t.Fatal(err)
		}
		me := login.Game.Me
		if me.Victim == nil || me.Victim.ID == p.ID || *me.Victim != *view.Players[i].Victim {
			t.Fatal("private victim missing, changed, or self-targeted")
		}
		if login.Game.Players != nil || login.Game.Missions != nil {
			t.Fatal("player received others' assignments")
		}
		data, _ := json.Marshal(me.Victim)
		var fields map[string]any
		_ = json.Unmarshal(data, &fields)
		if len(fields) != 2 || fields["id"] == nil || fields["name"] == nil {
			t.Fatalf("victim exposes more than identity: %s", data)
		}
	}
	if view.Players[3].Victim != nil || g.Players[3].VictimID != "" {
		t.Fatal("unjoined invitee received a victim")
	}
	issued, err := s.ResetCode(ctx, owner, players[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	login, err := s.Login(ctx, admission.Game.InvitationCode, issued.Code, "player")
	if err != nil || *login.Game.Me.Victim != *view.Players[0].Victim {
		t.Fatalf("code reset changed victim: %v", err)
	}
	if err = s.Transition(ctx, owner, "end"); err != nil {
		t.Fatal(err)
	}
	a, ended, _ := s.Authorize(ctx, login.Token)
	if *services.View(ended, a).Me.Victim != *view.Players[0].Victim {
		t.Fatal("ending lost victim")
	}
}
