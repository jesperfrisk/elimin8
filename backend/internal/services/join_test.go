package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

type lobby struct {
	s          *services.GameService
	repo       *testutil.Memory
	id         string
	invitation string
	owner      models.Access
}

func newLobby(t *testing.T) lobby {
	t.Helper()
	s, repo := newTestService(t)
	return lobbyOn(t, s, repo)
}

func lobbyOn(t *testing.T, s *services.GameService, repo *testutil.Memory) lobby {
	t.Helper()
	ctx := context.Background()
	admission, err := s.Create(ctx, "Self join", "")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := s.Authorize(ctx, admission.Token)
	if err != nil {
		t.Fatal(err)
	}
	return lobby{s: s, repo: repo, id: admission.Game.ID, invitation: admission.Game.InvitationCode, owner: owner}
}

func (l lobby) game(t *testing.T) *models.Game {
	t.Helper()
	g, err := l.repo.Get(context.Background(), l.id)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestNameKeyTreatsEquivalentNamesAsTheSameAndDocumentsItsLimit(t *testing.T) {
	same := [][]string{
		{"Alex", " alex ", "ALEX", " Alex\t", "Ａｌｅｘ", "Al​ex", "A­lex"},
		{"Alex B", "alex  b", "ALEX B", "Alex\tB"},
		{"Café", "Café", "CAFÉ"}, // é as one character, or e plus an accent
		{"Straße", "STRASSE", "strasse"},
		{"Ｊｏｓé", "josé"},
	}
	for _, group := range same {
		want := services.NameKey(group[0])
		if want == "" {
			t.Fatalf("empty key for %q", group[0])
		}
		for _, name := range group[1:] {
			if got := services.NameKey(name); got != want {
				t.Errorf("%q and %q should collide: %q vs %q", group[0], name, want, got)
			}
		}
	}
	different := [][2]string{
		{"Alex", "Alexa"},
		{"Alex B", "AlexB"},
		{"Alex", "Alex2"},
		// Documented limit: look-alike letters from another alphabet are not detected.
		{"Alex", "Аlex"}, // Cyrillic capital A
	}
	for _, pair := range different {
		if services.NameKey(pair[0]) == services.NameKey(pair[1]) {
			t.Errorf("%q and %q must not collide", pair[0], pair[1])
		}
	}
	// A name with nothing visible has no key at all.
	if services.NameKey("​ \t­") != "" {
		t.Fatal("invisible-only name has a key")
	}
}

func TestJoinRegistersAJoinedPlayerWithStableNumberAndWorkingRecoveryCode(t *testing.T) {
	ctx := context.Background()
	l := newLobby(t)
	first, err := l.s.Join(ctx, l.invitation, "  Alex  ")
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == "" || first.Role != "player" || len(first.RecoveryCode) != 8 || first.Game.Me == nil || first.Game.Me.Name != "Alex" {
		t.Fatalf("unexpected result: %+v", first)
	}
	// The invitation code may be typed loosely, like the login does.
	second, err := l.s.Join(ctx, strings.ToLower(l.invitation[:4])+" "+l.invitation[4:], "Sam")
	if err != nil {
		t.Fatal(err)
	}
	// Organizer pre-add stays additive and keeps numbering after the self-joined players.
	if _, err = l.s.AddPlayers(ctx, l.owner, []string{"Jordan"}); err != nil {
		t.Fatal(err)
	}
	g := l.game(t)
	if len(g.Players) != 3 {
		t.Fatalf("players = %d", len(g.Players))
	}
	for i, want := range []struct {
		name   string
		number int
	}{{"Alex", 1}, {"Sam", 2}, {"Jordan", 3}} {
		p := g.Players[i]
		if p.Name != want.name || p.Number != want.number {
			t.Fatalf("player %d = %q #%d, want %q #%d", i, p.Name, p.Number, want.name, want.number)
		}
	}
	alex := g.Players[0]
	if alex.JoinedAt == nil || alex.Included || alex.CredentialVersion != 1 || alex.CodeHash == "" || strings.Contains(alex.CodeHash, first.RecoveryCode) {
		t.Fatalf("stored player is wrong: %+v", alex)
	}
	if g.Players[2].JoinedAt != nil { // the organizer's invitee has not joined
		t.Fatal("pre-added invitee is marked joined")
	}
	if first.Game.Me.ID != alex.ID || second.Game.Me.Name != "Sam" {
		t.Fatal("admissions are for the wrong players")
	}
	// The recovery code is the existing private code: it signs the same player back in.
	again, err := l.s.Login(ctx, l.invitation, first.RecoveryCode, "player")
	if err != nil || again.Game.Me == nil || again.Game.Me.ID != alex.ID {
		t.Fatalf("recovery code did not return the same player: %+v %v", again, err)
	}
	// A name is never a credential.
	if _, err = l.s.Login(ctx, l.invitation, "Alex", "player"); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("login by name: %v", err)
	}
}

