package mongodb

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func unsetKeys(t *testing.T, g *models.Game) []string {
	t.Helper()
	update, err := saveUpdate(g)
	if err != nil {
		t.Fatal(err)
	}
	set := update["$set"].(bson.M)
	for _, forbidden := range []string{"_id", "publishedRevision"} {
		if _, ok := set[forbidden]; ok {
			t.Fatalf("$set must not write %s", forbidden)
		}
	}
	unset, _ := update["$unset"].(bson.M)
	for _, forbidden := range []string{"_id", "publishedRevision"} {
		if _, ok := unset[forbidden]; ok {
			t.Fatalf("$unset must never remove %s", forbidden)
		}
	}
	keys := make([]string, 0, len(unset))
	for k := range unset {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Needs no service: the update document itself must remove cleared fields.
func TestSaveUpdateUnsetsClearedOptionalFields(t *testing.T) {
	now := time.Now().UTC()
	full := &models.Game{ID: "g", Revision: 2, Players: []models.Player{}, Missions: []models.LobbyMission{}, Eliminations: []models.Elimination{},
		StartedAt: &now, EndedAt: &now, WinnerID: "w", WinnerKind: "team", Mode: models.ModeTeam, Teams: []models.Team{{ID: "t", Name: "Red"}},
		Timer: &models.Timer{DurationMinutes: 45}, EndReason: "timer", Result: &models.GameResult{}, Withdrawals: []models.Withdrawal{{PlayerID: "p"}}}
	if got := unsetKeys(t, full); len(got) != 0 {
		t.Fatalf("a fully populated game should unset nothing, got %v", got)
	}

	cleared := *full
	cleared.Timer = nil
	cleared.Teams = nil
	if got := unsetKeys(t, &cleared); !reflect.DeepEqual(got, []string{"teams", "timer"}) {
		t.Fatalf("cleared timer and teams must be unset, got %v", got)
	}

	bare := &models.Game{ID: "g", Revision: 1}
	want := []string{"endReason", "endedAt", "mode", "result", "startedAt", "teams", "timer", "winnerId", "winnerKind", "withdrawals"}
	if got := unsetKeys(t, bare); !reflect.DeepEqual(got, want) {
		t.Fatalf("a bare game must unset every optional field\n got %v\nwant %v", got, want)
	}
}

// Real MongoDB: set MONGODB_TEST_URI (for example mongodb://localhost:27017)
// to run it. It uses a throwaway database and skips otherwise, so
// `go test ./...` still needs no running services.
func TestSaveClearsOptionalFieldsInMongoDB(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set; skipping real MongoDB persistence test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(5 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	db := client.Database(fmt.Sprintf("elimin8_test_%d", time.Now().UnixNano()))
	defer func() { _ = db.Drop(context.Background()) }()
	repo := NewGameRepository(db)
	if err = repo.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}

	g := &models.Game{ID: "persist", InvitationCode: "INV", Status: "lobby", Revision: 1, Players: []models.Player{}, Missions: []models.LobbyMission{}, Eliminations: []models.Elimination{},
		Mode: models.ModeTeam, Teams: []models.Team{{ID: "r", Name: "Red"}, {ID: "b", Name: "Blue"}}, Timer: &models.Timer{DurationMinutes: 45}}
	if err = repo.Insert(ctx, g); err != nil {
		t.Fatal(err)
	}
	// Publish revision 1 so we can prove Save leaves the outbox alone.
	if err = repo.Published(ctx, g.ID, 1); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.Get(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Timer == nil || len(loaded.Teams) != 2 {
		t.Fatalf("setup failed: %+v", loaded)
	}

	// Clear the timer and delete every team, like the service does.
	loaded.Timer, loaded.Teams = nil, nil
	loaded.Revision++
	if err = repo.Save(ctx, loaded, 1); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Get(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Timer != nil {
		t.Fatalf("cleared timer still stored: %+v", after.Timer)
	}
	if len(after.Teams) != 0 {
		t.Fatalf("deleted teams still stored: %+v", after.Teams)
	}
	var raw bson.M
	if err = db.Collection("games").FindOne(ctx, bson.M{"_id": g.ID}).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"timer", "teams"} {
		if _, present := raw[key]; present {
			t.Fatalf("%q is still a field of the stored document", key)
		}
	}
	// The outbox is untouched: revision moved on, publishedRevision did not.
	if after.Revision != 2 || after.PublishedRevision != 1 {
		t.Fatalf("revision=%d publishedRevision=%d, want 2 and 1", after.Revision, after.PublishedRevision)
	}
	pending, err := repo.Pending(ctx)
	if err != nil || len(pending) != 1 || pending[0].ID != g.ID {
		t.Fatalf("expected the game to be pending publication: %v %v", pending, err)
	}

	// A stale writer still loses: revision conflict detection is unchanged.
	stale := *after
	stale.Revision = 99
	if err = repo.Save(ctx, &stale, 1); err != services.ErrConflict {
		t.Fatalf("stale save: got %v, want ErrConflict", err)
	}

	// Setting the fields again after clearing works.
	after.Timer = &models.Timer{DurationMinutes: 10}
	after.Revision++
	if err = repo.Save(ctx, after, 2); err != nil {
		t.Fatal(err)
	}
	again, _ := repo.Get(ctx, g.ID)
	if again.Timer == nil || again.Timer.DurationMinutes != 10 {
		t.Fatalf("timer not re-set: %+v", again.Timer)
	}
}
