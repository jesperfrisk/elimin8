package models

import "time"

type Elimination struct {
	ID         string     `bson:"id" json:"id"`
	KillerID   string     `bson:"killerId" json:"killerId"`
	VictimID   string     `bson:"victimId" json:"victimId"`
	Status     string     `bson:"status" json:"status"`
	CreatedAt  time.Time  `bson:"createdAt" json:"createdAt"`
	ResolvedAt *time.Time `bson:"resolvedAt,omitempty" json:"resolvedAt,omitempty"`

	// Mission is the killer's mission when the victim accepted, snapshotted
	// before assignment transfer. Private to the victim: never serialized in
	// the general elimination list (json:"-"); only EliminationReceipt shows it.
	Mission string `bson:"mission,omitempty" json:"-"`
}

// EliminationReceipt is the victim's own notice of a confirmed elimination.
// Mission is null for eliminations stored before snapshots existed.
type EliminationReceipt struct {
	Killer      VictimView `json:"killer"`
	Mission     *string    `json:"mission"`
	ConfirmedAt time.Time  `json:"confirmedAt"`
}