func TestJoinNamesAreUniqueAcrossAllPlayersAndNeverReclaimed(t *testing.T) {
	ctx := context.Background()
	l := newLobby(t)
	if _, err := l.s.Join(ctx, l.invitation, "Alex"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alex", " ALEX ", "Ａｌｅｘ", "Al​ex"} {
		if _, err := l.s.Join(ctx, l.invitation, name); !errors.Is(err, services.ErrNameTaken) {
			t.Fatalf("%q: %v", name, err)
		}
	}
	// An organizer-added invitee holds the name too: a self-join cannot claim it, only their code can.
	if _, err := l.s.AddPlayers(ctx, l.owner, []string{"Bob"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.s.Join(ctx, l.invitation, "bob"); !errors.Is(err, services.ErrNameTaken) {
		t.Fatalf("self-join claimed an invitee's name: %v", err)
	}
	// Legacy duplicates stay untouched and still block the name.
	g := l.game(t)
	g.Players = append(g.Players, models.Player{ID: "legacy1", Name: "Dup", CodeHash: "h1", CredentialVersion: 1}, models.Player{ID: "legacy2", Name: "dup", CodeHash: "h2", CredentialVersion: 1})
	l.repo.Games[l.id] = g
	if _, err := l.s.Join(ctx, l.invitation, "DUP"); !errors.Is(err, services.ErrNameTaken) {
		t.Fatalf("legacy name: %v", err)
	}
	got := l.game(t)
	if got.Players[2].Name != "Dup" || got.Players[3].Name != "dup" {
		t.Fatal("historical players were renamed")
	}
	if len(got.Players) != 4 {
		t.Fatalf("a refused join changed the roster: %d players", len(got.Players))
	}
}

func TestJoinRejectsInvalidInputAndUnknownOrClosedLobbies(t *testing.T) {
	ctx := context.Background()
	l := newLobby(t)
	for _, name := range []string{"", "   ", "​­", strings.Repeat("a", 81), "bad\xffutf8"} {
		if _, err := l.s.Join(ctx, l.invitation, name); !errors.Is(err, services.ErrInvalid) {
			t.Errorf("%q: %v", name, err)
		}
	}
	if _, err := l.s.Join(ctx, strings.Repeat("A", 33), "Alex"); !errors.Is(err, services.ErrInvalid) {
		t.Fatalf("long invitation: %v", err)
	}
	if _, err := l.s.Join(ctx, "NOSUCHCD", "Alex"); !errors.Is(err, services.ErrNotFound) {
		t.Fatalf("unknown lobby: %v", err)
	}
	// 80 characters is allowed.
	if _, err := l.s.Join(ctx, l.invitation, strings.Repeat("a", 80)); err != nil {
		t.Fatalf("80 characters: %v", err)
	}
	for _, status := range []string{"active", "ended"} {
		g := l.game(t)
		g.Status = status
		l.repo.Games[l.id] = g
		if _, err := l.s.Join(ctx, l.invitation, "Late"); !errors.Is(err, services.ErrLobbyClosed) {
			t.Fatalf("%s: %v", status, err)
		}
	}
}

func TestConcurrentSameNameJoinsAcrossInstancesCreateOnePlayer(t *testing.T) {
	ctx := context.Background()
	s1, repo := newTestService(t)
	s2 := services.NewGameService(repo) // a second API instance on the same database
	l := lobbyOn(t, s1, repo)
	repo.BeforeSave = func(*models.Game) { time.Sleep(2 * time.Millisecond) } // widen the race window
	variants := []string{"Alex", " alex ", "ALEX", "Ａｌｅｘ", "alex", "Alex ", "  ALEX", "aLeX", "Alex", "alex ", "ALEX ", "Ａｌｅｘ "}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var wins int
	var other []error
	for i, name := range variants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc := s1
			if i%2 == 1 {
				svc = s2
			}
			_, err := svc.Join(ctx, l.invitation, name)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if !errors.Is(err, services.ErrNameTaken) {
				other = append(other, err)
			}
		}()
	}
	wg.Wait()
	repo.BeforeSave = nil
	if wins != 1 || len(other) != 0 {
		t.Fatalf("wins=%d other errors=%v", wins, other)
	}
	if n := len(l.game(t).Players); n != 1 {
		t.Fatalf("%d players were created for one name", n)
	}
}

