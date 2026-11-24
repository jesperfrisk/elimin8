package models

// ResultWinner is a winning player or team: identity only.
type ResultWinner struct {
	Kind string `bson:"kind" json:"kind"` // "player" or "team"
	ID   string `bson:"id" json:"id"`
	Name string `bson:"name" json:"name"`
}

// ResultStanding is one player's (individual games) or team's (team games)
// final confirmed-kill count. Kill counts exist only here, after the game has
// ended; they never appear in any mid-game view.
type ResultStanding struct {
	Kind  string `bson:"kind" json:"kind"`
	ID    string `bson:"id" json:"id"`
	Name  string `bson:"name" json:"name"`
	Kills int    `bson:"kills" json:"kills"`
}

// GameResult is the final, immutable result of a game, stored once when the
// game ends by survival or by timer expiry. Its JSON form is the response of
// GET /api/games/{id}/result.
type GameResult struct {
	Reason    string           `bson:"reason" json:"reason"` // "survival" or "timer"
	Tie       bool             `bson:"tie" json:"tie"`
	Winners   []ResultWinner   `bson:"winners" json:"winners"`
	Standings []ResultStanding `bson:"standings" json:"standings"`
}
