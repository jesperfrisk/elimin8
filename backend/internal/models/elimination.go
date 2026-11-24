package models

import "time"

type Elimination struct {
	ID         string     `bson:"id" json:"id"`
	KillerID   string     `bson:"killerId" json:"killerId"`
	VictimID   string     `bson:"victimId" json:"victimId"`
	Status     string     `bson:"status" json:"status"`
	CreatedAt  time.Time  `bson:"createdAt" json:"createdAt"`
	ResolvedAt *time.Time `bson:"resolvedAt,omitempty" json:"resolvedAt,omitempty"`
}
