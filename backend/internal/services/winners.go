package services

import (
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

const (
	winnerPlayer = "player"
	winnerTeam   = "team"
	endSurvival  = "survival"
)

// settleAfterTransfer is the single post-transfer step shared by confirmed
// eliminations and confirmed withdrawals: first see whether the game is won,
// otherwise keep the team circle valid.
func settleAfterTransfer(g *models.Game, now time.Time) {
	if detectWinner(g, now) {
		return
	}
	// Team mode: never leave a hunter targeting a teammate. When no safe circle
	// exists detectWinner has already ended the game.
	_ = repairTeamCircle(g)
}

// detectWinner ends an active game when the survival rules are met and reports
// whether it did. Individual mode: the last active player wins. Team mode: the
// last team with active players wins, and so does a team holding an unbeatable
// majority, i.e. when no team-safe circle can exist for the remaining players
// (the same predicate as ValidAssignmentExists). Withdrawn and eliminated
// players are not active. It is idempotent: an ended game is left untouched, so
// two concurrent final events (serialized by mutate) end the game once.
func detectWinner(g *models.Game, now time.Time) bool {
	if g.Status != "active" {
		return false
	}
	var living []*models.Player
	var keys []string
	for i := range g.Players {
		if p := &g.Players[i]; stillPlaying(p) {
			living = append(living, p)
			keys = append(keys, circleKey(g, p))
		}
	}
	switch {
	case len(living) == 0:
		return false
	case len(living) == 1:
		// A team game is won by a team even when a single player is left; only
		// individual mode has a player winner. TeamID is always set for included
		// players in team mode (the start check requires it), so the player
		// fallback is just a guard for malformed data.
		if g.Mode == models.ModeTeam && living[0].TeamID != "" {
			finishGame(g, living, now, winnerTeam, living[0].TeamID)
		} else {
			finishGame(g, living, now, winnerPlayer, living[0].ID)
		}
		return true
	case g.Mode == models.ModeTeam && !feasibleCircle(keys):
		if team := dominantTeam(living); team != "" {
			finishGame(g, living, now, winnerTeam, team)
			return true
		}
	}
	return false
}

// dominantTeam returns the team holding more than half of the living players.
func dominantTeam(living []*models.Player) string {
	counts := map[string]int{}
	for _, p := range living {
		if p.TeamID != "" {
			counts[p.TeamID]++
		}
	}
	for team, n := range counts {
		if n*2 > len(living) {
			return team
		}
	}
	return ""
}

// finishGame records the survival result. The game is over, so nobody keeps a
// private victim or mission.
func finishGame(g *models.Game, living []*models.Player, now time.Time, kind, id string) {
	for _, p := range living {
		p.VictimID, p.Mission = "", ""
	}
	g.Status, g.EndedAt, g.EndReason = "ended", &now, endSurvival
	g.WinnerKind, g.WinnerID = kind, id
	setResult(g, endSurvival)
}

// winnerView is the public winner payload: kind, id and name only, and only
// once the game has ended. It never lists members, assignments or counts.
func winnerView(g *models.Game) *models.WinnerView {
	if g.Status != "ended" || g.WinnerID == "" {
		return nil
	}
	if g.WinnerKind == winnerTeam {
		for _, t := range g.Teams {
			if t.ID == g.WinnerID {
				return &models.WinnerView{Kind: winnerTeam, ID: t.ID, Name: t.Name}
			}
		}
		return nil
	}
	for _, p := range g.Players {
		if p.ID == g.WinnerID {
			return &models.WinnerView{Kind: winnerPlayer, ID: p.ID, Name: p.Name}
		}
	}
	return nil
}
