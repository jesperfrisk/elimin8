package models

// RecapSummary is the headline numbers of a finished game.
type RecapSummary struct {
	DurationSeconds int    `json:"durationSeconds"`
	Players         int    `json:"players"`
	Eliminations    int    `json:"eliminations"`
	Withdrawals     int    `json:"withdrawals"`
	DeclinedReports int    `json:"declinedReports"`
	Reason          string `json:"reason"` // "survival", "timer" or "organizer"
	Tie             bool   `json:"tie"`
}

// RecapEvent is one player leaving the game. It names the subject only, never
// the killer.
type RecapEvent struct {
	PlayerID  string `json:"playerId"`
	Name      string `json:"name"`
	Event     string `json:"event"` // "eliminated" or "withdrew"
	AtSeconds int    `json:"atSeconds"`
}

// RecapAward names one player and a time in seconds. For the longest survivor,
// AtSeconds is how long they stayed in the game.
type RecapAward struct {
	PlayerID  string `json:"playerId"`
	Name      string `json:"name"`
	AtSeconds int    `json:"atSeconds"`
}

// RecapAwards never contains killer/victim pairs.
type RecapAwards struct {
	LongestSurvivor    *RecapAward      `json:"longestSurvivor"`
	FirstElimination   *RecapAward      `json:"firstElimination"`
	FastestElimination *RecapAward      `json:"fastestElimination"`
	MostKills          []ResultStanding `json:"mostKills"`
	LastElimination    *RecapAward      `json:"lastElimination"`
}

// Recap is the response of GET /api/games/{id}/recap, available once the game
// has ended.
type Recap struct {
	Winners     []ResultWinner   `json:"winners"`
	Summary     RecapSummary     `json:"summary"`
	Leaderboard []ResultStanding `json:"leaderboard"`
	Timeline    []RecapEvent     `json:"timeline"`
	Awards      RecapAwards      `json:"awards"`
}
