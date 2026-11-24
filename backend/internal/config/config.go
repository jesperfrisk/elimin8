package config

import (
	"os"

	"github.com/jesperfrisk/elimin8/backend/internal/storage"
)

// Config contains server-side configuration. Values are read from the process environment.
type Config struct {
	HTTPAddress     string
	MongoDBURI      string
	MongoDBDatabase string
	PhotoStorage    storage.S3Config
}

func Load() Config {
	return Config{
		HTTPAddress:     envOrDefault("HTTP_ADDR", ":8080"),
		MongoDBURI:      envOrDefault("MONGODB_URI", "mongodb://localhost:27017"),
		MongoDBDatabase: envOrDefault("MONGODB_DATABASE", "elimin8"),
		PhotoStorage: storage.S3Config{
			Endpoint:  os.Getenv("PHOTO_STORAGE_ENDPOINT"),
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
