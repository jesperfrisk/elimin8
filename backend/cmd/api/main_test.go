package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func photoStartupEnvironment(t *testing.T, enabled bool) {
	t.Helper()
	t.Setenv("PHOTO_STORAGE_ENDPOINT", "")
	t.Setenv("PHOTO_STORAGE_PROVIDER", "s3")
	t.Setenv("PHOTO_STORAGE_REGION", "us-east-1")
	t.Setenv("PHOTO_STORAGE_BUCKET", "startup-private-bucket")
	t.Setenv("PHOTO_STORAGE_ACCESS_KEY", "")
	t.Setenv("PHOTO_STORAGE_SECRET_KEY", "")
	// A malformed URI fails synchronously, without contacting a real MongoDB.
	// Reaching this error proves disabled photo storage did not gate native startup.
	t.Setenv("MONGODB_URI", "invalid-mongo-uri-private-marker")
	if enabled {
		t.Setenv("PHOTO_STORAGE_ENDPOINT", "http://127.0.0.1:1")
		t.Setenv("PHOTO_STORAGE_ACCESS_KEY", "startup-private-access")
		t.Setenv("PHOTO_STORAGE_SECRET_KEY", "startup-private-secret")
	}
}

func TestNativeStartupIgnoresRetentionWhenPhotoStorageDisabled(t *testing.T) {
	photoStartupEnvironment(t, false)
	t.Setenv("PHOTO_RETENTION_HOURS", "invalid-private-retention-value")
	err := run()
	if err == nil || err.Error() != "MongoDB connection failed; check MONGODB_URI and database availability" {
		t.Fatalf("disabled photo storage gated native startup: %v", err)
	}
}

func TestStartupRejectsInvalidRetentionWhenPhotoStorageEnabled(t *testing.T) {
	photoStartupEnvironment(t, true)
	t.Setenv("PHOTO_RETENTION_HOURS", "invalid-private-retention-value")
	err := run()
	if err == nil || !strings.Contains(err.Error(), "PHOTO_RETENTION_HOURS must be a positive whole number") {
		t.Fatalf("enabled photo storage did not reject invalid retention before I/O: %v", err)
	}
	if strings.Contains(err.Error(), "invalid-private-retention-value") {
		t.Fatal("startup reflected raw invalid environment value")
	}
}

func TestStartupFailsClosedWhenConfiguredPhotoStorageUnavailable(t *testing.T) {
	photoStartupEnvironment(t, true)
	t.Setenv("PHOTO_RETENTION_HOURS", "24")
	var attempted atomic.Bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempted.Store(true)
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("x-amz-request-id", "private-request-marker")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>private-remote-diagnostic</Message></Error>`))
	}))
	defer remote.Close()
	t.Setenv("PHOTO_STORAGE_ENDPOINT", remote.URL)
	err := run()
	if !attempted.Load() || err == nil || !strings.Contains(err.Error(), "photo storage") || strings.Contains(err.Error(), "MongoDB") {
		t.Fatalf("configured inaccessible storage did not stop startup before MongoDB: %v", err)
	}
	if !strings.Contains(err.Error(), "during bucket lookup") {
		t.Fatal("startup did not identify the safe initialization stage")
	}
	for _, secret := range []string{remote.URL, "startup-private-bucket", "startup-private-access", "startup-private-secret", "private-request-marker", "private-remote-diagnostic"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("startup exposed remote storage diagnostics or credentials")
		}
	}
}
