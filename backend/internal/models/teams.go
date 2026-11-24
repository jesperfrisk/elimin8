package models

// GameMode selects individual or team play. The zero value means individual.
type GameMode string

const (
	ModeIndividual GameMode = "individual"
	ModeTeam       GameMode = "team"
)

// Team groups players. Membership lives on Player.TeamID (one source of truth).
type Team struct {
	ID   string `bson:"id"`
	Name string `bson:"name"`
}

// TeamSetupView is the organizer's view of a team. Never sent to players.
type TeamSetupView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	PlayerIDs []string `json:"playerIds"`
}

// TeamView is a player's own team: their name and teammates, nothing else.
type TeamView struct {
	Name    string       `json:"name"`
	Members []VictimView `json:"members"`
}
