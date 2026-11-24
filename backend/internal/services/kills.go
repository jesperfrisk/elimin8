package services

import "github.com/jesperfrisk/elimin8/backend/internal/models"

// Confirmed kills are private: they are only incremented in the same revisioned
// write as the elimination (see transferElimination) and read by the results
// calculation. They must never appear in any view or API response. Declined,
// expired, cancelled and duplicate claims never reach recordKill, withdrawals
// never call it, and kills stay counted after the killer is eliminated.

// recordKill credits the killer with one confirmed kill.
func recordKill(killer *models.Player) { killer.ConfirmedKills++ }

// KillTotals returns confirmed kills per player ID for every included player
// (zero for players with none, including games created before the field existed).
func KillTotals(g *models.Game) map[string]int {
	totals := make(map[string]int, len(g.Players))
	for _, p := range g.Players {
		if p.Included {
			totals[p.ID] = p.ConfirmedKills
		}
	}
	return totals
}

// TeamKillTotals sums KillTotals by team ID. Players without a team are skipped.
func TeamKillTotals(g *models.Game) map[string]int {
	totals := map[string]int{}
	for _, p := range g.Players {
		if p.Included && p.TeamID != "" {
			totals[p.TeamID] += p.ConfirmedKills
		}
	}
	return totals
}
