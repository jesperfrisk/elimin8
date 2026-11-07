package config

import "os"

// Config contains server-side configuration. Values are read from the process environment.
type Config struct {
	HTTPAddress     string
	MongoDBURI      string
	MongoDBDatabase string
}

func Load() Config {
	return Config{
		HTTPAddress:     envOrDefault("HTTP_ADDR", ":8080"),
		MongoDBURI:      envOrDefault("MONGODB_URI", "mongodb://localhost:27017"),
		MongoDBDatabase: envOrDefault("MONGODB_DATABASE", "elimin8"),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
