package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

// failingSessions makes persisting the remember credential fail after registration committed.
type failingSessions struct {
	*testutil.Memory
	fail bool
}

func (f *failingSessions) PutPlayerSession(ctx context.Context, s models.PlayerSession) error {
	if f.fail {
		return errors.New("storage unavailable")
	}
	return f.Memory.PutPlayerSession(ctx, s)
}

type joinEnv struct {
	mux        *http.ServeMux
	svc        *services.GameService
	repo       *failingSessions
	id         string
	invitation string
	owner      string
}

func newJoinEnv(t *testing.T, rateLimit int) joinEnv {
	t.Helper()
	repo := &failingSessions{Memory: testutil.NewMemory()}
	svc := services.NewGameService(repo)
	ctl := NewGameController(svc)
	if rateLimit > 0 {
		ctl.SetRateLimit(rateLimit)
	}
	mux := http.NewServeMux()
	ctl.Register(mux)
	admission, err := svc.Create(context.Background(), "Self join", "")
	if err != nil {
		t.Fatal(err)
	}
	return joinEnv{mux: mux, svc: svc, repo: repo, id: admission.Game.ID, invitation: admission.Game.InvitationCode, owner: admission.Token}
}

func (e joinEnv) do(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, req)
	return w
}

func (e joinEnv) join(name string, headers map[string]string) *httptest.ResponseRecorder {
	return e.do("POST", "/api/join", map[string]string{"invitationCode": e.invitation, "name": name}, headers)
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body is not JSON: %q", w.Body.String())
	}
	return m
}

func rememberCookie(t *testing.T, w *httptest.ResponseRecorder, gameID string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "elimin8_remember_"+gameID {
			return c
		}
	}
	return nil
}

