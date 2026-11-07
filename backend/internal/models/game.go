package models

import "time"

// Persistence types never leave the repository/service boundary as HTTP responses.
type Game struct {
	ID                string     `bson:"_id"`
	Name              string     `bson:"name"`
	Description       string     `bson:"description"`
	InvitationCode    string     `bson:"invitationCode"`
	OrganizerHash     string     `bson:"organizerHash"`
	Status            string     `bson:"status"`
	Revision          int64      `bson:"revision"`
	PublishedRevision int64      `bson:"publishedRevision"`
	Players           []Player   `bson:"players"`
	CreatedAt         time.Time  `bson:"createdAt"`
	StartedAt         *time.Time `bson:"startedAt,omitempty"`
	EndedAt           *time.Time `bson:"endedAt,omitempty"`
}

type Player struct {
	ID                string     `bson:"id"`
	Name              string     `bson:"name"`
	CodeHash          string     `bson:"codeHash"`
	CredentialVersion int        `bson:"credentialVersion"`
	JoinedAt          *time.Time `bson:"joinedAt,omitempty"`
	Included          bool       `bson:"included"`
}

type Access struct {
	Hash      string    `bson:"_id"`
	GameID    string    `bson:"gameId"`
	PlayerID  string    `bson:"playerId"`
	Version   int       `bson:"version"`
	ExpiresAt time.Time `bson:"expiresAt"`
}

type PlayerView struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	JoinedAt *time.Time `json:"joinedAt"`
	Included bool       `json:"included"`
}

type GameView struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Description      string       `json:"description"`
	InvitationCode   string       `json:"invitationCode"`
	Status           string       `json:"status"`
	Revision         int64        `json:"revision"`
	CreatedAt        time.Time    `json:"createdAt"`
	StartedAt        *time.Time   `json:"startedAt"`
	EndedAt          *time.Time   `json:"endedAt"`
	JoinedCount      int          `json:"joinedCount"`
	InvitedCount     int          `json:"invitedCount"`
	ParticipantCount int          `json:"participantCount"`
	Players          []PlayerView `json:"players,omitempty"`
	Me               *PlayerView  `json:"me,omitempty"`
}

type Admission struct {
	Token          string    `json:"token"`
	ExpiresAt      time.Time `json:"expiresAt"`
	Role           string    `json:"role"`
	ClientPrefix   string    `json:"clientPrefix"`
	Topic          string    `json:"topic"`
	Game           GameView  `json:"game"`
	ManagementCode string    `json:"managementCode,omitempty"`
}

type IssuedPlayer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}
