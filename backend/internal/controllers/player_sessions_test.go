package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func sessionHTTPFixture(t *testing.T) (*services.GameService, *testutil.Memory, *GameController, *http.ServeMux, models.Admission, string) {
	t.Helper()
	ctx := context.Background()
	repo := testutil.NewMemory()
	s := services.NewGameService(repo)
	owner, err := s.Create(ctx, "Session exchange", "")
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := s.Authorize(ctx, owner.Token)
	if err != nil {
		t.Fatal(err)
	}
	players, err := s.AddPlayers(ctx, a, []string{"Alice"})
	if err != nil {
		t.Fatal(err)
	}
	login, err := s.Login(ctx, owner.Game.InvitationCode, players[0].Code, "player")
	if err != nil {
		t.Fatal(err)
	}
	secret, _, err := s.IssuePlayerSession(ctx, login.Token)
	if err != nil {
		t.Fatal(err)
	}
	c := NewGameController(s)
	c.SetRateLimit(600)
	trusted, _ := ParseTrustedProxies("10.0.0.1")
	c.SetTrustedProxies(trusted)
	mux := http.NewServeMux()
	c.Register(mux)
	return s, repo, c, mux, login, secret
}

func sessionRequest(mux *http.ServeMux, method, target, gameID, secret, origin, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target+"/api/games/"+gameID+"/session", strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:4000"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if secret != "" {
		r.AddCookie(&http.Cookie{Name: playerSessionCookieName(gameID), Value: secret})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestSessionHTTPOriginBodyPrivacyAndIdempotentClearing(t *testing.T) {
	s, repo, _, mux, login, secret := sessionHTTPFixture(t)
	id := login.Game.ID
	ctx := context.Background()
	original := repo.Sessions[services.HashToken(secret)]
	for _, method := range []string{"POST", "DELETE"} {
		for _, origin := range []string{"", "null", "http://evil.example", "http://localhost:13084", "https://localhost:13083", "http://localhost:13083/path"} {
			w := sessionRequest(mux, method, "http://localhost:13083", id, secret, origin, "")
			if w.Code != 403 || len(w.Result().Cookies()) != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unsafe %s origin accepted", method)
			}
		}
		w := sessionRequest(mux, method, "http://localhost:13083", id, secret, "http://localhost:13083", strings.Repeat("x", 1025))
		if w.Code != 400 {
			t.Fatal("unbounded session request body")
		}
	}
	for _, value := range []string{"", strings.Repeat("X", 48), strings.Repeat("X", 49)} {
		w := sessionRequest(mux, "POST", "http://localhost:13083", id, value, "http://localhost:13083", "")
		if w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("invalid cookie exchanged")
		}
	}
	w := sessionRequest(mux, "POST", "http://localhost:13083", "another-game", secret, "http://localhost:13083", "")
	if w.Code != 401 {
		t.Fatal("wrong-game cookie accepted")
	}
	w = sessionRequest(mux, "POST", "http://localhost:13083", id, secret, "http://localhost:13083", "{}")
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatal("resume failed or renewed remember cookie")
	}
	var admission models.Admission
	if err := json.Unmarshal(w.Body.Bytes(), &admission); err != nil {
		t.Fatal(err)
	}
	if admission.Role != "player" || admission.Game.Me.ID != login.Game.Me.ID || admission.Token == login.Token || admission.Topic != login.Topic {
		t.Fatal("unexpected admission contract")
	}
	if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), original.Hash) || strings.Contains(w.Body.String(), "sessionHash") {
		t.Fatal("session credential leaked in JSON")
	}
	if repo.Sessions[original.Hash].ExpiresAt != original.ExpiresAt {
		t.Fatal("resume extended remember lifetime")
	}
	private := httptest.NewRequest("GET", "http://localhost/api/games/"+id, nil)
	private.AddCookie(&http.Cookie{Name: playerSessionCookieName(id), Value: secret})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, private)
	if w.Code != 401 {
		t.Fatal("cookie authorized a bearer-only private route")
	}
	for _, cookie := range []string{secret, secret, "", strings.Repeat("X", 48)} {
		w := sessionRequest(mux, "DELETE", "http://localhost:13083", id, cookie, "http://localhost:13083", "")
		if w.Code != 204 || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("delete is not idempotent")
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != playerSessionCookieName(id) || cookies[0].Path != playerSessionCookiePath(id) || cookies[0].Value != "" || cookies[0].MaxAge >= 0 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Secure {
			t.Fatal("incorrect cookie clearing scope or flags")
		}
	}
	if _, _, err := s.Authorize(ctx, admission.Token); err != services.ErrUnauthorized {
		t.Fatal("revoked resumed bearer still authorized")
	}
	if repo.Sessions[original.Hash].RevokedAt == nil {
		t.Fatal("delete did not persist revocation")
	}
}

func TestSessionHTTPTransportSpoofingAndAmbiguousHeaders(t *testing.T) {
	_, _, _, mux, login, secret := sessionHTTPFixture(t)
	id := login.Game.ID
	for _, tc := range []struct {
		name, target, peer, proto string
		want                      int
	}{
		{"direct TLS", "https://localhost:13083", "192.0.2.1:4000", "", 200},
		{"trusted TLS proxy", "http://localhost:13083", "10.0.0.1:4000", "https", 200},
		{"untrusted forwarding", "http://localhost:13083", "192.0.2.1:4000", "https", 403},
		{"ambiguous forwarding", "http://localhost:13083", "10.0.0.1:4000", "https,http", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.target+"/api/games/"+id+"/session", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", "https://localhost:13083")
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			r.AddCookie(&http.Cookie{Name: playerSessionCookieName(id), Value: secret})
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
		})
	}
	for _, change := range []string{"duplicate-origin", "duplicate-cookie", "cross-site"} {
		r := httptest.NewRequest("POST", "http://localhost:13083/api/games/"+id+"/session", nil)
		r.Header.Set("Origin", "http://localhost:13083")
		r.AddCookie(&http.Cookie{Name: playerSessionCookieName(id), Value: secret})
		want := 403
		switch change {
		case "duplicate-origin":
			r.Header.Add("Origin", "http://evil.example")
		case "duplicate-cookie":
			r.AddCookie(&http.Cookie{Name: playerSessionCookieName(id), Value: secret})
			want = 401
		case "cross-site":
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal("ambiguous or cross-site request accepted")
		}
	}
}

func TestSessionHTTPRateLimitSharedAcrossGameIDs(t *testing.T) {
	_, _, c, mux, _, _ := sessionHTTPFixture(t)
	c.SetRateLimit(1)
	first := sessionRequest(mux, "POST", "http://localhost:13083", "one-game", "", "http://localhost:13083", "")
	second := sessionRequest(mux, "POST", "http://localhost:13083", "another-game", "", "http://localhost:13083", "")
	if first.Code != 401 || second.Code != 429 || second.Header().Get("Retry-After") != "60" || second.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("session limiter was bypassed by changing game IDs")
	}
}
