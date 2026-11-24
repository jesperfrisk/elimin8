package models

const (
	KillBoardActive     = "active"
	KillBoardEliminated = "eliminated"
	KillBoardWithdrawn  = "withdrawn"
)

// KillBoardEntry is deliberately minimal: it must never carry killers,
// victims, missions, kill counts, teams or timestamps.
type KillBoardEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type KillBoardView struct {
	Entries []KillBoardEntry `json:"entries"`
}
