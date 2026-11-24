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

// teamGameRepo is eliminationGame turned into a team game (see teamGame).
func teamGameRepo(t *testing.T, teams []string) (*services.GameService, *testutil.Memory, []models.Access) {
	t.Helper()
	ctx := context.Background()
	s, repo, a := eliminationGame(t, len(teams))
	g, _ := repo.Get(ctx, a[0].GameID)
	g.Mode = models.ModeTeam
	seen := map[string]bool{}
	for i, team := range teams {
		g.Players[i].TeamID = team
		if !seen[team] {
			seen[team] = true
			g.Teams = append(g.Teams, models.Team{ID: team, Name: "Team " + team})
		}
	}
	repo.Games[g.ID] = g
	return s, repo, a
}

// expire makes the game's timer due and lets the service expire it lazily.
func expire(t *testing.T, s *services.GameService, repo *testutil.Memory, id string) *models.Game {
	t.Helper()
	ctx := context.Background()
	g, _ := repo.Get(ctx, id)
	ends := time.Now().Add(-time.Minute)
	g.Timer = &models.Timer{DurationMinutes: 10, EndsAt: &ends}
	repo.Games[id] = g
	g, _ = repo.Get(ctx, id)
	expired, err := s.ExpireDue(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if expired.Status != "ended" || expired.EndReason != "timer" {
		t.Fatalf("game did not expire: %s/%s", expired.Status, expired.EndReason)
	}
	return expired
}

func winnerNames(entries []models.ResultWinner) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func killsOf(r *models.GameResult) map[string]int {
	out := map[string]int{}
	for _, st := range r.Standings {
		out[st.Name] = st.Kills
	}
	return out
}

func TestTimerExpiryIndividualMostKillsWinsAndEliminatedKillsCount(t *testing.T) {
	s, repo, a := eliminationGame(t, 4) // 0 -> 1 -> 2 -> 3 -> 0
	confirmKill(t, s, a[0], a[1], "1")  // player 0 kills 1: 0 has one kill
	confirmKill(t, s, a[3], a[0], "0")  // player 3 kills 0: 3 has one kill, 0 is out
	g := expire(t, s, repo, a[0].GameID)
	if g.Result == nil || g.Result.Reason != "timer" {
		t.Fatalf("no timer result stored: %+v", g.Result)
	}
	// Player 0 was eliminated but the kill they earned still counts, so they
	// tie with player 3 for the lead.
	if got := winnerNames(g.Result.Winners); len(got) != 2 || got[0] != "Player 0" || got[1] != "Player 3" || !g.Result.Tie {
		t.Fatalf("winners = %v tie=%v", got, g.Result.Tie)
	}
	want := map[string]int{"Player 0": 1, "Player 3": 1, "Player 1": 0, "Player 2": 0}
	for name, kills := range killsOf(g.Result) {
		if want[name] != kills {
			t.Fatalf("%s has %d kills, want %d", name, kills, want[name])
		}
	}
	if len(g.Result.Standings) != 4 {
		t.Fatalf("standings must list every participant: %+v", g.Result.Standings)
	}
	order := []string{"Player 0", "Player 3", "Player 1", "Player 2"} // kills desc, then name
	for i, st := range g.Result.Standings {
		if st.Name != order[i] || st.Kind != "player" {
			t.Fatalf("standings order wrong: %+v", g.Result.Standings)
		}
	}
}

func TestTimerExpirySingleLeaderAndWithdrawalsAreNotKills(t *testing.T) {
	s, repo, a := eliminationGame(t, 4)
	confirmKill(t, s, a[0], a[1], "1") // the only kill
	withdraw(t, s, a[2])               // a withdrawal credits nobody
	g := expire(t, s, repo, a[0].GameID)
	if got := winnerNames(g.Result.Winners); len(got) != 1 || got[0] != "Player 0" || g.Result.Tie {
		t.Fatalf("winners = %v tie=%v", got, g.Result.Tie)
	}
	total := 0
	for _, kills := range killsOf(g.Result) {
		total += kills
	}
	if total != 1 || killsOf(g.Result)["Player 2"] != 0 || killsOf(g.Result)["Player 3"] != 0 {
		t.Fatalf("withdrawal changed kill totals: %+v", g.Result.Standings)
	}
}

func TestTimerExpiryWithNoKillsIsATieOfEveryone(t *testing.T) {
	s, repo, a := eliminationGame(t, 3)
	g := expire(t, s, repo, a[0].GameID)
	if got := winnerNames(g.Result.Winners); len(got) != 3 || !g.Result.Tie {
		t.Fatalf("a zero-kill game must be a tie: winners=%v tie=%v", got, g.Result.Tie)
	}
	for _, st := range g.Result.Standings {
		if st.Kills != 0 {
			t.Fatalf("unexpected kills: %+v", g.Result.Standings)
		}
	}
}

func TestTimerExpiryTeamsUseCombinedKillsIncludingEliminatedKillers(t *testing.T) {
	s, repo, a := teamGameRepo(t, []string{"A", "B", "C", "A", "B", "C"})
	confirmKill(t, s, a[3], a[4], "4") // player 3 (team A) kills player 4: A = 1
	confirmKill(t, s, a[2], a[3], "3") // player 2 (team C) kills player 3: C = 1, A's kill stays
	g := expire(t, s, repo, a[0].GameID)
	if g.Result.Reason != "timer" || !g.Result.Tie || len(g.Result.Winners) != 2 {
		t.Fatalf("expected a two-team tie: %+v", g.Result)
	}
	if g.Result.Winners[0].Kind != "team" || g.Result.Winners[0].Name != "Team A" || g.Result.Winners[1].Name != "Team C" {
		t.Fatalf("winners = %+v", g.Result.Winners)
	}
	want := map[string]int{"Team A": 1, "Team C": 1, "Team B": 0}
	if len(g.Result.Standings) != 3 {
		t.Fatalf("team games list teams, not players: %+v", g.Result.Standings)
	}
	for name, kills := range killsOf(g.Result) {
		if want[name] != kills {
			t.Fatalf("%s has %d kills, want %d", name, kills, want[name])
		}
	}
	for _, st := range g.Result.Standings {
		if st.Kind != "team" {
			t.Fatalf("team standings must be teams: %+v", st)
		}
	}
}

func TestSurvivalResultIndividualAndTeam(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 3)
	confirmKill(t, s, a[0], a[1], "1")
	confirmKill(t, s, a[0], a[2], "2")
	g, _ := repo.Get(ctx, a[0].GameID)
	result, err := services.ResultOf(g)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "survival" || result.Tie || len(result.Winners) != 1 || result.Winners[0] != (models.ResultWinner{Kind: "player", ID: "0", Name: "Player 0"}) {
		t.Fatalf("survival result = %+v", result)
	}
	if killsOf(result)["Player 0"] != 2 || killsOf(result)["Player 1"] != 0 {
		t.Fatalf("standings = %+v", result.Standings)
	}

	// A team that wins by majority is a team winner with team standings.
	ts, trepo, ta := teamGameRepo(t, []string{"A", "B", "A", "B"})
	confirmKill(t, ts, ta[0], ta[1], "1")
	tg, _ := trepo.Get(ctx, ta[0].GameID)
	tr, err := services.ResultOf(tg)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Reason != "survival" || len(tr.Winners) != 1 || tr.Winners[0] != (models.ResultWinner{Kind: "team", ID: "A", Name: "Team A"}) {
		t.Fatalf("team survival result = %+v", tr)
	}
	if got := killsOf(tr); got["Team A"] != 1 || got["Team B"] != 0 || len(tr.Standings) != 2 {
		t.Fatalf("team standings = %+v", tr.Standings)
	}
}

