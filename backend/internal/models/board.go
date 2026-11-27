package models

import "time"

// BoardView is the unauthenticated live-board payload. It is deliberately
// minimal: names, ids, a stable display number, status, the active count and
// the timer. It must never carry missions, targets, hunters, teams, kill
// counts, killer identities or codes.
type BoardView struct {
	GameID      string        `json:"gameId"`
	Name        string        `json:"name"`
	Status      string        `json:"status"`
	ActiveCount int           `json:"activeCount"`
	Timer       *BoardTimer   `json:"timer"`
	Players     []BoardPlayer `json:"players"`
}

type BoardPlayer struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// PhotoURL is the opaque, relative URL of the player's photo (absent when they
	// have none). Photos of included participants are readable by board viewers.
	PhotoURL string `json:"photoUrl,omitempty"`
}

type BoardTimer struct {
	Minutes          int        `json:"minutes"`
	EndsAt           *time.Time `json:"endsAt"`
	RemainingSeconds *int64     `json:"remainingSeconds"`
	EndReason        *string    `json:"endReason"`
}
