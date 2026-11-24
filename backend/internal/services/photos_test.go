package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/storage"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type photoStore struct {
	objects     map[string][]byte
	putError    error
	deleteError error
	afterPut    func()
	afterGet    func()
}

func (s *photoStore) Put(_ context.Context, key string, data []byte) error {
	if s.putError != nil {
		return s.putError
	}
	s.objects[key] = append([]byte(nil), data...)
	if s.afterPut != nil {
		s.afterPut()
	}
	return nil
}
func (s *photoStore) Get(_ context.Context, key string) ([]byte, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	if s.afterGet != nil {
		s.afterGet()
	}
	return append([]byte(nil), data...), nil
}
func (s *photoStore) Delete(_ context.Context, key string) error {
	if s.deleteError != nil {
		return s.deleteError
	}
	delete(s.objects, key)
	return nil
}
func (s *photoStore) List(_ context.Context, prefix string) ([]storage.Object, error) {
	var objects []storage.Object
	for key := range s.objects {
		if strings.HasPrefix(key, prefix) {
			objects = append(objects, storage.Object{Key: key})
		}
	}
	return objects, nil
}

type photoFixture struct {
	photos        *services.PhotoService
	games         *services.GameService
	repo          *testutil.Memory
	store         *photoStore
	owner, player models.Access
	now           time.Time
}

