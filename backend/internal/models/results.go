package models

// GameResult is the final, immutable result of a game. Reserved for the
// results feature; never part of a mid-game view.
type GameResult struct {
	Reason string `bson:"reason"`
	Tie    bool   `bson:"tie"`
}
