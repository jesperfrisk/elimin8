package mongodb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// This verifies actual Mongo filtering/pagination rather than duplicating the
// query in a fake. Set MONGODB_TEST_URI to an isolated local MongoDB instance.
func TestPhotoRetentionMongoDiscovery(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set; requires isolated MongoDB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal("MongoDB test connection could not be configured")
	}
	defer client.Disconnect(context.Background())
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatal("MongoDB test instance is unavailable")
	}
	db := client.Database(fmt.Sprintf("elimin8_photo_retention_test_%d", time.Now().UnixNano()))
	defer db.Drop(context.Background())
	repo := NewGameRepository(db)
	if err := repo.EnsurePhotoRetentionIndex(ctx); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	var documents []any
	for i := 0; i < 205; i++ {
		documents = append(documents, bson.M{"_id": fmt.Sprintf("game%03d", i), "status": "ended", "endedAt": cutoff.Add(-time.Hour), "organizerHash": "not-public"})
	}
	documents = append(documents,
		bson.M{"_id": "boundary", "status": "ended", "endedAt": cutoff},
		bson.M{"_id": "active", "status": "active", "endedAt": cutoff.Add(-time.Hour)},
		bson.M{"_id": "lobby", "status": "lobby", "endedAt": cutoff.Add(-time.Hour)},
		bson.M{"_id": "recent", "status": "ended", "endedAt": cutoff.Add(time.Millisecond)},
		bson.M{"_id": "missing", "status": "ended"},
		bson.M{"_id": "null", "status": "ended", "endedAt": nil},
		bson.M{"_id": "wrong-type", "status": "ended", "endedAt": "2020-01-01"},
	)
	if _, err := db.Collection("games").InsertMany(ctx, documents); err != nil {
		t.Fatal(err)
	}
	var all []string
	after := ""
	for {
		ids, err := repo.DuePhotoGames(ctx, cutoff, after, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			if id <= after || !(id == "boundary" || strings.HasPrefix(id, "game")) {
				t.Fatalf("invalid/stale discovery result %q after %q", id, after)
			}
			after = id
			all = append(all, id)
		}
		if len(ids) < 100 {
			break
		}
	}
	if len(all) != 206 || all[0] != "boundary" || all[len(all)-1] != "game204" {
		t.Fatalf("unexpected discovery results: count %d", len(all))
	}
	for _, limit := range []int{-1, 0, 9999} {
		ids, err := repo.DuePhotoGames(ctx, cutoff, "", limit)
		if err != nil || len(ids) != 100 {
			t.Fatalf("unbounded/invalid limit %d: count=%d error=%v", limit, len(ids), err)
		}
	}
}