func TestConcurrentDifferentNamesAllJoinWithUniqueStableNumbers(t *testing.T) {
	ctx := context.Background()
	s1, repo := newTestService(t)
	s2 := services.NewGameService(repo)
	l := lobbyOn(t, s1, repo)
	repo.BeforeSave = func(*models.Game) { time.Sleep(time.Millisecond) }
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc := s1
			if i%2 == 1 {
				svc = s2
			}
			_, errs[i] = svc.Join(ctx, l.invitation, "Player "+string(rune('A'+i)))
		}()
	}
	wg.Wait()
	repo.BeforeSave = nil
	for i, err := range errs {
		if err != nil {
			t.Fatalf("join %d: %v", i, err)
		}
	}
	seenNumbers := map[int]bool{}
	for _, p := range l.game(t).Players {
		if p.Number < 1 || p.Number > n || seenNumbers[p.Number] {
			t.Fatalf("numbers are not unique and stable: %+v", l.game(t).Players)
		}
		seenNumbers[p.Number] = true
	}
	if len(seenNumbers) != n {
		t.Fatalf("%d players, want %d", len(seenNumbers), n)
	}
}

func TestJoinRacingStartEitherJoinsTheFrozenRosterOrIsRefused(t *testing.T) {
	ctx := context.Background()
	s1, repo := newTestService(t)
	s2 := services.NewGameService(repo)
	l := lobbyOn(t, s1, repo)
	for _, name := range []string{"First", "Second"} {
		if _, err := s1.Join(ctx, l.invitation, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s1.AddMissions(ctx, l.owner, []string{"Task"}); err != nil {
		t.Fatal(err)
	}
	repo.BeforeSave = func(*models.Game) { time.Sleep(time.Millisecond) }
	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc := s1
			if i%2 == 1 {
				svc = s2
			}
			_, errs[i] = svc.Join(ctx, l.invitation, "Late "+string(rune('A'+i)))
		}()
	}
	wg.Add(1)
	var startErr error
	go func() {
		defer wg.Done()
		time.Sleep(4 * time.Millisecond)
		startErr = s2.Transition(ctx, l.owner, "start")
	}()
	wg.Wait()
	repo.BeforeSave = nil
	if startErr != nil {
		t.Fatal(startErr)
	}
	g := l.game(t)
	joined := 0
	for _, err := range errs {
		switch {
		case err == nil:
			joined++
		case errors.Is(err, services.ErrLobbyClosed):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	// Everyone who got in did so before the roster froze, and the game started consistently.
	if g.Status != "active" || len(g.Players) != 2+joined {
		t.Fatalf("status=%s players=%d joined=%d", g.Status, len(g.Players), joined)
	}
	for _, p := range g.Players {
		if !p.Included || p.VictimID == "" || p.Mission == "" {
			t.Fatalf("a registered player missed the start: %+v", p)
		}
	}
}

func TestJoinResultProjectionsAndRecoveryCodeShownOnce(t *testing.T) {
	ctx := context.Background()
	l := newLobby(t)
	other, err := l.s.Join(ctx, l.invitation, "Other")
	if err != nil {
		t.Fatal(err)
	}
	res, err := l.s.Join(ctx, l.invitation, "Alex")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(res)
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	for _, key := range []string{"token", "expiresAt", "role", "clientPrefix", "topic", "game", "recoveryCode"} {
		if raw[key] == nil {
			t.Fatalf("missing %q in %s", key, data)
		}
	}
	if raw["managementCode"] != nil {
		t.Fatal("a player admission carries a management code")
	}
	// Role-scoped: no roster, no one else's code, no hashes or assignments.
	banned := regexp.MustCompile(`(?i)codeHash|organizerHash|hash|mission|victim|` + other.RecoveryCode + `|"players"`)
	body := strings.ReplaceAll(string(data), `"recoveryCode":"`+res.RecoveryCode+`"`, "")
	if m := banned.FindString(body); m != "" {
		t.Fatalf("join result leaks %q: %s", m, data)
	}
	// The recovery code appears in no later view of the game.
	view, _ := json.Marshal(services.View(l.game(t), models.Access{GameID: l.id, PlayerID: res.Game.Me.ID, Version: 1}))
	if strings.Contains(string(view), res.RecoveryCode) || strings.Contains(string(view), "recoveryCode") {
		t.Fatalf("a later view shows the recovery code: %s", view)
	}
}

func TestJoinResumeReturnsTheSamePlayerOnlyForAValidSameGameBearer(t *testing.T) {
	ctx := context.Background()
	l := newLobby(t)
	other := newLobbyOn(t, l)
	res, err := l.s.Join(ctx, l.invitation, "Alex")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := other.s.Join(ctx, other.invitation, "Zed")
	if err != nil {
		t.Fatal(err)
	}
	adm, err := l.s.JoinResume(ctx, l.invitation, res.Token)
	if err != nil {
		t.Fatal(err)
	}
	if adm.Token == res.Token || adm.Role != "player" || adm.Game.Me == nil || adm.Game.Me.ID != res.Game.Me.ID {
		t.Fatalf("resume returned a different identity: %+v", adm)
	}
	if n := len(l.game(t).Players); n != 1 {
		t.Fatalf("resume created a player: %d", n)
	}
	// The renewed token authorizes the same player.
	if a, _, err := l.s.Authorize(ctx, adm.Token); err != nil || a.PlayerID != res.Game.Me.ID {
		t.Fatalf("renewed token: %v", err)
	}
	// Other proof never claims anything.
	if _, err := l.s.JoinResume(ctx, l.invitation, foreign.Token); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("other game's player bearer: %v", err)
	}
	if _, err := l.s.JoinResume(ctx, l.invitation, "garbage"); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("garbage bearer: %v", err)
	}
	organizerToken, _ := l.s.Create(ctx, "Another game", "")
	if _, err := l.s.JoinResume(ctx, l.invitation, organizerToken.Token); !errors.Is(err, services.ErrForbidden) {
		t.Fatalf("an organizer bearer is not player proof: %v", err)
	}
	if _, err := l.s.JoinResume(ctx, "NOSUCHCD", res.Token); !errors.Is(err, services.ErrNotFound) {
		t.Fatalf("unknown invitation: %v", err)
	}
}

