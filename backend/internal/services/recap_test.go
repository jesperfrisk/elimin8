package services_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func recapGame() *models.Game {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(s int) *time.Time { t := start.Add(time.Duration(s) * time.Second); return &t }
	end := *at(600)
	return &models.Game{
		ID: "g", Status: "ended", StartedAt: &start, EndedAt: &end, EndReason: "timer",
		Players: []models.Player{
			{ID: "a", Name: "Alice", Included: true, VictimID: "SECRETV", Mission: "SECRETM", ConfirmedKills: 2},
			{ID: "b", Name: "Bob", Included: true, EliminatedAt: at(60)},
			{ID: "c", Name: "Cara", Included: true, EliminatedAt: at(300)},
			{ID: "d", Name: "Dan", Included: true, WithdrawnAt: at(120)},
			{ID: "e", Name: "Eve", Included: true},
			{ID: "x", Name: "Notjoined"},
		},
		Eliminations: []models.Elimination{
			{ID: "1", KillerID: "KILLERID", VictimID: "b", Status: "accepted"},
			{ID: "2", KillerID: "KILLERID", VictimID: "e", Status: "declined"},
		},
	}
}

func TestRecapNotReady(t *testing.T) {
	g := recapGame()
	g.Status = "active"
	if _, err := services.RecapOf(g); err != services.ErrResultNotReady {
		t.Fatalf("err = %v", err)
	}
}

func TestRecapIndividualTimerGame(t *testing.T) {
	r, err := services.RecapOf(recapGame())
	if err != nil {
		t.Fatal(err)
	}
	s := r.Summary
	if s.DurationSeconds != 600 || s.Players != 5 || s.Eliminations != 2 || s.Withdrawals != 1 || s.DeclinedReports != 1 || s.Reason != "timer" {
		t.Fatalf("summary = %+v", s)
	}
	if len(r.Timeline) != 3 || r.Timeline[0].Name != "Bob" || r.Timeline[1].Event != "withdrew" || r.Timeline[2].Name != "Cara" {
		t.Fatalf("timeline = %+v", r.Timeline)
	}
	if len(r.Leaderboard) != 5 || r.Leaderboard[0].Name != "Alice" || r.Leaderboard[0].Kills != 2 {
		t.Fatalf("leaderboard = %+v", r.Leaderboard)
	}
	a := r.Awards
	if a.FirstElimination.Name != "Bob" || a.FastestElimination.AtSeconds != 60 || a.LastElimination.Name != "Cara" || a.LastElimination.AtSeconds != 300 {
		t.Fatalf("awards = %+v", a)
	}
	if a.LongestSurvivor.Name != "Alice" || a.LongestSurvivor.AtSeconds != 600 {
		t.Fatalf("longest = %+v", a.LongestSurvivor)
	}
	if len(a.MostKills) != 1 || a.MostKills[0].Name != "Alice" {
		t.Fatalf("mostKills = %+v", a.MostKills)
	}

	raw, _ := json.Marshal(r)
	for _, secret := range []string{"SECRETV", "SECRETM", "KILLERID", "killer", "victim", "mission", "team"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(secret)) {
			t.Fatalf("recap leaks %q: %s", secret, raw)
		}
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	if len(top) != 4 {
		t.Fatalf("top-level keys = %v", top)
	}
	var sum map[string]any
	_ = json.Unmarshal(top["summary"], &sum)
	if len(sum) != 7 {
		t.Fatalf("summary keys = %v", sum)
	}
}

func TestRecapOrganizerEndedZeroKills(t *testing.T) {
	g := recapGame()
	g.EndReason = ""
	g.Players[0].ConfirmedKills = 0
	r, err := services.RecapOf(g)
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.Reason != "organizer" || r.Summary.Tie || len(r.Awards.MostKills) != 0 {
		t.Fatalf("recap = %+v", r)
	}
}

func TestRecapNoEliminationsAndTeamTie(t *testing.T) {
	g := recapGame()
	g.Mode = models.ModeTeam
	g.Teams = []models.Team{{ID: "t1", Name: "Red"}, {ID: "t2", Name: "Blue"}}
	for i, team := range []string{"t1", "t2", "t2", "t2", "t1"} {
		g.Players[i].TeamID = team
		g.Players[i].EliminatedAt, g.Players[i].WithdrawnAt, g.Players[i].ConfirmedKills = nil, nil, 0
	}
	g.Result = &models.GameResult{Reason: "timer", Tie: true, Standings: []models.ResultStanding{
		{Kind: "team", ID: "t2", Name: "Blue", Kills: 1}, {Kind: "team", ID: "t1", Name: "Red", Kills: 1},
	}}
	r, err := services.RecapOf(g)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Summary.Tie || len(r.Awards.MostKills) != 2 || r.Awards.FirstElimination != nil || len(r.Timeline) != 0 {
		t.Fatalf("recap = %+v", r)
	}
	raw, _ := json.Marshal(r)
	if !strings.Contains(string(raw), `"firstElimination":null`) || !strings.Contains(string(raw), `"timeline":[]`) {
		t.Fatalf("json = %s", raw)
	}
}