func TestResultAvailabilityOnlyAfterTheEnd(t *testing.T) {
	ctx := context.Background()
	_, repo, a := eliminationGame(t, 3)
	g, _ := repo.Get(ctx, a[0].GameID)
	if _, err := services.ResultOf(g); !errors.Is(err, services.ErrResultNotReady) {
		t.Fatalf("active game: %v", err)
	}
	// The organizer ending the game is neither survival nor timer: no result.
	g.Status = "ended"
	if _, err := services.ResultOf(g); !errors.Is(err, services.ErrNoResult) {
		t.Fatalf("organizer-ended game: %v", err)
	}
	// A game that ended by survival before results existed still has an answer.
	g.WinnerID, g.WinnerKind = "1", "player"
	legacy, err := services.ResultOf(g)
	if err != nil || legacy.Reason != "survival" || len(legacy.Winners) != 1 || legacy.Winners[0].ID != "1" {
		t.Fatalf("legacy survival game: %+v %v", legacy, err)
	}
}

func TestResultIsCalculatedOnceAndIsImmutable(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 3)
	confirmKill(t, s, a[0], a[1], "1")
	first := expire(t, s, repo, a[0].GameID)
	snapshot, _ := json.Marshal(first.Result)
	endedAt := *first.EndedAt
	// Hammer the expiry path with many concurrent readers.
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, _ := repo.Get(ctx, a[0].GameID)
			if _, err := s.ExpireDue(ctx, g); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	g, _ := repo.Get(ctx, a[0].GameID)
	again, _ := json.Marshal(g.Result)
	if string(again) != string(snapshot) || !g.EndedAt.Equal(endedAt) || g.EndReason != "timer" {
		t.Fatalf("result changed after it was set:\n%s\n%s", snapshot, again)
	}
	// Even if a kill count changed afterwards, the stored result is never recalculated.
	g.Players[2].ConfirmedKills = 99
	repo.Games[g.ID] = g
	g, _ = repo.Get(ctx, a[0].GameID)
	if _, err := s.ExpireDue(ctx, g); err != nil {
		t.Fatal(err)
	}
	g, _ = repo.Get(ctx, a[0].GameID)
	final, _ := json.Marshal(g.Result)
	if string(final) != string(snapshot) {
		t.Fatalf("result was recalculated:\n%s\n%s", snapshot, final)
	}
}

