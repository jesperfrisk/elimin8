package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func withdraw(t *testing.T, s *services.GameService, a models.Access) {
	t.Helper()
	ctx := context.Background()
	request, err := s.RequestWithdrawal(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmWithdrawal(ctx, a, request.Token); err != nil {
		t.Fatal(err)
	}
}

// assertCircle checks that the living players form one unbroken circle: every
// living player has exactly one living victim and exactly one hunter, and
// nobody targets themselves.
func assertCircle(t *testing.T, g *models.Game) {
	t.Helper()
	living, hunters := 0, map[string]int{}
	for _, p := range g.Players {
		if p.EliminatedAt != nil || p.WithdrawnAt != nil {
			if p.VictimID != "" || p.Mission != "" {
				t.Fatalf("%s left the game but kept an assignment", p.ID)
			}
			continue
		}
		living++
		if g.Status == "ended" {
			continue
		}
		if p.VictimID == "" || p.VictimID == p.ID {
			t.Fatalf("%s has invalid victim %q", p.ID, p.VictimID)
		}
		hunters[p.VictimID]++
	}
	if g.Status == "ended" {
		return
	}
	if len(hunters) != living {
		t.Fatalf("circle broken: %d living players, %d distinct victims", living, len(hunters))
	}
	for _, p := range g.Players {
		if p.EliminatedAt != nil || p.WithdrawnAt != nil {
			if hunters[p.ID] != 0 {
				t.Fatalf("%s is still someone's victim", p.ID)
			}
		} else if hunters[p.ID] != 1 {
			t.Fatalf("%s has %d hunters", p.ID, hunters[p.ID])
		}
	}
}

func TestWithdrawalTransfersAssignmentsWithoutAKill(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4) // 0 -> 1 -> 2 -> 3 -> 0
	notifications := 0
	s.Notify = func() { notifications++ }
	before, _ := repo.Get(ctx, a[0].GameID)

	request, err := s.RequestWithdrawal(ctx, a[1])
	if err != nil {
		t.Fatal(err)
	}
	if request.Token == "" || !request.ExpiresAt.After(time.Now()) {
		t.Fatalf("bad request: %+v", request)
	}
	// An unconfirmed request changes nothing about the game or its players.
	g, _ := repo.Get(ctx, a[0].GameID)
	for i := range g.Players {
		if g.Players[i].VictimID != before.Players[i].VictimID || g.Players[i].WithdrawnAt != nil {
			t.Fatal("unconfirmed request changed assignments")
		}
	}
	if g.Status != "active" {
		t.Fatal("unconfirmed request changed status")
	}

	if err = s.ConfirmWithdrawal(ctx, a[1], request.Token); err != nil {
		t.Fatal(err)
	}
	g, _ = repo.Get(ctx, a[0].GameID)
	if g.Players[1].WithdrawnAt == nil || g.Players[1].EliminatedAt != nil {
		t.Fatal("player was not marked withdrawn")
	}
	if g.Players[0].VictimID != "2" || g.Players[0].Mission != before.Players[1].Mission {
		t.Fatalf("hunter did not inherit victim and mission: %+v", g.Players[0])
	}
	for _, p := range g.Players {
		if p.ConfirmedKills != 0 {
			t.Fatal("withdrawal credited a kill")
		}
	}
	if len(g.Withdrawals) != 0 || g.Status != "active" || g.WinnerID != "" {
		t.Fatalf("unexpected game state: %+v", g)
	}
	assertCircle(t, g)
	if notifications < 2 {
		t.Fatalf("expected realtime notifications, got %d", notifications)
	}

	// Status is public; assignments stay private; no withdrawal timestamps leak.
	for _, actor := range append(a, models.Access{}) {
		view := services.View(g, actor)
		data, _ := json.Marshal(view)
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		if string(data) == "" || len(view.Participants) != 4 {
			t.Fatal("participants missing")
		}
		statuses := map[string]string{}
		for _, p := range view.Participants {
			statuses[p.ID] = p.Status
			encoded, _ := json.Marshal(p)
			var fields map[string]any
			_ = json.Unmarshal(encoded, &fields)
			if len(fields) != 3 || fields["id"] == nil || fields["name"] == nil || fields["status"] == nil {
				t.Fatalf("participant leaks fields: %s", encoded)
			}
		}
		want := map[string]string{"0": "active", "1": "withdrawn", "2": "active", "3": "active"}
		for id, status := range want {
			if statuses[id] != status {
				t.Fatalf("status of %s = %q, want %q", id, statuses[id], status)
			}
		}
		if _, leaked := raw["withdrawals"]; leaked {
			t.Fatal("pending withdrawals leaked")
		}
		if actor.PlayerID == "" {
			for _, p := range view.Players {
				if p.ID == "1" && (p.Victim != nil || p.Mission != "" || p.Status != "withdrawn") {
					t.Fatalf("withdrawn player view wrong: %+v", p)
				}
			}
		}
	}
	me := services.View(g, a[1]).Me
	if me == nil || me.Status != "withdrawn" || me.Victim != nil || me.Mission != "" {
		t.Fatalf("withdrawn player still sees assignments: %+v", me)
	}

	// A withdrawn player cannot act any more.
	if _, err = s.RequestWithdrawal(ctx, a[1]); !errors.Is(err, services.ErrWithdrawal) {
		t.Fatalf("withdrawn player requested again: %v", err)
	}
	if _, err = s.ReportElimination(ctx, a[1], "2"); !errors.Is(err, services.ErrElimination) {
		t.Fatalf("withdrawn player reported a kill: %v", err)
	}
	if _, err = s.ReportElimination(ctx, a[0], "1"); !errors.Is(err, services.ErrElimination) {
		t.Fatalf("withdrawn player is still a valid target: %v", err)
	}
	// The same token cannot be replayed.
	if err = s.ConfirmWithdrawal(ctx, a[1], request.Token); !errors.Is(err, services.ErrWithdrawal) {
		t.Fatalf("token replay accepted: %v", err)
	}
}

