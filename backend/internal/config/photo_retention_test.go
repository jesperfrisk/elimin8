package config

import (
	"strings"
	"testing"
	"time"
)

func TestPhotoRetentionConfiguration(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 24 * time.Hour},
		{"1", time.Hour},
		{" 48 ", 48 * time.Hour},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("PHOTO_RETENTION_HOURS", tc.value)
			got, err := LoadPhotoRetention()
			if err != nil || got != tc.want {
				t.Fatalf("duration = %v, error = %v; want %v", got, err, tc.want)
			}
		})
	}
	for _, value := range []string{"0", "-1", "0.5", "invalid-secret-value", " ", "2562048", "9223372036854775807", "99999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PHOTO_RETENTION_HOURS", value)
			if _, err := LoadPhotoRetention(); err == nil || strings.Contains(err.Error(), "invalid-secret-value") {
				t.Fatal("invalid duration accepted or raw environment value exposed")
			}
		})
	}
}
