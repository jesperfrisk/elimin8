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
}

// LobbyMission is a game-local assignment candidate. Its ID is only for lobby
// controls; player assignments own a copy of the text independently of this entry.
type LobbyMission struct {
	ID      string `bson:"id" json:"id"`
	Text    string `bson:"text" json:"text"`
	Enabled bool   `bson:"enabled" json:"enabled"`
}

type Player struct {
	ID                string     `bson:"id"`
	Name              string     `bson:"name"`
	CodeHash          string     `bson:"codeHash"`
	CredentialVersion int        `bson:"credentialVersion"`
	JoinedAt          *time.Time `bson:"joinedAt,omitempty"`
	Included          bool       `bson:"included"`
	Mission           string     `bson:"mission,omitempty"`
	VictimID          string     `bson:"victimId,omitempty"`
	EliminatedAt      *time.Time `bson:"eliminatedAt,omitempty"`
}

type Elimination struct {
	ID         string     `bson:"id" json:"id"`
	KillerID   string     `bson:"killerId" json:"killerId"`
	VictimID   string     `bson:"victimId" json:"victimId"`
	Status     string     `bson:"status" json:"status"`
	CreatedAt  time.Time  `bson:"createdAt" json:"createdAt"`
	ResolvedAt *time.Time `bson:"resolvedAt,omitempty" json:"resolvedAt,omitempty"`
}

// ParticipantView is safe to share with other players: no private assignments.
type ParticipantView struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	EliminatedAt *time.Time `json:"eliminatedAt,omitempty"`
}

type Access struct {
	Hash      string    `bson:"_id"`
	GameID    string    `bson:"gameId"`
	PlayerID  string    `bson:"playerId"`
	Version   int       `bson:"version"`
	ExpiresAt time.Time `bson:"expiresAt"`
}

type PlayerView struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	JoinedAt     *time.Time  `json:"joinedAt"`
	Included     bool        `json:"included"`
	Mission      string      `json:"mission,omitempty"`
	Victim       *VictimView `json:"victim,omitempty"`
	EliminatedAt *time.Time  `json:"eliminatedAt,omitempty"`
}

// VictimView exposes identity only, never the victim's private assignments.
type VictimView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type GameView struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	InvitationCode   string            `json:"invitationCode"`
	Status           string            `json:"status"`
	Revision         int64             `json:"revision"`
	CreatedAt        time.Time         `json:"createdAt"`
	StartedAt        *time.Time        `json:"startedAt"`
	EndedAt          *time.Time        `json:"endedAt"`
	JoinedCount      int               `json:"joinedCount"`
	InvitedCount     int               `json:"invitedCount"`
	ParticipantCount int               `json:"participantCount"`
	Players          []PlayerView      `json:"players,omitempty"`
	Me               *PlayerView       `json:"me,omitempty"`
	Missions         *[]LobbyMission   `json:"missions,omitempty"`
	Participants     []ParticipantView `json:"participants,omitempty"`
	Eliminations     []Elimination     `json:"eliminations,omitempty"`
	WinnerID         string            `json:"winnerId,omitempty"`
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
