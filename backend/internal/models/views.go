package models

import "time"

// ParticipantView is safe to share with other players: no private assignments.
type ParticipantView struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	EliminatedAt *time.Time `json:"eliminatedAt,omitempty"`
}

type PlayerView struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	JoinedAt     *time.Time  `json:"joinedAt"`
	Included     bool        `json:"included"`
	Mission      string      `json:"mission,omitempty"`
	Victim       *VictimView `json:"victim,omitempty"`
	EliminatedAt *time.Time  `json:"eliminatedAt,omitempty"`

	// Only ever set on the player's own "me" view.
	Team *TeamView `json:"team,omitempty"`
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

	// Timer: visible to every role. Remaining time is computed at view time.
	TimerMinutes     *int       `json:"timerMinutes"`
	EndsAt           *time.Time `json:"endsAt"`
	RemainingSeconds *int64     `json:"remainingSeconds"`
	EndReason        *string    `json:"endReason"`

	// Organizer only. Players and participants never receive team setup.
	Mode  string          `json:"mode,omitempty"`
	Teams []TeamSetupView `json:"teams,omitempty"`
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