func TestConcurrentSurvivalEndStoresOneResult(t *testing.T) {
	ctx := context.Background()
	for round := 0; round < 25; round++ {
		s, repo, a := teamGameRepo(t, []string{"A", "B", "A", "B"})
		tokens := map[int]string{}
		for _, i := range []int{1, 3} {
			request, err := s.RequestWithdrawal(ctx, a[i])
			if err != nil {
				t.Fatal(err)
			}
			tokens[i] = request.Token
		}
		var wg sync.WaitGroup
		for _, i := range []int{1, 3} {
			wg.Add(1)
			go func() { defer wg.Done(); _ = s.ConfirmWithdrawal(ctx, a[i], tokens[i]) }()
		}
		wg.Wait()
		g, _ := repo.Get(ctx, a[0].GameID)
		if g.Result == nil || g.Result.Reason != "survival" || len(g.Result.Winners) != 1 || g.Result.Winners[0].ID != "A" || g.Result.Tie {
			t.Fatalf("round %d: result = %+v", round, g.Result)
		}
		for _, kills := range killsOf(g.Result) {
			if kills != 0 {
				t.Fatalf("round %d: withdrawals must not earn kills: %+v", round, g.Result.Standings)
			}
		}
	}
}

func TestKillCountsNeverLeakMidGameAndResultHasOnlyAgreedFields(t *testing.T) {
	ctx := context.Background()
	s, repo, a := eliminationGame(t, 4)
	organizer := models.Access{GameID: a[0].GameID}
	confirmKill(t, s, a[0], a[1], "1") // a kill exists, so a leak would be visible
	g, _ := repo.Get(ctx, a[0].GameID)
	leak := func(g *models.Game, label string) {
		for _, actor := range append(append([]models.Access{}, a...), organizer) {
			data, _ := json.Marshal(services.View(g, actor))
			text := strings.ToLower(string(data))
			if strings.Contains(text, "kills") {
				t.Fatalf("%s view leaks kill counts: %s", label, data)
			}
		}
	}
	leak(g, "mid-game")

	ended := expire(t, s, repo, a[0].GameID)
	leak(ended, "post-game") // the game view still never carries kill counts
	result, err := services.ResultOf(ended)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if len(raw) != 4 || raw["reason"] == nil || raw["tie"] == nil || raw["winners"] == nil || raw["standings"] == nil {
		t.Fatalf("unexpected result fields: %s", data)
	}
	for _, w := range raw["winners"].([]any) {
		if len(w.(map[string]any)) != 3 {
			t.Fatalf("winner has extra fields: %s", data)
		}
	}
	for _, st := range raw["standings"].([]any) {
		fields := st.(map[string]any)
		if len(fields) != 4 || fields["kills"] == nil {
			t.Fatalf("standing has unexpected fields: %s", data)
		}
	}
	for _, banned := range []string{"victim", "mission", "killer", "members", "playerids", "eliminat", "withdraw"} {
		if strings.Contains(strings.ToLower(string(data)), banned) {
			t.Fatalf("result exposes %q: %s", banned, data)
		}
	}
}
