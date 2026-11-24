package services

import (
	"errors"
	"sort"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

var (
	// ErrResultNotReady means the game has not ended yet (HTTP 409).
	ErrResultNotReady = errors.New("the result is only available after the game has ended")
	// ErrNoResult means the game ended without a calculated result, for example
	// when the organizer ended it (HTTP 404).
	ErrNoResult = errors.New("this game ended without a result")
)

const endTimer = "timer"

// setResult calculates and stores the result exactly once. A stored result is
// never recalculated or overwritten, so it is immutable and the call is
// idempotent. Callers run it inside the game mutation that ends the game, so a
// concurrent second ending is rejected by the revision check.
func setResult(g *models.Game, reason string) {
	if g.Result != nil {
		return
	}
	g.Result = buildResult(g, reason)
}

// OnTimerExpired fills the seam left by the timer ticket: it runs inside the
// expiry mutation, exactly once per game, after the game is "ended" with
// EndReason "timer". The individual with the most confirmed kills, or the team
// with the highest combined kills, wins; shared top scores are a tie.
func (s *GameService) OnTimerExpired(g *models.Game) { setResult(g, endTimer) }

// ResultOf returns the final result of an ended game. Games that ended by
// survival before results existed have none stored; theirs is derived from the
// recorded winner, which is deterministic because the game can no longer change.
func ResultOf(g *models.Game) (*models.GameResult, error) {
	if g.Status != "ended" {
		return nil, ErrResultNotReady
	}
	switch {
	case g.Result != nil:
		return g.Result, nil
	case g.WinnerID != "":
		return buildResult(g, endSurvival), nil
	}
	return nil, ErrNoResult
}

// buildResult computes winners and final standings. Kills earned by players who
// were later eliminated or withdrew still count; withdrawals never earn kills.
func buildResult(g *models.Game, reason string) *models.GameResult {
	result := &models.GameResult{Reason: reason, Winners: []models.ResultWinner{}, Standings: finalStandings(g)}
	switch reason {
	case endSurvival:
		if w := winnerView(g); w != nil {
			result.Winners = append(result.Winners, models.ResultWinner{Kind: w.Kind, ID: w.ID, Name: w.Name})
		}
	case endTimer:
		// Standings are sorted by kills, so the winners are the leading entries
		// that share the top score. Nobody is picked silently when they tie.
		for _, st := range result.Standings {
			if st.Kills != result.Standings[0].Kills {
				break
			}
			result.Winners = append(result.Winners, models.ResultWinner{Kind: st.Kind, ID: st.ID, Name: st.Name})
		}
	}
	result.Tie = len(result.Winners) > 1
	return result
}

// finalStandings lists every team (team mode) or every participant (individual
// mode) with its confirmed kills, most kills first. Ties are ordered by name and
// ID only, which reveals nothing about who killed whom.
func finalStandings(g *models.Game) []models.ResultStanding {
	standings := []models.ResultStanding{}
	if g.Mode == models.ModeTeam {
		totals := TeamKillTotals(g)
		members := map[string]bool{}
		for _, p := range g.Players {
			if p.Included && p.TeamID != "" {
				members[p.TeamID] = true
			}
		}
		for _, t := range g.Teams {
			if members[t.ID] {
				standings = append(standings, models.ResultStanding{Kind: winnerTeam, ID: t.ID, Name: t.Name, Kills: totals[t.ID]})
			}
		}
	} else {
		totals := KillTotals(g)
		for _, p := range g.Players {
			if p.Included {
				standings = append(standings, models.ResultStanding{Kind: winnerPlayer, ID: p.ID, Name: p.Name, Kills: totals[p.ID]})
			}
		}
	}
	sort.SliceStable(standings, func(i, j int) bool {
		a, b := standings[i], standings[j]
		if a.Kills != b.Kills {
			return a.Kills > b.Kills
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	return standings
}
