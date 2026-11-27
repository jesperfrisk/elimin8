package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

func TestPlayerLoginRememberCookieTransportOriginAndPrivacy(t *testing.T) {
	ctx := context.Background()
	repo := testutil.NewMemory()
	s := services.NewGameService(repo)
	owner, err := s.Create(ctx, "Remember cookie", "")
	if err != nil {
		t.Fatal(err)
	}
	access, _, err := s.Authorize(ctx, owner.Token)
	if err != nil {
		t.Fatal(err)
	}
	players, err := s.AddPlayers(ctx, access, []string{"Alice"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	s.Now = func() time.Time { return now }
	c := NewGameController(s)
	c.SetRateLimit(600)
	trusted, err := ParseTrustedProxies("10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	c.SetTrustedProxies(trusted)
	mux := http.NewServeMux()
	c.Register(mux)
	for _, tc := range []struct {
		name, target, peer, origin, proto, fetchSite string
		status                                       int
		secure                                       bool
	}{
		{name: "native CLI", status: 200},
		{name: "HTTP LAN with port", origin: "http://localhost:13082", status: 200},
		{name: "direct HTTPS", target: "https://localhost:13082/api/access", origin: "https://localhost:13082", status: 200, secure: true},
		{name: "trusted TLS proxy", peer: "10.0.0.1:4000", proto: "https", origin: "https://localhost:13082", status: 200, secure: true},
		{name: "untrusted TLS header ignored", proto: "https", origin: "http://localhost:13082", status: 200},
		{name: "untrusted header cannot authorize HTTPS origin", proto: "https", origin: "https://localhost:13082", status: 403},
		{name: "different port", origin: "http://localhost:13083", status: 403},
		{name: "cross origin", origin: "http://evil.example", status: 403},
		{name: "opaque origin", origin: "null", status: 403},
		{name: "origin URL with path", origin: "http://localhost:13082/path", status: 403},
		{name: "cross site metadata", fetchSite: "cross-site", status: 403},
		{name: "malformed proxy chain", peer: "10.0.0.1:4000", proto: "https,http", origin: "https://localhost:13082", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.target
			if target == "" {
				target = "http://localhost:13082/api/access"
			}
			body, _ := json.Marshal(map[string]string{"invitationCode": owner.Game.InvitationCode, "code": players[0].Code, "role": "player"})
			r := httptest.NewRequest("POST", target, strings.NewReader(string(body)))
			r.RemoteAddr = "192.0.2.5:4000"
			if tc.peer != "" {
				r.RemoteAddr = tc.peer
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			if tc.fetchSite != "" {
				r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			cookies := w.Result().Cookies()
			if tc.status != 200 {
				if len(cookies) != 0 {
					t.Fatal("failed login set cookie")
				}
				return
			}
			if len(cookies) != 1 {
				t.Fatal("expected one remembered game cookie")
			}
			cookie := cookies[0]
			if cookie.Name != playerSessionCookieName(owner.Game.ID) || cookie.Path != "/api/games/"+owner.Game.ID+"/session" || !cookie.HttpOnly || cookie.Secure != tc.secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 30*24*60*60 || !cookie.Expires.Equal(now.Add(services.RememberDuration)) || cookie.Domain != "" {
				t.Fatal("incorrect remember cookie attributes")
			}
			var admission models.Admission
			if err := json.Unmarshal(w.Body.Bytes(), &admission); err != nil {
				t.Fatal(err)
			}
			if admission.Token == "" || admission.Game.Me.ID != players[0].ID || admission.Role != "player" {
				t.Fatal("admission contract changed")
			}
			if strings.Contains(w.Body.String(), cookie.Value) || strings.Contains(w.Body.String(), services.HashToken(cookie.Value)) {
				t.Fatal("remember secret or hash leaked into JSON")
			}
			if _, err := s.ValidatePlayerSession(ctx, owner.Game.ID, cookie.Value); err != nil {
				t.Fatal("cookie does not identify authenticated player")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("login response is cacheable")
			}
		})
	}
	for _, role := range []string{"organizer", "player"} {
		code, want := "WRONG", 401
		if role == "organizer" {
			code, want = owner.ManagementCode, 200
		}
		body, _ := json.Marshal(map[string]string{"invitationCode": owner.Game.InvitationCode, "code": code, "role": role})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "http://localhost/api/access", strings.NewReader(string(body))))
		if w.Code != want || len(w.Result().Cookies()) != 0 {
			t.Fatal("organizer or rejected player login issued a cookie")
		}
	}
	for _, method := range []string{"POST", "DELETE"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, "http://localhost/api/games/"+owner.Game.ID+"/session", nil))
		if w.Code != 403 {
			t.Fatal("cookie-backed session endpoints must require Origin")
		}
	}
}

type failedRememberRepository struct{ *testutil.Memory }

func (r failedRememberRepository) PutPlayerSession(context.Context, models.PlayerSession) error {
	return errors.New("PRIVATE_DATABASE_DETAILS")
}

func TestPlayerCookieStorageFailureIsSafe(t *testing.T) {
	ctx := context.Background()
	s := services.NewGameService(failedRememberRepository{testutil.NewMemory()})
	owner, err := s.Create(ctx, "Unavailable remember store", "")
	if err != nil {
		t.Fatal(err)
	}
	access, _, err := s.Authorize(ctx, owner.Token)
	if err != nil {
		t.Fatal(err)
	}
	players, err := s.AddPlayers(ctx, access, []string{"Alice"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"invitationCode": owner.Game.InvitationCode, "code": players[0].Code, "role": "player"})
	mux := http.NewServeMux()
	NewGameController(s).Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "http://localhost/api/access", strings.NewReader(string(body))))
	if w.Code != 500 || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "token") {
		t.Fatal("storage failure issued admission or leaked details")
	}
}
