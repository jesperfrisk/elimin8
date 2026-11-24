package services_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/storage"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

type retentionRepo struct {
	*testutil.Memory
	ids         []string
	mu          sync.Mutex
	cursors     []string
	discoverErr error
	forceIDs    bool
}

func (r *retentionRepo) DuePhotoGames(ctx context.Context, before time.Time, after string, limit int) ([]string, error) {
	r.mu.Lock()
	r.cursors = append(r.cursors, after)
	r.mu.Unlock()
	if r.discoverErr != nil {
		return nil, r.discoverErr
	}
	var ids []string
	for _, id := range r.ids {
		if id <= after {
			continue
		}
		g, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if r.forceIDs || (g.Status == "ended" && g.EndedAt != nil && !g.EndedAt.After(before)) {
			ids = append(ids, id)
			if len(ids) == limit {
				break
			}
		}
	}
	return ids, nil
}

type retentionStore struct {
	mu             sync.Mutex
	objects        map[string][]byte
	deleteFailures map[string]int
	listFailures   map[string]int
	deleted        []string
	afterList      func()
	beforeList     func(context.Context, string) error
	onDelete       func()
	unrelated      bool
	listLimit      int
}

func newRetentionStore() *retentionStore {
	return &retentionStore{objects: map[string][]byte{}, deleteFailures: map[string]int{}, listFailures: map[string]int{}}
}
func (s *retentionStore) Put(ctx context.Context, key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = append([]byte(nil), data...)
	return ctx.Err()
}
func (s *retentionStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return append([]byte(nil), data...), ctx.Err()
}
func (s *retentionStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.deleteFailures[key] > 0 {
		s.deleteFailures[key]--
		s.mu.Unlock()
		return errors.New("private-storage-key-and-credential")
	}
	delete(s.objects, key)
	s.deleted = append(s.deleted, key)
	hook := s.onDelete
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}
func (s *retentionStore) List(ctx context.Context, prefix string) ([]storage.Object, error) {
	if s.beforeList != nil {
		if err := s.beforeList(ctx, prefix); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	if s.listFailures[prefix] > 0 {
		s.listFailures[prefix]--
		s.mu.Unlock()
		return nil, errors.New("private-storage-list-error")
	}
	var objects []storage.Object
	for key := range s.objects {
		if strings.HasPrefix(key, prefix) {
			objects = append(objects, storage.Object{Key: key})
		}
	}
	if s.unrelated {
		objects = append(objects, storage.Object{Key: "games/active/photos/keep"})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	if s.listLimit > 0 && len(objects) > s.listLimit {
		objects = objects[:s.listLimit]
	}
	hook := s.afterList
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return objects, ctx.Err()
}
func (s *retentionStore) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[key]
	return ok
}
func (s *retentionStore) seed(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = []byte("jpeg")
}
func retentionFixture(t *testing.T, games ...*models.Game) (*services.GameService, *retentionRepo, *retentionStore) {
	t.Helper()
	repo := &retentionRepo{Memory: testutil.NewMemory()}
	for _, g := range games {
		g.InvitationCode = g.ID
		if g.Revision == 0 {
			g.Revision = 1
		}
		if err := repo.Insert(context.Background(), g); err != nil {
			t.Fatal(err)
		}
		repo.ids = append(repo.ids, g.ID)
	}
	sort.Strings(repo.ids)
	return services.NewGameService(repo), repo, newRetentionStore()
}
func endedPhotoGame(id string, at time.Time) *models.Game {
	return &models.Game{ID: id, Status: "ended", EndedAt: &at, Players: []models.Player{{ID: "player", PhotoID: "current", Included: true}}, WinnerID: "winner", EndReason: "survival", PublishedRevision: 1}
}
func newRetention(s *services.GameService, r *retentionRepo, store *retentionStore, now time.Time) *services.PhotoRetentionService {
	job := services.NewPhotoRetentionService(s, r, store, 24*time.Hour)
	job.Now = func() time.Time { return now }
	return job
}

func TestPhotoRetentionBoundaryAndAllEndReasons(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)
	for _, reason := range []string{"", "timer", "survival", "organizer"} {
		t.Run("reason_"+reason, func(t *testing.T) {
			g := endedPhotoGame("ended", cutoff)
			g.EndReason = reason
			s, repo, store := retentionFixture(t, g)
			store.seed("games/ended/photos/current")
			store.seed("games/ended/photos/old-replacement")
			store.seed("games/ended/photos/orphan")
			store.seed("games/unrelated/photos/keep")
			notifications := 0
			s.Notify = func() { notifications++ }
			job := newRetention(s, repo, store, now.Add(-time.Nanosecond))
			if err := job.Sweep(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !store.has("games/ended/photos/current") {
				t.Fatal("deleted before retention boundary")
			}
			job.Now = func() time.Time { return now }
			if err := job.Sweep(context.Background()); err != nil {
				t.Fatal(err)
			}
			got, _ := repo.Get(context.Background(), g.ID)
			if got.Players[0].PhotoID != "" || got.Revision != 2 || notifications != 1 {
				t.Fatalf("references not cleared exactly once: %+v", got)
			}
			if got.EndedAt == nil || !got.EndedAt.Equal(cutoff) || got.EndReason != reason || got.WinnerID != "winner" || got.PublishedRevision != 1 {
				t.Fatal("cleanup changed result/end metadata or outbox state")
			}
			if len(store.deleted) != 3 || !store.has("games/unrelated/photos/keep") {
				t.Fatal("cleanup failed to isolate complete game prefix")
			}
			if err := job.Sweep(context.Background()); err != nil {
				t.Fatal(err)
			}
			again, _ := repo.Get(context.Background(), g.ID)
			if again.Revision != 2 || notifications != 1 {
				t.Fatal("idempotent cleanup performed another game write")
			}
		})
	}
}

func TestPhotoRetentionRestartFindsLateOrphan(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	g := endedPhotoGame("ended", now.Add(-48*time.Hour))
	s, repo, store := retentionFixture(t, g)
	job := newRetention(s, repo, store, now)
	if err := job.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.seed("games/ended/photos/late-orphan")
	restarted := newRetention(services.NewGameService(repo), repo, store, now)
	if err := restarted.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.has("games/ended/photos/late-orphan") {
		t.Fatal("restart skipped orphan in already-cleaned game")
	}
}

func TestPhotoRetentionRejectsUnexpiredDiscoveryResults(t *testing.T) {
	now := time.Now().UTC()
	cases := []*models.Game{
		{ID: "active", Status: "active", EndedAt: nil},
		{ID: "lobby", Status: "lobby", EndedAt: nil},
		endedPhotoGame("recent", now.Add(-23*time.Hour)),
		{ID: "missing-time", Status: "ended"},
	}
	s, repo, store := retentionFixture(t, cases...)
	repo.forceIDs = true
	for _, g := range cases {
		store.seed("games/" + g.ID + "/photos/keep")
	}
	if err := newRetention(s, repo, store, now).Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.deleted) != 0 {
		t.Fatal("stale discovery deleted unexpired photos")
	}
}

