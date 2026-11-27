package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// LoadPhotoRetention returns the post-game photo lifetime. Reject invalid or
// overflowing values rather than silently retaining photos for an unintended time.
func LoadPhotoRetention() (time.Duration, error) {
	value := os.Getenv("PHOTO_RETENTION_HOURS")
	if value == "" {
		return 24 * time.Hour, nil
	}
	hours, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	const maxHours = int64((1<<63 - 1) / time.Hour)
	if err != nil || hours <= 0 || hours > maxHours {
		return 0, errors.New("PHOTO_RETENTION_HOURS must be a positive whole number of hours within the supported duration")
	}
	return time.Duration(hours) * time.Hour, nil
}
