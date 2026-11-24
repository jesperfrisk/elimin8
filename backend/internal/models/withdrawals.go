package models

import "time"

// Withdrawal is a pending, unconfirmed withdrawal request. Reserved for the
// withdrawals feature; Player.WithdrawnAt records a confirmed withdrawal.
type Withdrawal struct {
	PlayerID  string    `bson:"playerId"`
	TokenHash string    `bson:"tokenHash"`
	ExpiresAt time.Time `bson:"expiresAt"`
}