func newPhotoFixture(t *testing.T) *photoFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	repo := testutil.NewMemory()
	g := &models.Game{ID: "g1", InvitationCode: "INVITE", Status: "lobby", Revision: 1, CreatedAt: now,
		Players: []models.Player{
			{ID: "p0", Name: "Alice", JoinedAt: &now, CredentialVersion: 1},
			{ID: "p1", Name: "Bob", JoinedAt: &now, CredentialVersion: 1},
			{ID: "p2", Name: "Invitee", CredentialVersion: 1},
		},
	}
	if err := repo.Insert(ctx, g); err != nil {
		t.Fatal(err)
	}
	owner := models.Access{Hash: "owner", GameID: g.ID, ExpiresAt: now.Add(time.Hour)}
	player := models.Access{Hash: "player", GameID: g.ID, PlayerID: "p0", Version: 1, ExpiresAt: now.Add(time.Hour)}
	_ = repo.PutAccess(ctx, owner)
	_ = repo.PutAccess(ctx, player)
	store := &photoStore{objects: map[string][]byte{}}
	games := services.NewGameService(repo)
	photos := services.NewPhotoService(games, store)
	f := &photoFixture{photos: photos, games: games, repo: repo, store: store, owner: owner, player: player, now: now}
	photos.Now = func() time.Time { return f.now }
	return f
}
func photoPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 800; x++ {
			im.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 80, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func (f *photoFixture) game(t *testing.T) *models.Game {
	t.Helper()
	g, e := f.repo.Get(context.Background(), "g1")
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func (f *photoFixture) save(t *testing.T, g *models.Game) {
	t.Helper()
	r := g.Revision
	g.Revision++
	if e := f.repo.Save(context.Background(), g, r); e != nil {
		t.Fatal(e)
	}
}

func TestPhotoLifecyclePrivacyAndPersistence(t *testing.T) {
	f := newPhotoFixture(t)
	ctx := context.Background()
	data := photoPNG(t)
	inviteURL, err := f.photos.Put(ctx, f.owner, "p2", data)
	if err != nil {
		t.Fatal(err)
	}
	g := f.game(t)
	inviteID := g.Players[2].PhotoID
	if _, err = f.photos.Get(ctx, "g1", inviteID, nil); !errors.Is(err, services.ErrNotFound) {
		t.Fatalf("anonymous invitee photo: %v", err)
	}
	if _, err = f.photos.Get(ctx, "g1", inviteID, &f.player); !errors.Is(err, services.ErrNotFound) {
		t.Fatalf("player reads unjoined invitee: %v", err)
	}
	if _, err = f.photos.Get(ctx, "g1", inviteID, &f.owner); err != nil {
		t.Fatal(err)
	}
	if inviteURL == "" || !strings.HasPrefix(inviteURL, "/api/games/g1/photos/") {
		t.Fatal(inviteURL)
	}

	notified := 0
	f.games.Notify = func() { notified++ }
	photoURL, err := f.photos.Put(ctx, f.player, "p0", data)
	if err != nil {
		t.Fatal(err)
	}
	g = f.game(t)
	firstID := g.Players[0].PhotoID
	if len(firstID) != 32 || photoURL != services.PhotoURL("g1", firstID) || notified != 1 {
		t.Fatalf("url/id/notification: %q %d", photoURL, notified)
	}
	stored, err := f.photos.Get(ctx, "g1", firstID, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(stored))
	if err != nil || cfg.Width != 400 || cfg.Height != 200 {
		t.Fatalf("stored dimensions: %+v %v", cfg, err)
	}
	own := services.View(g, f.player)
	if own.Me == nil || own.Me.PhotoURL != photoURL {
		t.Fatalf("own photo missing: %+v", own.Me)
	}
	organizer := services.View(g, f.owner)
	if organizer.Players[0].PhotoURL != photoURL {
		t.Fatal("organizer photo missing")
	}
	encoded, _ := json.Marshal(own)
	for _, secret := range []string{"photoId", "s3://", "secret-key"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("private storage data in view")
		}
	}

	if _, err = f.photos.Put(ctx, f.owner, "p0", data); err != nil {
		t.Fatal(err)
	}
	if _, err = f.photos.Get(ctx, "g1", firstID, nil); !errors.Is(err, services.ErrNotFound) {
		t.Fatalf("replaced image available: %v", err)
	}
	if _, ok := f.store.objects["games/g1/photos/"+firstID]; ok {
		t.Fatal("old object not deleted")
	}
	if err = f.photos.Delete(ctx, f.player, "p0"); err != nil {
		t.Fatal(err)
	}
	if err = f.photos.Delete(ctx, f.player, "p0"); err != nil {
		t.Fatal("delete must be idempotent", err)
	}
	g = f.game(t)
	if g.Players[0].PhotoID != "" {
		t.Fatal("photo reference not cleared")
	}
	encodedBSON, _ := bson.Marshal(g.Players[0])
	var fields bson.M
	_ = bson.Unmarshal(encodedBSON, &fields)
	if value, ok := fields["photoId"]; !ok || value != "" {
		t.Fatal("empty photo reference would not overwrite MongoDB field", fields)
	}
	g.Status = "active"
	g.StartedAt = &f.now
	g.Players[0].Included = true
	g.Players[1].Included = true
	f.save(t, g)
	if _, err = f.photos.Get(ctx, "g1", inviteID, nil); !errors.Is(err, services.ErrNotFound) {
		t.Fatalf("excluded invitee available after start: %v", err)
	}
	for _, p := range services.View(f.game(t), f.player).Participants {
		j, _ := json.Marshal(p)
		if strings.Contains(string(j), "photo") {
			t.Fatal("shared participant contract changed")
		}
	}
}

func TestPhotoWritesRevalidateCredentialsAndGameState(t *testing.T) {
	data := photoPNG(t)
	ctx := context.Background()
	tests := []struct {
		name    string
		prepare func(*photoFixture) models.Access
		target  string
		want    error
	}{
		{"another player's photo", func(f *photoFixture) models.Access { return f.player }, "p1", services.ErrForbidden},
		{"forged organizer role", func(f *photoFixture) models.Access { a := f.player; a.PlayerID = ""; return a }, "p1", services.ErrUnauthorized},
		{"expired access", func(f *photoFixture) models.Access { f.now = f.now.Add(2 * time.Hour); return f.player }, "p0", services.ErrUnauthorized},
		{"revoked version", func(f *photoFixture) models.Access {
			g := f.game(t)
			g.Players[0].CredentialVersion++
			f.save(t, g)
			return f.player
		}, "p0", services.ErrUnauthorized},
		{"unknown access", func(f *photoFixture) models.Access { a := f.owner; a.Hash = "unknown"; return a }, "p0", services.ErrUnauthorized},
		{"ended game", func(f *photoFixture) models.Access {
			g := f.game(t)
			g.Status = "ended"
			g.EndedAt = &f.now
			f.save(t, g)
			return f.owner
		}, "p0", services.ErrGameEnded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newPhotoFixture(t)
			a := tc.prepare(f)
			if _, err := f.photos.Put(ctx, a, tc.target, data); !errors.Is(err, tc.want) {
				t.Fatalf("Put=%v want %v", err, tc.want)
			}
			if err := f.photos.Delete(ctx, a, tc.target); !errors.Is(err, tc.want) {
				t.Fatalf("Delete=%v want %v", err, tc.want)
			}
			if len(f.store.objects) != 0 {
				t.Fatal("rejected write stored data")
			}
		})
	}
}

