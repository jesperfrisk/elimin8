package mongodb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestPlayerSessionMongoPersistenceTTLAndSeparation(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(5 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	db := client.Database(fmt.Sprintf("elimin8_sessions_test_%d", time.Now().UnixNano()))
	defer func() { _ = db.Drop(context.Background()) }()
	repo := NewGameRepository(db)
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	record := models.PlayerSession{Hash: services.HashToken("raw-test-secret"), GameID: "game", PlayerID: "player", Version: 3, CreatedAt: now, ExpiresAt: now.Add(services.RememberDuration)}
	if err := repo.PutPlayerSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	// Reconstructing the repository must find state persisted by the first one.
	restarted := NewGameRepository(db)
	got, err := restarted.GetPlayerSession(ctx, record.Hash)
	if err != nil || got != record {
		t.Fatal("session did not survive repository restart")
	}
	var stored bson.M
	if err := db.Collection("player_sessions").FindOne(ctx, bson.M{"_id": record.Hash}).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 6 || stored["_id"] != record.Hash {
		t.Fatal("unexpected persistence fields")
	}
	if count, err := db.Collection("game_access").CountDocuments(ctx, bson.M{"_id": record.Hash}); err != nil || count != 0 {
		t.Fatal("remembered credential entered bearer collection")
	}
	indexes, err := db.Collection("player_sessions").Indexes().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer indexes.Close(ctx)
	found := false
	for indexes.Next(ctx) {
		var index struct {
			Key    bson.M `bson:"key"`
			Expire *int32 `bson:"expireAfterSeconds"`
		}
		if err := indexes.Decode(&index); err != nil {
			t.Fatal(err)
		}
		if _, exists := index.Key["expiresAt"]; exists && index.Expire != nil && *index.Expire == 0 {
			found = true
		}
	}
	if indexes.Err() != nil || !found {
		t.Fatal("absolute-expiry TTL index missing")
	}
	if err := restarted.DeletePlayerSession(ctx, record.Hash); err != nil {
		t.Fatal(err)
	}
	if err := restarted.DeletePlayerSession(ctx, record.Hash); err != nil {
		t.Fatal("deletion must be idempotent")
	}
	if _, err := restarted.GetPlayerSession(ctx, record.Hash); err != services.ErrNotFound {
		t.Fatal("deleted session still present")
	}
	game := &models.Game{ID: "game", InvitationCode: "SESSION-MONGO", Status: "lobby", Revision: 1, Players: []models.Player{{ID: "player", CredentialVersion: 3, JoinedAt: &now}}}
	if err := repo.Insert(ctx, game); err != nil {
		t.Fatal(err)
	}
	bearer := strings.Repeat("B", 48)
	if err := repo.PutAccess(ctx, models.Access{Hash: services.HashToken(bearer), GameID: "game", PlayerID: "player", Version: 3, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	issuer := services.NewGameService(repo)
	issuer.Now = func() time.Time { return now }
	secret, persisted, err := issuer.IssuePlayerSession(ctx, bearer)
	if err != nil {
		t.Fatal(err)
	}
	newProcess := services.NewGameService(NewGameRepository(db))
	clock := now.Add(10 * 24 * time.Hour)
	newProcess.Now = func() time.Time { return clock }
	if got, err := newProcess.ValidatePlayerSession(ctx, "game", secret); err != nil || got.ExpiresAt != persisted.ExpiresAt {
		t.Fatal("service restart lost or renewed credential")
	}
	clock = persisted.ExpiresAt
	if _, err := newProcess.ValidatePlayerSession(ctx, "game", secret); err != services.ErrUnauthorized {
		t.Fatal("real stored credential accepted at expiry")
	}
	if _, err := repo.GetPlayerSession(ctx, persisted.Hash); err != nil {
		t.Fatal("application expiry test requires delayed TTL deletion")
	}
	clock = now
	resumed, err := newProcess.ResumePlayerSession(ctx, "game", secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := newProcess.ForgetPlayerSession(ctx, "game", secret); err != nil {
		t.Fatal(err)
	}
	thirdProcess := services.NewGameService(NewGameRepository(db))
	if _, err := thirdProcess.ValidatePlayerSession(ctx, "game", secret); err != services.ErrUnauthorized {
		t.Fatal("revocation lost on process restart")
	}
	if _, _, err := thirdProcess.Authorize(ctx, resumed.Token); err != services.ErrUnauthorized {
		t.Fatal("linked bearer survived persisted revocation")
	}
	marker, err := repo.GetPlayerSession(ctx, persisted.Hash)
	if err != nil || marker.RevokedAt == nil || marker.ExpiresAt != persisted.ExpiresAt {
		t.Fatal("revocation did not preserve fixed-expiry marker")
	}
	if err := thirdProcess.ForgetPlayerSession(ctx, "game", secret); err != nil {
		t.Fatal(err)
	}
	again, err := repo.GetPlayerSession(ctx, persisted.Hash)
	if err != nil || !again.RevokedAt.Equal(*marker.RevokedAt) {
		t.Fatal("repeated revocation changed the original marker")
	}
}