func TestWithdrawalRejections(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	organizer := models.Access{GameID: a[0].GameID}
	if _, err := s.RequestWithdrawal(ctx, organizer); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("organizer requested: %v", err)
	}
	if err := s.ConfirmWithdrawal(ctx, organizer, "x"); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("organizer confirmed: %v", err)
	}

	request, err := s.RequestWithdrawal(ctx, a[1])
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmWithdrawal(ctx, a[1], ""); !errors.Is(err, services.ErrInvalid) {
		t.Fatalf("empty token: %v", err)
	}
	if err = s.ConfirmWithdrawal(ctx, a[1], "WRONG"); !errors.Is(err, services.ErrWithdrawal) {
		t.Fatalf("wrong token: %v", err)
	}
	// Another player cannot use someone else's token.
	if err = s.ConfirmWithdrawal(ctx, a[2], request.Token); !errors.Is(err, services.ErrWithdrawal) {
		t.Fatalf("token used by another player: %v", err)
	}
	// Re-requesting replaces the earlier token.
	second, err := s.RequestWithdrawal(ctx, a[1])
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmWithdrawal(ctx, a[1], request.Token); !errors.Is(err, services.ErrWithdrawal) {
		t.Fatalf("superseded token accepted: %v", err)
	}
	// An expired request is dropped, not honoured.
	stored, _ := repo.Get(ctx, a[0].GameID)
	stored.Withdrawals[0].ExpiresAt = time.Now().Add(-time.Second)
	repo.Games[stored.ID] = stored
	if err = s.ConfirmWithdrawal(ctx, a[1], second.Token); !errors.Is(err, services.ErrWithdrawal) {
		t.Fatalf("expired token accepted: %v", err)
	}
	g, _ := repo.Get(ctx, a[0].GameID)
	for _, p := range g.Players {
		if p.WithdrawnAt != nil {
			t.Fatal("a rejected confirmation withdrew someone")
		}
	}

	// Lobby and ended games reject withdrawals.
	lobby, _ := repo.Get(ctx, a[0].GameID)
	lobby.Status = "lobby"
	repo.Games[lobby.ID] = lobby
	if _, err = s.RequestWithdrawal(ctx, a[2]); !errors.Is(err, services.ErrWithdrawal) {
		t.Fatalf("lobby request accepted: %v", err)
	}
	ended, _ := repo.Get(ctx, a[0].GameID)
	ended.Status = "ended"
	repo.Games[ended.ID] = ended
	if _, err = s.RequestWithdrawal(ctx, a[2]); !errors.Is(err, services.ErrWithdrawal) {
		t.Fatalf("ended request accepted: %v", err)
	}
}

func TestWithdrawalLeavingOneSurvivorEndsTheGame(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 2)
	withdraw(t, s, a[1])
	g, _ := repo.Get(ctx, a[0].GameID)
	if g.Status != "ended" || g.WinnerID != "0" || g.EndedAt == nil {
		t.Fatalf("last survivor did not win: status=%s winner=%q", g.Status, g.WinnerID)
	}
	if g.Players[0].VictimID != "" || g.Players[1].WithdrawnAt == nil || g.Players[1].EliminatedAt != nil {
		t.Fatalf("unexpected end state: %+v", g.Players)
	}
	if services.View(g, a[0]).WinnerID != "0" {
		t.Fatal("winner not visible")
	}
}