// newLobbyOn creates a second, separate game on the same service and repository.
func newLobbyOn(t *testing.T, l lobby) lobby { return lobbyOn(t, l.s, l.repo) }

func TestResumeKeepsTheRememberedCredentialLinkAndItsDeadline(t *testing.T) {
	ctx := context.Background()
	l := newLobby(t)
	res, err := l.s.Join(ctx, l.invitation, "Alex")
	if err != nil {
		t.Fatal(err)
	}
	secret, session, err := l.s.IssuePlayerSession(ctx, res.Token)
	if err != nil {
		t.Fatal(err)
	}
	fromCookie, err := l.s.ResumePlayerSession(ctx, l.id, secret)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := l.s.JoinResume(ctx, l.invitation, fromCookie.Token)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.ExpiresAt.After(session.ExpiresAt) {
		t.Fatal("a resumed admission outlives the 30-day credential")
	}
	// Signing out (revoking the credential) kills the resumed admission too.
	if err := l.s.ForgetPlayerSession(ctx, l.id, secret); err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{"original": res.Token, "exchanged": fromCookie.Token, "resumed": renewed.Token} {
		if _, _, err := l.s.Authorize(ctx, token); !errors.Is(err, services.ErrUnauthorized) {
			t.Fatalf("%s admission survived revocation: %v", name, err)
		}
	}
}

