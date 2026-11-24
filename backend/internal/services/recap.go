package services

import (
	"sort"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

const endOrganizer = "organizer"

// RecapOf computes the post-game recap on demand from stored data. It is
// deterministic because an ended game no longer changes. It names no killers,
// missions, targets or team memberships; kill counts appear only through the
// same standings the result already exposes.
func RecapOf(g *models.Game) (*models.Recap, error) {
	if g.Status != "ended" {
		return nil, ErrResultNotReady
	}
	start := g.CreatedAt
	if g.StartedAt != nil {
		start = *g.StartedAt
	}
	end := start
	if g.EndedAt != nil {
		end = *g.EndedAt
	}
	secs := func(t time.Time) int {
		if d := int(t.Sub(start).Seconds()); d > 0 {
			return d
		}
		return 0
	}

	recap := &models.Recap{Timeline: []models.RecapEvent{}}
	sum := &recap.Summary
	sum.DurationSeconds = secs(end)

	if g.Result != nil {
		sum.Reason, sum.Tie = g.Result.Reason, g.Result.Tie
		recap.Leaderboard = append([]models.ResultStanding{}, g.Result.Standings...)
	} else {
		switch {
		case g.EndReason != "":
			sum.Reason = g.EndReason
		case g.WinnerID != "":
			sum.Reason = endSurvival
		default:
			sum.Reason = endOrganizer
		}
		if sum.Reason == endOrganizer {
			recap.Leaderboard = finalStandings(g)
		} else {
			r := buildResult(g, sum.Reason)
			sum.Tie, recap.Leaderboard = r.Tie, r.Standings
		}
	}

	survived := []models.RecapAward{}
	for _, p := range g.Players {
		if !p.Included {
			continue
		}
		sum.Players++
		switch {
		case p.EliminatedAt != nil:
			sum.Eliminations++
			recap.Timeline = append(recap.Timeline, models.RecapEvent{PlayerID: p.ID, Name: p.Name, Event: "eliminated", AtSeconds: secs(*p.EliminatedAt)})
			survived = append(survived, models.RecapAward{PlayerID: p.ID, Name: p.Name, AtSeconds: secs(*p.EliminatedAt)})
		case p.WithdrawnAt != nil:
			sum.Withdrawals++
			recap.Timeline = append(recap.Timeline, models.RecapEvent{PlayerID: p.ID, Name: p.Name, Event: "withdrew", AtSeconds: secs(*p.WithdrawnAt)})
		default:
			survived = append(survived, models.RecapAward{PlayerID: p.ID, Name: p.Name, AtSeconds: secs(end)})
		}
	}
	for _, e := range g.Eliminations {
		if e.Status == "declined" {
			sum.DeclinedReports++
		}
	}
	sort.SliceStable(recap.Timeline, func(i, j int) bool {
		a, b := recap.Timeline[i], recap.Timeline[j]
		if a.AtSeconds != b.AtSeconds {
			return a.AtSeconds < b.AtSeconds
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.PlayerID < b.PlayerID
	})

	awards := &recap.Awards
	awards.MostKills = []models.ResultStanding{}
	for _, st := range recap.Leaderboard {
		if st.Kills == 0 || st.Kills != recap.Leaderboard[0].Kills {
			break
		}
		awards.MostKills = append(awards.MostKills, st)
	}
	var elim []models.RecapEvent
	for _, ev := range recap.Timeline {
		if ev.Event == "eliminated" {
			elim = append(elim, ev)
		}
	}
	if len(elim) > 0 {
		first, last := elim[0], elim[len(elim)-1]
		awards.FirstElimination = &models.RecapAward{PlayerID: first.PlayerID, Name: first.Name, AtSeconds: first.AtSeconds}
		// The first elimination is also the fastest one after the start.
		fastest := *awards.FirstElimination
		awards.FastestElimination = &fastest
		awards.LastElimination = &models.RecapAward{PlayerID: last.PlayerID, Name: last.Name, AtSeconds: last.AtSeconds}
	}
	if len(survived) > 0 {
		sort.SliceStable(survived, func(i, j int) bool {
			a, b := survived[i], survived[j]
			if a.AtSeconds != b.AtSeconds {
				return a.AtSeconds > b.AtSeconds
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.PlayerID < b.PlayerID
		})
		awards.LongestSurvivor = &survived[0]
	}
	return recap, nil
}
