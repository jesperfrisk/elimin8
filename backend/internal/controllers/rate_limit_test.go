package controllers

import (
	"net/http/httptest"
	"testing"
)

func attempt(c *GameController, remote string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/access", nil)
	r.RemoteAddr = remote
	w := httptest.NewRecorder()
	if !c.limited(w, r) {
		w.WriteHeader(200)
	}
	return w
}

func TestDefaultRateLimitRejectsWithRetryAfter(t *testing.T) {
	c := NewGameController(nil)
	for i := 0; i < 30; i++ {
		if w := attempt(c, "10.0.0.1:5000"); w.Code != 200 {
			t.Fatalf("attempt %d rejected", i+1)
		}
	}
	w := attempt(c, "10.0.0.1:5001")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("31st attempt: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	if w := attempt(c, "10.0.0.2:5000"); w.Code != 200 {
		t.Fatal("another address must have its own bucket")
	}
}

func TestConfiguredRateLimitIsBounded(t *testing.T) {
	c := NewGameController(nil)
	c.SetRateLimit(100)
	for i := 0; i < 100; i++ {
		if w := attempt(c, "10.0.0.1:5000"); w.Code != 200 {
			t.Fatalf("attempt %d rejected under a limit of 100", i+1)
		}
	}
	if w := attempt(c, "10.0.0.1:5000"); w.Code != 429 {
		t.Fatalf("101st attempt: %d, want 429", w.Code)
	}
	for _, bad := range []int{0, -5, 601, 1_000_000} {
		d := NewGameController(nil)
		d.SetRateLimit(bad)
		if d.limit != DefaultLoginRateLimit {
			t.Fatalf("SetRateLimit(%d) changed the limit to %d", bad, d.limit)
		}
	}
}
