package models

import "time"

// Timer is the optional game time limit. Reserved for the timer feature.
type Timer struct {
	DurationMinutes int        `bson:"durationMinutes"`
	EndsAt          *time.Time `bson:"endsAt,omitempty"`
}
