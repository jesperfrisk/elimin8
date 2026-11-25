package config

import (
	"os"
	"strconv"
)

const (
	DefaultLoginRateLimit = 30
	MaxLoginRateLimit     = 600
)

// Config contains server-side configuration. Values are read from the process environment.
type Config struct {
	HTTPAddress     string
	MongoDBURI      string
	MongoDBDatabase string
	// LoginRateLimit is the anonymous attempts allowed per remote address and
	// endpoint each minute. It is bounded to [1, MaxLoginRateLimit].
	LoginRateLimit int
}

func Load() Config {
	return Config{
		HTTPAddress:     envOrDefault("HTTP_ADDR", ":8080"),
		MongoDBURI:      envOrDefault("MONGODB_URI", "mongodb://localhost:27017"),
		MongoDBDatabase: envOrDefault("MONGODB_DATABASE", "elimin8"),
		LoginRateLimit:  loginRateLimit(os.Getenv("LOGIN_RATE_LIMIT_PER_MINUTE")),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// loginRateLimit parses LOGIN_RATE_LIMIT_PER_MINUTE. Anything unset, malformed
// or outside [1, MaxLoginRateLimit] keeps the protected default, so a typo can
// never switch the limiter off.
func loginRateLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxLoginRateLimit {
		return DefaultLoginRateLimit
	}
	return n
}
