package services

import (
	"errors"
	"sort"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

var ErrTooFewPlayers = errors.New("at least two players must join the lobby before starting")

// ErrNoValidAssignment means no circle exists in which nobody's victim is a
// teammate (one team holds more than half of the circle). The winner ticket
// consumes this through ValidAssignmentExists.
var ErrNoValidAssignment = errors.New("no team-safe victim assignment exists for the remaining players")

// victimIntn returns a uniform int in [0, n). Tests replace it for determinism.
var victimIntn = cryptoIntn

// circleKey is the team a player is grouped by. Players without a team (and
// every player in individual mode) count as their own singleton team.
func circleKey(g *models.Game, p *models.Player) string {
	if g.Mode == models.ModeTeam && p.TeamID != "" {
		return "t:" + p.TeamID
	}
	return "p:" + p.ID
}

// feasibleCircle reports whether keys can form a circle with no two adjacent
// equal keys: at least two players and no key above half of them.
func feasibleCircle(keys []string) bool {
	if len(keys) < 2 {
		return false
	}
	counts := map[string]int{}
	for _, k := range keys {
		counts[k]++
		if counts[k]*2 > len(keys) {
			return false
		}
	}
	return true
}

// ValidAssignmentExists reports whether the living participants (joined
// players before the start, included and not eliminated afterwards) can form
// a circle with no victim on the hunter's own team. It is always true in
// individual mode when two or more players remain.
func ValidAssignmentExists(g *models.Game) bool {
	var keys []string
	for i := range g.Players {
		if p := &g.Players[i]; assignable(g, p) {
			keys = append(keys, circleKey(g, p))
		}
	}
	return feasibleCircle(keys)
}

func assignable(g *models.Game, p *models.Player) bool {
	if g.Status == "lobby" || g.StartedAt == nil {
		return p.JoinedAt != nil
	}
	return p.Included && p.EliminatedAt == nil
}

func shuffle[T any](items []T) {
	for i := len(items) - 1; i > 0; i-- {
		j := victimIntn(i + 1)
		items[i], items[j] = items[j], items[i]
	}
}

// arrangeCircle returns the players (indexes into g.Players) in circle order.
// In team mode no two neighbours, including the wrap-around pair, share a
// team. Members are shuffled, then grouped by team (largest first, random
// order among equals) and dealt into the even slots followed by the odd
// slots, which keeps same-team players apart whenever that is possible.
func arrangeCircle(g *models.Game, participants []int) ([]int, error) {
	keys := make([]string, len(participants))
	for i, idx := range participants {
		keys[i] = circleKey(g, &g.Players[idx])
	}
	if g.Mode == models.ModeTeam && !feasibleCircle(keys) {
		return nil, ErrNoValidAssignment
	}
	shuffle(participants)
	if g.Mode != models.ModeTeam {
		return participants, nil
	}
	groups := map[string][]int{}
	var order []string
	for _, idx := range participants {
		k := circleKey(g, &g.Players[idx])
		if groups[k] == nil {
			order = append(order, k)
		}
		groups[k] = append(groups[k], idx)
	}
	shuffle(order)
	sort.SliceStable(order, func(a, b int) bool { return len(groups[order[a]]) > len(groups[order[b]]) })
	dealt := make([]int, 0, len(participants))
	for _, k := range order {
		dealt = append(dealt, groups[k]...)
	}
	circle := make([]int, len(dealt))
	slot := 0
	for _, idx := range dealt {
		circle[slot] = idx
		slot += 2
		if slot >= len(circle) {
			slot = 1
		}
	}
	// A rotation keeps the start position random without breaking adjacency.
	r := victimIntn(len(circle))
	return append(circle[r:], circle[:r]...), nil
}

// A shuffled circle gives every participant exactly one victim and one hunter,
// with no self-targets or disconnected groups. In team mode no victim is a
// teammate; ErrNoValidAssignment is returned when that is impossible and no
// assignments are written. Invitees do not enter the circle.
func assignVictims(g *models.Game) error {
	participants := make([]int, 0, len(g.Players))
	for i, p := range g.Players {
		if p.JoinedAt != nil {
			participants = append(participants, i)
		}
	}
	if len(participants) < 2 {
		return ErrTooFewPlayers
	}
	circle, err := arrangeCircle(g, participants)
	if err != nil {
		return err
	}
	for i := range g.Players {
		g.Players[i].VictimID = ""
	}
	linkCircle(g, circle)
	return nil
}

func linkCircle(g *models.Game, circle []int) {
	for i, index := range circle {
		g.Players[index].VictimID = g.Players[circle[(i+1)%len(circle)]].ID
	}
}

// livingCircle returns living participants in victim-chain order, or nil when
// the chain is not one complete circle.
func livingCircle(g *models.Game) []int {
	byID := map[string]int{}
	first := -1
	for i := range g.Players {
		if p := &g.Players[i]; assignable(g, p) {
			byID[p.ID] = i
			if first < 0 {
				first = i
			}
		}
	}
	if first < 0 {
		return nil
	}
	circle := []int{first}
	for next, ok := byID[g.Players[first].VictimID]; ok && next != first; next, ok = byID[g.Players[next].VictimID] {
		if len(circle) > len(byID) {
			return nil
		}
		circle = append(circle, next)
	}
	if len(circle) != len(byID) {
		return nil
	}
	return circle
}

func circleIsSafe(g *models.Game, circle []int) bool {
	for i, idx := range circle {
		if circleKey(g, &g.Players[idx]) == circleKey(g, &g.Players[circle[(i+1)%len(circle)]]) {
			return false
		}
	}
	return true
}

// repairTeamCircle is called after any transfer (elimination, withdrawal). If
// a hunter now targets a teammate it reorders the living circle: first by
// swapping two players, then by re-dealing everyone. It returns
// ErrNoValidAssignment, leaving the circle untouched, when no safe circle
// exists. It does nothing in individual mode.
func repairTeamCircle(g *models.Game) error {
	if g.Mode != models.ModeTeam {
		return nil
	}
	circle := livingCircle(g)
	if len(circle) < 2 || circleIsSafe(g, circle) {
		return nil
	}
	if !ValidAssignmentExists(g) {
		return ErrNoValidAssignment
	}
	for a := 0; a < len(circle); a++ {
		for b := a + 1; b < len(circle); b++ {
			circle[a], circle[b] = circle[b], circle[a]
			if circleIsSafe(g, circle) {
				linkCircle(g, circle)
				return nil
			}
			circle[a], circle[b] = circle[b], circle[a]
		}
	}
	arranged, err := arrangeCircle(g, circle)
	if err != nil {
		return err
	}
	linkCircle(g, arranged)
	return nil
}
