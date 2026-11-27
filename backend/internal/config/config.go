package config

import (
	"os"
	"strconv"

	"github.com/jesperfrisk/elimin8/backend/internal/storage"
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
	// TrustedProxies is the raw TRUSTED_PROXIES list (IPs/CIDRs) whose
	// X-Forwarded-For header is believed. Unset trusts no proxy.
	TrustedProxies string
	PhotoStorage   storage.S3Config
}

func Load() Config {
	return Config{
		HTTPAddress:     envOrDefault("HTTP_ADDR", ":8080"),
		MongoDBURI:      envOrDefault("MONGODB_URI", "mongodb://localhost:27017"),
		MongoDBDatabase: envOrDefault("MONGODB_DATABASE", "elimin8"),
		LoginRateLimit:  loginRateLimit(os.Getenv("LOGIN_RATE_LIMIT_PER_MINUTE")),
		TrustedProxies:  os.Getenv("TRUSTED_PROXIES"),
		PhotoStorage: storage.S3Config{
			Endpoint:  os.Getenv("PHOTO_STORAGE_ENDPOINT"),
			Provider:  envOrDefault("PHOTO_STORAGE_PROVIDER", "s3"),
			Region:    envOrDefault("PHOTO_STORAGE_REGION", "us-east-1"),
			Bucket:    envOrDefault("PHOTO_STORAGE_BUCKET", "elimin8-photos"),
			AccessKey: os.Getenv("PHOTO_STORAGE_ACCESS_KEY"),
			SecretKey: os.Getenv("PHOTO_STORAGE_SECRET_KEY"),
		},
	}
}

// ValidatePhotoStorage rejects partial configuration; an unset endpoint with no
// credentials keeps native development independent of an object store.
func (c Config) ValidatePhotoStorage() error {
	if c.PhotoStorage.Endpoint == "" {
		if c.PhotoStorage.AccessKey != "" || c.PhotoStorage.SecretKey != "" {
			return storage.ErrInvalid
		}
		return nil
	}
	return c.PhotoStorage.Validate()
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