func TestWithdrawalCancelsPendingEliminationClaims(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	claim, err := s.ReportElimination(ctx, a[0], "1")
	if err != nil {
		t.Fatal(err)
	}
	withdraw(t, s, a[1])
	if err = s.RespondElimination(ctx, a[1], claim.ID, true); !errors.Is(err, services.ErrElimination) {
		t.Fatalf("withdrawn victim could still accept: %v", err)
	}
	g, _ := repo.Get(ctx, a[0].GameID)
	if g.Eliminations[0].Status != "cancelled" || g.Players[1].EliminatedAt != nil {
		t.Fatalf("claim not cancelled: %+v", g.Eliminations[0])
	}
	assertCircle(t, g)
}

func TestWithdrawalAndEliminationOfTheSamePlayerRace(t *testing.T) {
	ctx := context.Background()
	for round := 0; round < 25; round++ {
		s, repo, a := eliminationGame(t, 4)
		claim, err := s.ReportElimination(ctx, a[0], "1")
		if err != nil {
			t.Fatal(err)
		}
		request, err := s.RequestWithdrawal(ctx, a[1])
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var eliminationErr, withdrawalErr error
		wg.Add(2)
		go func() { defer wg.Done(); eliminationErr = s.RespondElimination(ctx, a[1], claim.ID, true) }()
		go func() { defer wg.Done(); withdrawalErr = s.ConfirmWithdrawal(ctx, a[1], request.Token) }()
		wg.Wait()
		// Exactly one of the two wins; the other is rejected on reload.
		if (eliminationErr == nil) == (withdrawalErr == nil) {
			t.Fatalf("round %d: elimination=%v withdrawal=%v", round, eliminationErr, withdrawalErr)
		}
		if eliminationErr != nil && !errors.Is(eliminationErr, services.ErrElimination) {
			t.Fatalf("unexpected elimination error: %v", eliminationErr)
		}
		if withdrawalErr != nil && !errors.Is(withdrawalErr, services.ErrWithdrawal) {
			t.Fatalf("unexpected withdrawal error: %v", withdrawalErr)
		}
		g, _ := repo.Get(ctx, a[0].GameID)
		p := g.Players[1]
		if (p.EliminatedAt == nil) == (p.WithdrawnAt == nil) {
			t.Fatalf("round %d: player must be eliminated xor withdrawn: %+v", round, p)
		}
		assertCircle(t, g)
	}
}

func TestConcurrentWithdrawalsKeepTheCircleValid(t *testing.T) {
	ctx := context.Background()
	// Two adjacent players withdraw at once from a four-player circle.
	s, repo, a := eliminationGame(t, 4)
	tokens := make([]string, 2)
	for i := range tokens {
		request, err := s.RequestWithdrawal(ctx, a[i])
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = request.Token
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range tokens {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = s.ConfirmWithdrawal(ctx, a[i], tokens[i]) }()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("both withdrawals should succeed: %v %v", errs[0], errs[1])
	}
	g, _ := repo.Get(ctx, a[0].GameID)
	if g.Status != "active" || g.Players[2].VictimID != "3" || g.Players[3].VictimID != "2" {
		t.Fatalf("unexpected circle after two withdrawals: %+v", g.Players)
	}
	assertCircle(t, g)

	// Both players of a two-player game withdraw at once: the game ends once.
	s, repo, a = eliminationGame(t, 2)
	for i := range tokens {
		request, err := s.RequestWithdrawal(ctx, a[i])
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = request.Token
	}
	for i := range tokens {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = s.ConfirmWithdrawal(ctx, a[i], tokens[i]) }()
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("exactly one withdrawal should succeed: %v %v", errs[0], errs[1])
	}
	g, _ = repo.Get(ctx, a[0].GameID)
	withdrawn := 0
	for _, p := range g.Players {
		if p.WithdrawnAt != nil {
			withdrawn++
		}
	}
	if g.Status != "ended" || withdrawn != 1 || g.WinnerID == "" {
		t.Fatalf("game should end once with one winner: status=%s withdrawn=%d winner=%q", g.Status, withdrawn, g.WinnerID)
	}
}

func TestWithdrawalInTeamModeRepairsTeammateTargets(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 5) // 0 -> 1 -> 2 -> 3 -> 4 -> 0
	g, _ := repo.Get(ctx, a[0].GameID)
	g.Mode = models.ModeTeam
	for i, team := range []string{"A", "B", "A", "B", "B"} {
		g.Players[i].TeamID = team
	}
	repo.Games[g.ID] = g
	// Player 1 leaving would hand player 0 a teammate (player 2) as victim.
	withdraw(t, s, a[1])
	g, _ = repo.Get(ctx, a[0].GameID)
	assertCircle(t, g)
	teams := map[string]string{}
	for _, p := range g.Players {
		teams[p.ID] = p.TeamID
	}
	for _, p := range g.Players {
		if p.WithdrawnAt == nil && p.VictimID != "" && teams[p.VictimID] == p.TeamID {
			t.Fatalf("player %s targets teammate %s after withdrawal", p.ID, p.VictimID)
		}
	}
}
