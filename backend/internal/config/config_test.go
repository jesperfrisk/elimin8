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

func TestPhotoStorageConfiguration(t *testing.T) {
	for _, key := range []string{"PHOTO_STORAGE_PROVIDER", "PHOTO_STORAGE_ENDPOINT", "PHOTO_STORAGE_REGION", "PHOTO_STORAGE_BUCKET", "PHOTO_STORAGE_ACCESS_KEY", "PHOTO_STORAGE_SECRET_KEY"} {
		t.Setenv(key, "")
	}
	cfg := Load()
	if cfg.PhotoStorage.Endpoint != "" || cfg.ValidatePhotoStorage() != nil {
		t.Fatal("native storage should default to disabled")
	}
	if cfg.PhotoStorage.Provider != "s3" || cfg.PhotoStorage.Region != "us-east-1" || cfg.PhotoStorage.Bucket != "elimin8-photos" {
		t.Fatal("unexpected defaults")
	}
	t.Setenv("PHOTO_STORAGE_ACCESS_KEY", "test-access")
	if Load().ValidatePhotoStorage() == nil {
		t.Fatal("partial credentials must fail")
	}
	t.Setenv("PHOTO_STORAGE_ENDPOINT", "http://127.0.0.1:9000")
	if Load().ValidatePhotoStorage() == nil {
		t.Fatal("missing secret must fail")
	}
	t.Setenv("PHOTO_STORAGE_SECRET_KEY", "test-secret")
	if Load().ValidatePhotoStorage() != nil {
		t.Fatal("complete configuration should pass")
	}
	t.Setenv("PHOTO_STORAGE_PROVIDER", "rustfs")
	if Load().ValidatePhotoStorage() != nil {
		t.Fatal("explicit RustFS provider should pass")
	}
	t.Setenv("PHOTO_STORAGE_PROVIDER", "unknown")
	if Load().ValidatePhotoStorage() == nil {
		t.Fatal("unsupported provider must fail")
	}
	t.Setenv("PHOTO_STORAGE_PROVIDER", "s3")
	t.Setenv("PHOTO_STORAGE_ENDPOINT", "https://user:password@example.com")
	if Load().ValidatePhotoStorage() == nil {
		t.Fatal("URL credentials must be rejected")
	}
}