func TestPhotoRetentionPartialFailureRetriesWithoutLeakingErrors(t *testing.T) {
	now := time.Now().UTC()
	first := endedPhotoGame("a", now.Add(-48*time.Hour))
	second := endedPhotoGame("b", now.Add(-48*time.Hour))
	s, repo, store := retentionFixture(t, first, second)
	store.seed("games/a/photos/current")
	store.seed("games/a/photos/orphan")
	store.seed("games/b/photos/current")
	store.deleteFailures["games/a/photos/current"] = 1
	job := newRetention(s, repo, store, now)
	err := job.Sweep(context.Background())
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "games/") {
		t.Fatalf("failure missing or unsafe: %v", err)
	}
	a, _ := repo.Get(context.Background(), "a")
	b, _ := repo.Get(context.Background(), "b")
	if a.Players[0].PhotoID == "" || b.Players[0].PhotoID != "" {
		t.Fatal("failure cleared reference or blocked later game")
	}
	if !store.has("games/a/photos/current") || store.has("games/a/photos/orphan") || store.has("games/b/photos/current") {
		t.Fatal("partial cleanup incorrect")
	}
	if err := job.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, _ = repo.Get(context.Background(), "a")
	if a.Players[0].PhotoID != "" || store.has("games/a/photos/current") {
		t.Fatal("failed object did not retry")
	}
	store.seed("games/a/photos/late")
	store.listFailures["games/a/photos/"] = 1
	if err := job.Sweep(context.Background()); err == nil {
		t.Fatal("listing failure hidden")
	}
	if err := job.Sweep(context.Background()); err != nil || store.has("games/a/photos/late") {
		t.Fatal("listing did not retry")
	}
	repo.discoverErr = errors.New("credential")
	if err := job.Sweep(context.Background()); err == nil || strings.Contains(err.Error(), "credential") {
		t.Fatal("discovery error missing or leaked")
	}
}

func TestPhotoRetentionPaginationDoesNotStarveLaterGames(t *testing.T) {
	now := time.Now().UTC()
	var games []*models.Game
	for i := 0; i < 205; i++ {
		games = append(games, endedPhotoGame(fmt.Sprintf("game%03d", i), now.Add(-48*time.Hour)))
	}
	s, repo, store := retentionFixture(t, games...)
	for _, g := range games {
		store.seed("games/" + g.ID + "/photos/current")
	}
	store.deleteFailures["games/game000/photos/current"] = 10
	if err := newRetention(s, repo, store, now).Sweep(context.Background()); err == nil {
		t.Fatal("expected first object failure")
	}
	if len(repo.cursors) != 3 || repo.cursors[0] != "" || repo.cursors[1] != "game099" || repo.cursors[2] != "game199" {
		t.Fatalf("incorrect paging: %v", repo.cursors)
	}
	if store.has("games/game204/photos/current") || !store.has("games/game000/photos/current") || len(store.deleted) != 204 {
		t.Fatal("early failure starved later pages")
	}
}

