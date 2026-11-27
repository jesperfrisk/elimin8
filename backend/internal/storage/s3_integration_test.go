package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Opt in only against a disposable, explicitly configured private S3 service.
func TestS3LivePrivateOperations(t *testing.T) {
	endpoint := os.Getenv("ELIMIN8_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("ELIMIN8_TEST_S3_ENDPOINT is unset")
	}
	ctx := context.Background()
	store, err := NewS3(ctx, S3Config{Endpoint: endpoint, Provider: "rustfs", Region: "us-east-1", Bucket: "elimin8-photos", AccessKey: os.Getenv("PHOTO_STORAGE_ACCESS_KEY"), SecretKey: os.Getenv("PHOTO_STORAGE_SECRET_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	var suffix [16]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	prefix := "games/STORAGESMOKE/photos/"
	key := prefix + hex.EncodeToString(suffix[:])
	payload := []byte("processed JPEG test bytes")
	if err = store.Put(ctx, key, payload); err != nil {
		t.Fatal(err)
	}
	defer store.Delete(ctx, key)
	data, err := store.Get(ctx, key)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("private object read failed: %v", err)
	}
	objects, err := store.List(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range objects {
		if o.Key == key {
			found = true
		}
	}
	if !found {
		t.Fatal("uploaded object absent from listing")
	}
	response, err := http.Get(strings.TrimRight(endpoint, "/") + "/elimin8-photos/" + key)
	if err != nil {
		t.Fatal("anonymous check failed")
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous object read should be forbidden, got %d", response.StatusCode)
	}
	if err = store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err = store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted object remained readable: %v", err)
	}
	// Simulate another S3 tool creating a normal external filename. Cleanup must
	// preserve its exact plus/percent/unicode spelling and use DeleteObjects.
	oddKey := key + " family räven+%2F.jpg"
	_, err = store.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(oddKey), Body: bytes.NewReader(payload), ContentType: aws.String("image/jpeg")})
	if err != nil {
		t.Fatal("external odd-name upload failed")
	}
	defer store.Delete(ctx, oddKey)
	objects, err = store.List(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, object := range objects {
		if object.Key == oddKey {
			found = true
		}
	}
	if !found {
		t.Fatal("external odd filename changed during listing")
	}
	if err = store.Delete(ctx, oddKey); err != nil {
		t.Fatal("external odd-name cleanup failed", err)
	}
	remaining, err := store.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(oddKey)})
	if err == nil {
		remaining.Body.Close()
		t.Fatal("external odd-name photo remained after cleanup")
	}
	if !hasCode(err, "NoSuchKey", "NotFound", "404") {
		t.Fatal("external odd-name absence check failed")
	}
}
