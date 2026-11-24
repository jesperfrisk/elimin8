package models

// GameMode selects individual or team play. The zero value means individual.
// Reserved for the teams feature; not exposed in any view yet.
type GameMode string

const (
	ModeIndividual GameMode = "individual"
	ModeTeam       GameMode = "team"
)

// Team groups players. Player.TeamID refers to Team.ID.
type Team struct {
	ID   string `bson:"id"`
	Name string `bson:"name"`
}
