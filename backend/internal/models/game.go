package models

import "time"

// Persistence types never leave the repository/service boundary as HTTP responses.
type Game struct {
	ID                string         `bson:"_id"`
	Name              string         `bson:"name"`
	Description       string         `bson:"description"`
	InvitationCode    string         `bson:"invitationCode"`
	OrganizerHash     string         `bson:"organizerHash"`
	Status            string         `bson:"status"`
	Revision          int64          `bson:"revision"`
	PublishedRevision int64          `bson:"publishedRevision"`
	Players           []Player       `bson:"players"`
	Missions          []LobbyMission `bson:"missions"`
	CreatedAt         time.Time      `bson:"createdAt"`
	StartedAt         *time.Time     `bson:"startedAt,omitempty"`
	EndedAt           *time.Time     `bson:"endedAt,omitempty"`
	Eliminations      []Elimination  `bson:"eliminations"`
	WinnerID          string         `bson:"winnerId,omitempty"`

	// Reserved for Phase 1 features. Zero values are omitted from storage and
	// none of these are exposed in any view until their feature ticket lands.
	Mode      GameMode    `bson:"mode,omitempty"`
	Teams     []Team      `bson:"teams,omitempty"`
	Timer     *Timer      `bson:"timer,omitempty"`
	EndReason string      `bson:"endReason,omitempty"`
	Result    *GameResult `bson:"result,omitempty"`
}

type Access struct {
	Hash      string    `bson:"_id"`
	GameID    string    `bson:"gameId"`
	PlayerID  string    `bson:"playerId"`
	Version   int       `bson:"version"`
	ExpiresAt time.Time `bson:"expiresAt"`
}
