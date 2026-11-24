package models

import "time"

// Withdrawal is a pending, unconfirmed withdrawal request. Only a hash of the
// confirmation token is stored. Player.WithdrawnAt records a confirmed
// withdrawal.
type Withdrawal struct {
	PlayerID  string    `bson:"playerId"`
	TokenHash string    `bson:"tokenHash"`
	ExpiresAt time.Time `bson:"expiresAt"`
}

// WithdrawalRequest is returned once to the requesting player; confirming
// requires presenting the token again.
type WithdrawalRequest struct {
	Token     string    `json:"withdrawalToken"`
	ExpiresAt time.Time `json:"expiresAt"`
}
