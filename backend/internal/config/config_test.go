package config

import "testing"

func TestLoginRateLimitIsBoundedAndDefaultsSafely(t *testing.T) {
	for raw, want := range map[string]int{
		"":        DefaultLoginRateLimit,
		"abc":     DefaultLoginRateLimit,
		"0":       DefaultLoginRateLimit,
		"-1":      DefaultLoginRateLimit,
		"601":     DefaultLoginRateLimit,
		"1":       1,
		"120":     120,
		"600":     600,
		" 100":    DefaultLoginRateLimit,
		"1000000": DefaultLoginRateLimit,
	} {
		if got := loginRateLimit(raw); got != want {
			t.Errorf("loginRateLimit(%q) = %d, want %d", raw, got, want)
		}
	}
}