func TestOrganizerResetInvalidatesTheRecoveryCodeAndRememberedCredential(t *testing.T) {
	ctx := context.Background()
	l := newLobby(t)
	res, err := l.s.Join(ctx, l.invitation, "Alex")
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := l.s.IssuePlayerSession(ctx, res.Token)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := l.s.ResetCode(ctx, l.owner, res.Game.Me.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.s.Login(ctx, l.invitation, res.RecoveryCode, "player"); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("old recovery code still works: %v", err)
	}
	if _, err := l.s.ResumePlayerSession(ctx, l.id, secret); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("remembered credential survived the reset: %v", err)
	}
	if _, err := l.s.JoinResume(ctx, l.invitation, res.Token); !errors.Is(err, services.ErrUnauthorized) {
		t.Fatalf("old bearer survived the reset: %v", err)
	}
	back, err := l.s.Login(ctx, l.invitation, reset.Code, "player")
	if err != nil || back.Game.Me.ID != res.Game.Me.ID {
		t.Fatalf("the new code did not restore the original player: %v", err)
	}
}

// failingAccess makes the sign-in step fail after the registration has committed.
type failingAccess struct {
	*testutil.Memory
	fail bool
}

func (f *failingAccess) PutAccess(ctx context.Context, a models.Access) error {
	if f.fail {
		return errors.New("storage unavailable")
	}
	return f.Memory.PutAccess(ctx, a)
}

func TestRegistrationThatCommitsButCannotSignInReportsItAndKeepsRecoveryPossible(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewMemory()
	repo := &failingAccess{Memory: mem}
	s := services.NewGameService(repo)
	l := lobbyOn(t, s, mem)
	repo.fail = true
	res, err := s.Join(ctx, l.invitation, "Alex")
	var incomplete *services.RegistrationIncompleteError
	if !errors.As(err, &incomplete) || len(incomplete.RecoveryCode) != 8 || incomplete.RecoveryCode != res.RecoveryCode {
		t.Fatalf("want an incomplete registration carrying the code, got %v", err)
	}
	// The player exists, so a retry is honestly refused instead of creating a second one...
	repo.fail = false
	if _, err := s.Join(ctx, l.invitation, "Alex"); !errors.Is(err, services.ErrNameTaken) {
		t.Fatalf("retry: %v", err)
	}
	if n := len(l.game(t).Players); n != 1 {
		t.Fatalf("%d players", n)
	}
	// ...and the code returned in the error signs the same player in.
	back, err := s.Login(ctx, l.invitation, incomplete.RecoveryCode, "player")
	if err != nil || back.Game.Me.Name != "Alex" {
		t.Fatalf("recovery after a failed sign-in: %v", err)
	}
}
