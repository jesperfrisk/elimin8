package models

import "time"

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
	// Always persist the empty value so removing a photo clears MongoDB's field.
	PhotoID string `bson:"photoId"`

	// Reserved for Phase 1 features; not exposed in any view yet.
	TeamID         string     `bson:"teamId,omitempty"`
	WithdrawnAt    *time.Time `bson:"withdrawnAt,omitempty"`
	ConfirmedKills int        `bson:"confirmedKills,omitempty"`
}
