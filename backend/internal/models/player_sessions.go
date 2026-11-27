package models

import "time"

// PlayerSession is a hashed remembered credential, separate from API access.
// It is never an HTTP view; even accidental JSON serialization reveals nothing.
type PlayerSession struct {
	Hash      string    `bson:"_id" json:"-"`
	GameID    string    `bson:"gameId" json:"-"`
	PlayerID  string    `bson:"playerId" json:"-"`
	Version   int       `bson:"version" json:"-"`
	CreatedAt time.Time `bson:"createdAt" json:"-"`
	ExpiresAt time.Time `bson:"expiresAt" json:"-"`
}