func TestPhotoRetentionCancellationAndStartupSweep(t *testing.T) {
	now := time.Now().UTC()
	s, repo, store := retentionFixture(t, endedPhotoGame("ended", now.Add(-48*time.Hour)))
	store.seed("games/ended/photos/current")
	job := newRetention(s, repo, store, now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := job.Sweep(ctx); !errors.Is(err, context.Canceled) || len(repo.cursors) != 0 {
		t.Fatal("cancelled sweep performed discovery")
	}
	job.Run(ctx, time.Minute)
	if len(repo.cursors) != 0 {
		t.Fatal("cancelled job ran startup sweep")
	}
	ctx, cancel = context.WithCancel(context.Background())
	store.afterList = cancel
	if err := job.Sweep(ctx); !errors.Is(err, context.Canceled) || !store.has("games/ended/photos/current") {
		t.Fatal("cancelled listing still deleted object")
	}
	store.afterList = nil
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	deleted := make(chan struct{}, 1)
	store.onDelete = func() {
		select {
		case deleted <- struct{}{}:
		default:
		}
	}
	stopped := make(chan struct{})
	go func() { job.Run(ctx, time.Hour); close(stopped) }()
	select {
	case <-deleted:
	case <-time.After(5 * time.Second):
		t.Fatal("startup sweep waited for interval")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("job ignored cancellation")
	}
}

func TestPhotoRetentionNeverDeletesObjectsOutsideGamePrefix(t *testing.T) {
	now := time.Now().UTC()
	s, repo, store := retentionFixture(t, endedPhotoGame("ended", now.Add(-48*time.Hour)))
	store.seed("games/active/photos/keep")
	store.unrelated = true
	if err := newRetention(s, repo, store, now).Sweep(context.Background()); err == nil {
		t.Fatal("invalid storage response accepted")
	}
	if !store.has("games/active/photos/keep") {
		t.Fatal("deleted unrelated object")
	}
}

type retentionConflictRepo struct {
	*retentionRepo
	once sync.Once
}

func (r *retentionConflictRepo) Save(ctx context.Context, g *models.Game, previous int64) error {
	conflicted := false
	var saveErr error
	r.once.Do(func() {
		current, err := r.Memory.Get(ctx, g.ID)
		if err != nil {
			saveErr = err
			return
		}
		current.Name = "concurrent update"
		current.Revision++
		saveErr = r.Memory.Save(ctx, current, previous)
		conflicted = true
	})
	if saveErr != nil {
		return saveErr
	}
	if conflicted {
		return services.ErrConflict
	}
	return r.Memory.Save(ctx, g, previous)
}
func TestPhotoRetentionRetriesReferenceClearAfterRevisionConflict(t *testing.T) {
	now := time.Now().UTC()
	_, base, store := retentionFixture(t, endedPhotoGame("ended", now.Add(-48*time.Hour)))
	repo := &retentionConflictRepo{retentionRepo: base}
	games := services.NewGameService(repo)
	job := services.NewPhotoRetentionService(games, repo, store, 24*time.Hour)
	job.Now = func() time.Time { return now }
	store.seed("games/ended/photos/current")
	if err := job.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(context.Background(), "ended")
	if got.Name != "concurrent update" || got.Players[0].PhotoID != "" || got.Revision != 3 {
		t.Fatal("reference cleanup failed to preserve concurrent revision")
	}
}
func TestPhotoRetentionConcurrentInstancesAreIdempotent(t *testing.T) {
	now := time.Now().UTC()
	first, repo, store := retentionFixture(t, endedPhotoGame("ended", now.Add(-48*time.Hour)))
	store.seed("games/ended/photos/current")
	firstJob := newRetention(first, repo, store, now)
	secondJob := newRetention(services.NewGameService(repo), repo, store, now)
	errs := make(chan error, 2)
	go func() { errs <- firstJob.Sweep(context.Background()) }()
	go func() { errs <- secondJob.Sweep(context.Background()) }()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	got, _ := repo.Get(context.Background(), "ended")
	if got.Players[0].PhotoID != "" || got.Revision != 2 || store.has("games/ended/photos/current") {
		t.Fatal("concurrent cleanup was not idempotent")
	}
}

func TestPhotoRetentionUsesConfiguredDuration(t *testing.T) {
	now := time.Now().UTC()
	s, repo, store := retentionFixture(t, endedPhotoGame("ended", now.Add(-2*time.Hour)))
	store.seed("games/ended/photos/current")
	longer := services.NewPhotoRetentionService(s, repo, store, 3*time.Hour)
	longer.Now = func() time.Time { return now }
	if err := longer.Sweep(context.Background()); err != nil || !store.has("games/ended/photos/current") {
		t.Fatal("configured longer retention ignored")
	}
	shorter := services.NewPhotoRetentionService(s, repo, store, time.Hour)
	shorter.Now = func() time.Time { return now }
	if err := shorter.Sweep(context.Background()); err != nil || store.has("games/ended/photos/current") {
		t.Fatal("configured shorter retention ignored")
	}
}

func TestPhotoRetentionRunRetriesTransientFailure(t *testing.T) {
	now := time.Now().UTC()
	s, repo, store := retentionFixture(t, endedPhotoGame("ended", now.Add(-48*time.Hour)))
	key := "games/ended/photos/current"
	store.seed(key)
	store.deleteFailures[key] = 1
	done := make(chan struct{}, 1)
	store.onDelete = func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	defer func() { cancel(); <-stopped }()
	go func() { newRetention(s, repo, store, now).Run(ctx, time.Millisecond); close(stopped) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("periodic job did not retry startup failure")
	}
}

func TestPhotoRetentionBoundedListingsProgressAcrossRestart(t *testing.T) {
	now := time.Now().UTC()
	first := endedPhotoGame("a-batched", now.Add(-48*time.Hour))
	first.Players[0].PhotoID = "z-current"
	last := endedPhotoGame("z-other", now.Add(-48*time.Hour))
	games, repo, store := retentionFixture(t, first, last)
	store.listLimit = 2
	for _, id := range []string{"a-old", "b-old", "c-orphan", "d-orphan", "z-current"} {
		store.seed("games/a-batched/photos/" + id)
	}
	store.seed("games/z-other/photos/current")
	job := newRetention(games, repo, store, now)
	for sweep := 1; sweep <= 3; sweep++ {
		if err := job.Sweep(context.Background()); err != nil {
			t.Fatal(err)
		}
		current, _ := repo.Get(context.Background(), "a-batched")
		if sweep < 3 {
			if current.Players[0].PhotoID != "z-current" || !store.has("games/a-batched/photos/z-current") {
				t.Fatal("partial batch prematurely cleared a current photo")
			}
		} else if current.Players[0].PhotoID != "" {
			t.Fatal("completed final batch retained stale reference")
		}
		other, _ := repo.Get(context.Background(), "z-other")
		if other.Players[0].PhotoID != "" || store.has("games/z-other/photos/current") {
			t.Fatal("large prefix starved later game")
		}
		if sweep == 1 {
			if len(store.deleted) != 3 {
				t.Fatal("first sweep did not make bounded progress")
			}
			job = newRetention(services.NewGameService(repo), repo, store, now)
		}
	}
	if len(store.deleted) != 6 {
		t.Fatal("bounded listings left objects behind after repeated sweeps")
	}
}

func TestPhotoRetentionBoundsEachGameAndContinuesAfterTimeout(t *testing.T) {
	now := time.Now().UTC()
	games, repo, store := retentionFixture(t, endedPhotoGame("a-failing", now.Add(-48*time.Hour)), endedPhotoGame("z-next", now.Add(-48*time.Hour)))
	store.seed("games/a-failing/photos/current")
	store.seed("games/z-next/photos/current")
	seen := map[string]context.Context{}
	store.beforeList = func(ctx context.Context, prefix string) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 30*time.Second {
			t.Fatal("purge did not receive a bounded per-game deadline")
		}
		seen[prefix] = ctx
		if prefix == "games/a-failing/photos/" {
			return context.DeadlineExceeded
		}
		return nil
	}
	job := newRetention(games, repo, store, now)
	parent, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := job.Sweep(parent); err == nil {
		t.Fatal("per-game timeout was hidden")
	}
	if parent.Err() != nil {
		t.Fatal("one game timeout cancelled the whole sweep")
	}
	first, _ := repo.Get(context.Background(), "a-failing")
	last, _ := repo.Get(context.Background(), "z-next")
	if first.Players[0].PhotoID == "" || !store.has("games/a-failing/photos/current") {
		t.Fatal("failed game lost its current reference")
	}
	if last.Players[0].PhotoID != "" || store.has("games/z-next/photos/current") {
		t.Fatal("timed-out first game starved later game")
	}
	if len(seen) != 2 {
		t.Fatal("did not visit both games")
	}
	for _, gameCtx := range seen {
		if gameCtx.Err() != context.Canceled {
			t.Fatal("per-game timer was not released")
		}
	}
}