func TestPhotoMutationConflictRechecksRevocationAndExpiry(t *testing.T) {
	for _, expire := range []bool{false, true} {
		name := "revocation"
		if expire {
			name = "expiry"
		}
		t.Run(name, func(t *testing.T) {
			f := newPhotoFixture(t)
			data := photoPNG(t)
			ctx := context.Background()
			f.repo.BeforeSave = func(*models.Game) {
				f.repo.BeforeSave = nil
				g := f.game(t)
				if expire {
					f.now = f.now.Add(2 * time.Hour)
				} else {
					g.Players[0].CredentialVersion++
				}
				f.save(t, g)
			}
			if _, err := f.photos.Put(ctx, f.player, "p0", data); !errors.Is(err, services.ErrUnauthorized) {
				t.Fatalf("retry accepted invalidated access: %v", err)
			}
			if f.game(t).Players[0].PhotoID != "" || len(f.store.objects) != 0 {
				t.Fatal("failed retry persisted reference or staged object")
			}
		})
	}
}

func TestPhotoStoreAndConcurrentReadFailures(t *testing.T) {
	f := newPhotoFixture(t)
	ctx := context.Background()
	data := photoPNG(t)
	url, err := f.photos.Put(ctx, f.player, "p0", data)
	if err != nil {
		t.Fatal(err)
	}
	id := f.game(t).Players[0].PhotoID
	f.store.putError = errors.New("store offline")
	if _, err = f.photos.Put(ctx, f.player, "p0", data); err == nil {
		t.Fatal("upload failure accepted")
	}
	if f.game(t).Players[0].PhotoID != id {
		t.Fatal("failed upload changed reference")
	}
	f.store.putError = nil
	f.store.afterGet = func() { f.store.afterGet = nil; g := f.game(t); g.Players[0].PhotoID = "replacement"; f.save(t, g) }
	if _, err = f.photos.Get(ctx, "g1", id, nil); !errors.Is(err, services.ErrNotFound) {
		t.Fatalf("concurrent replacement read: %v", err)
	}
	if url == "" {
		t.Fatal("missing URL")
	}
}

func TestPhotoReadRetentionBoundaryAndCancelledLobby(t *testing.T) {
	f := newPhotoFixture(t)
	ctx := context.Background()
	if _, err := f.photos.Put(ctx, f.player, "p0", photoPNG(t)); err != nil {
		t.Fatal(err)
	}
	g := f.game(t)
	id := g.Players[0].PhotoID
	g.Status = "ended"
	g.EndedAt = &f.now
	f.save(t, g)
	if _, err := f.photos.Get(ctx, "g1", id, &f.player); err != nil {
		t.Fatal("joined player cannot read cancelled lobby photo", err)
	}
	f.now = f.now.Add(24*time.Hour - time.Nanosecond)
	if _, err := f.photos.Get(ctx, "g1", id, nil); !errors.Is(err, services.ErrNotFound) {
		t.Fatal("nonparticipant cancelled-lobby photo publicly available", err)
	}
	// Include the player to exercise the exact public retention boundary.
	g = f.game(t)
	g.Players[0].Included = true
	f.save(t, g)
	if _, err := f.photos.Get(ctx, "g1", id, nil); err != nil {
		t.Fatal("photo expired early", err)
	}
	f.now = f.now.Add(time.Nanosecond)
	if _, err := f.photos.Get(ctx, "g1", id, nil); !errors.Is(err, services.ErrNotFound) {
		t.Fatalf("photo available at expiry: %v", err)
	}
}