func TestJoinCreatesTheJoinedPlayerAndSetsTheSameRememberCookieAsCodeLogin(t *testing.T) {
	e := newJoinEnv(t, 0)
	w := e.join("Alex", nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	code, _ := body["recoveryCode"].(string)
	if len(code) != 8 || body["token"] == nil || body["role"] != "player" || body["game"] == nil || body["managementCode"] != nil {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	cookie := rememberCookie(t, w, e.id)
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/api/games/"+e.id+"/session" || cookie.MaxAge != 30*24*3600 || cookie.Secure || len(cookie.Value) != 48 {
		t.Fatalf("remember cookie is wrong: %+v", cookie)
	}
	if strings.Contains(w.Body.String(), cookie.Value) {
		t.Fatal("the remember secret is in the JSON body")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", w.Header().Get("Cache-Control"))
	}
	// The cookie is the one the session exchange understands: it signs the same player back in.
	ex := e.do("POST", "/api/games/"+e.id+"/session", nil, map[string]string{"Origin": "http://example.com", "Cookie": cookie.Name + "=" + cookie.Value})
	if ex.Code != http.StatusOK {
		t.Fatalf("session exchange: %d %s", ex.Code, ex.Body.String())
	}
	exchanged := decode(t, ex)
	me := func(m map[string]any) string {
		game, _ := m["game"].(map[string]any)
		meView, _ := game["me"].(map[string]any)
		id, _ := meView["id"].(string)
		return id
	}
	if me(exchanged) == "" || me(exchanged) != me(body) {
		t.Fatal("the cookie from a join did not restore the same player")
	}
	// The recovery code logs in through the existing route.
	login := e.do("POST", "/api/access", map[string]string{"invitationCode": e.invitation, "code": code, "role": "player"}, nil)
	if login.Code != http.StatusOK || me(decode(t, login)) != me(body) {
		t.Fatalf("recovery code login: %d %s", login.Code, login.Body.String())
	}
}

func TestJoinFollowsThePublishedErrorContract(t *testing.T) {
	e := newJoinEnv(t, 0)
	if w := e.join("Alex", nil); w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	cases := []struct {
		name    string
		do      func() *httptest.ResponseRecorder
		status  int
		code    string
		message string
	}{
		{"name taken, any spelling", func() *httptest.ResponseRecorder { return e.join(" ALEX ", nil) }, 409, "name_taken", "already taken"},
		{"unknown lobby", func() *httptest.ResponseRecorder {
			return e.do("POST", "/api/join", map[string]string{"invitationCode": "NOSUCHCD", "name": "Sam"}, nil)
		}, 404, "", ""},
		{"empty name", func() *httptest.ResponseRecorder { return e.join("   ", nil) }, 400, "", ""},
		{"name too long", func() *httptest.ResponseRecorder { return e.join(strings.Repeat("a", 81), nil) }, 400, "", ""},
		{"unknown field", func() *httptest.ResponseRecorder {
			return e.do("POST", "/api/join", map[string]any{"invitationCode": e.invitation, "name": "Sam", "extra": true}, nil)
		}, 400, "", ""},
		{"cross-site origin", func() *httptest.ResponseRecorder {
			return e.join("Sam", map[string]string{"Origin": "https://evil.example"})
		}, 403, "", "same origin"},
		{"malformed origin", func() *httptest.ResponseRecorder { return e.join("Sam", map[string]string{"Origin": "null"}) }, 403, "", ""},
	}
	for _, tc := range cases {
		w := tc.do()
		body := decode(t, w)
		if w.Code != tc.status || (tc.code != "" && body["code"] != tc.code) || !strings.Contains(strings.ToLower(w.Body.String()), strings.ToLower(tc.message)) {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
		if w.Code != 201 && rememberCookie(t, w, e.id) != nil {
			t.Errorf("%s: a failed join set a remember cookie", tc.name)
		}
	}
	// A matching Origin (the browser's own) and no Origin (a non-browser client) both work.
	if w := e.join("Sam", map[string]string{"Origin": "http://example.com"}); w.Code != 201 {
		t.Fatalf("matching origin: %d %s", w.Code, w.Body.String())
	}
	if w := e.join("Jo", nil); w.Code != 201 {
		t.Fatalf("no origin: %d %s", w.Code, w.Body.String())
	}
	// Once the game has started, the lobby is closed.
	g, _ := e.repo.Get(context.Background(), e.id)
	g.Status = "active"
	e.repo.Games[e.id] = g
	w := e.join("Late", nil)
	if body := decode(t, w); w.Code != 409 || body["code"] != "lobby_closed" {
		t.Fatalf("lobby_closed: %d %s", w.Code, w.Body.String())
	}
}

func TestJoinIsThrottledWithRetryAfter(t *testing.T) {
	e := newJoinEnv(t, 2)
	for i, name := range []string{"A", "B"} {
		if w := e.join(name, nil); w.Code != 201 {
			t.Fatalf("join %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	w := e.join("C", nil)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("third join: %d retry-after=%q", w.Code, w.Header().Get("Retry-After"))
	}
	g, _ := e.repo.Get(context.Background(), e.id)
	if len(g.Players) != 2 {
		t.Fatalf("a throttled join created a player: %d players", len(g.Players))
	}
}

func TestJoinNeverTreatsTheCookieAsProofAndResumesOnlyWithAPlayerBearer(t *testing.T) {
	e := newJoinEnv(t, 0)
	first := e.join("Alex", nil)
	body := decode(t, first)
	cookie := rememberCookie(t, first, e.id)
	token, _ := body["token"].(string)
	// Sending the cookie to /api/join does nothing special: the name is registered as a new player.
	other := e.join("Sam", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if other.Code != http.StatusCreated {
		t.Fatalf("cookie must not be treated as resume proof: %d %s", other.Code, other.Body.String())
	}
	if decode(t, other)["recoveryCode"] == body["recoveryCode"] {
		t.Fatal("a cookie reclaimed another player's identity")
	}
	g, _ := e.repo.Get(context.Background(), e.id)
	if len(g.Players) != 2 {
		t.Fatalf("players = %d", len(g.Players))
	}

	// A valid bearer of this game resumes: 200, the same player, no new code, no new cookie, nobody created.
	w := e.join("Whatever", map[string]string{"Authorization": "Bearer " + token})
	resumed := decode(t, w)
	if w.Code != http.StatusOK || resumed["recoveryCode"] != nil || resumed["token"] == token || rememberCookie(t, w, e.id) != nil || len(w.Result().Cookies()) != 0 {
		t.Fatalf("resume: %d %s cookies=%v", w.Code, w.Body.String(), w.Result().Cookies())
	}
	game, _ := resumed["game"].(map[string]any)
	meView, _ := game["me"].(map[string]any)
	firstGame, _ := body["game"].(map[string]any)
	firstMe, _ := firstGame["me"].(map[string]any)
	if meView["id"] != firstMe["id"] {
		t.Fatal("resume returned another player")
	}
	if g, _ := e.repo.Get(context.Background(), e.id); len(g.Players) != 2 {
		t.Fatalf("resume created a player: %d", len(g.Players))
	}

	// Other proof never claims anything.
	// A second game on the same server: its player's bearer is valid, but not for this game.
	second, err := e.svc.Create(context.Background(), "Another game", "")
	if err != nil {
		t.Fatal(err)
	}
	zed := e.do("POST", "/api/join", map[string]string{"invitationCode": second.Game.InvitationCode, "name": "Zed"}, nil)
	if zed.Code != http.StatusCreated {
		t.Fatalf("second game join: %d %s", zed.Code, zed.Body.String())
	}
	foreign := decode(t, zed)["token"].(string)
	for name, tc := range map[string]struct {
		header string
		status int
	}{
		"another game's player bearer": {"Bearer " + foreign, http.StatusForbidden},
		"the organizer bearer":         {"Bearer " + e.owner, http.StatusForbidden},
		"a garbage bearer":             {"Bearer nope", http.StatusUnauthorized},
		"a non-bearer scheme":          {"Basic abc", http.StatusUnauthorized},
	} {
		w := e.join("Alex", map[string]string{"Authorization": tc.header})
		if w.Code != tc.status || rememberCookie(t, w, e.id) != nil {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
}

func TestJoinThatCommitsButCannotIssueTheCookieReturnsTheRecoveryCode(t *testing.T) {
	e := newJoinEnv(t, 0)
	e.repo.fail = true // persisting the remember credential fails after the player is registered
	w := e.join("Alex", nil)
	body := decode(t, w)
	code, _ := body["recoveryCode"].(string)
	if w.Code != http.StatusInternalServerError || body["code"] != "registration_incomplete" || len(code) != 8 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if rememberCookie(t, w, e.id) != nil {
		t.Fatal("a cookie was claimed although issuance failed")
	}
	e.repo.fail = false
	// The name is now honestly taken, and the returned code signs the player in (and sets the cookie).
	if w := e.join("alex", nil); w.Code != http.StatusConflict {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	login := e.do("POST", "/api/access", map[string]string{"invitationCode": e.invitation, "code": code, "role": "player"}, nil)
	if login.Code != http.StatusOK || rememberCookie(t, login, e.id) == nil {
		t.Fatalf("recovery login: %d %s", login.Code, login.Body.String())
	}
	g, _ := e.repo.Get(context.Background(), e.id)
	if len(g.Players) != 1 {
		t.Fatalf("players = %d", len(g.Players))
	}
}
