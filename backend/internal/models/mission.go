package models

// LobbyMission is a game-local assignment candidate. Its ID is only for lobby
// controls; player assignments own a copy of the text independently of this entry.
type LobbyMission struct {
	ID      string `bson:"id" json:"id"`
	Text    string `bson:"text" json:"text"`
	Enabled bool   `bson:"enabled" json:"enabled"`
}
