package controllers

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func proxied(t *testing.T, trusted string) *GameController {
	t.Helper()
	c := NewGameController(nil)
	p, err := ParseTrustedProxies(trusted)
	if err != nil {
		t.Fatal(err)
	}
	c.SetTrustedProxies(p)
	return c
}

func viaProxy(c *GameController, peer string, xff ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/access", nil)
	r.RemoteAddr = peer
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	w := httptest.NewRecorder()
	if !c.limited(w, r) {
		w.WriteHeader(200)
	}
	return w
}

func TestClientAddressResolution(t *testing.T) {
	c := proxied(t, "10.0.0.0/8, 2001:db8:1::/48, 192.0.2.1")
	cases := []struct {
		name, peer string
		xff        []string
		want       string
	}{
		{"direct", "198.51.100.7:1", nil, "198.51.100.7"},
		{"untrusted peer spoofing", "198.51.100.7:1", []string{"203.0.113.5"}, "198.51.100.7"},
		{"trusted proxy", "10.1.2.3:1", []string{"203.0.113.5"}, "203.0.113.5"},
		{"trusted proxy no header", "10.1.2.3:1", nil, "10.1.2.3"},
		{"client-forged left entry ignored", "10.1.2.3:1", []string{"1.1.1.1, 203.0.113.5"}, "203.0.113.5"},
		{"chain of trusted hops", "10.1.2.3:1", []string{"203.0.113.5, 10.9.9.9, 192.0.2.1"}, "203.0.113.5"},
		{"split header lines", "10.1.2.3:1", []string{"1.1.1.1", "203.0.113.5, 10.4.4.4"}, "203.0.113.5"},
		{"all hops trusted", "10.1.2.3:1", []string{"10.5.5.5"}, "10.5.5.5"},
		{"malformed entry", "10.1.2.3:1", []string{"203.0.113.5, bogus"}, "10.1.2.3"},
		{"ipv6 client", "10.1.2.3:1", []string{"2001:db8:ffff::9"}, "2001:db8:ffff::9"},
		{"ipv6 trusted proxy", "[2001:db8:1::5]:1", []string{"198.51.100.9"}, "198.51.100.9"},
		{"ipv6 untrusted peer spoofing", "[2001:db8:2::5]:1", []string{"198.51.100.9"}, "2001:db8:2::5"},
		{"ipv4-mapped peer", "[::ffff:10.1.2.3]:1", []string{"203.0.113.5"}, "203.0.113.5"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("POST", "/api/access", nil)
		r.RemoteAddr = tc.peer
		for _, v := range tc.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := c.clientAddress(r); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNoTrustedProxiesIgnoresForwardingHeaders(t *testing.T) {
	c := NewGameController(nil)
	for i := 0; i < 30; i++ {
		if w := viaProxy(c, "10.0.0.1:1", fmt.Sprintf("203.0.113.%d", i)); w.Code != 200 {
			t.Fatalf("attempt %d rejected", i+1)
		}
	}
	if w := viaProxy(c, "10.0.0.1:2", "203.0.113.200"); w.Code != 429 {
		t.Fatalf("rotating X-Forwarded-For bypassed the limit: %d", w.Code)
	}
}

func TestUntrustedPeerCannotRotateForwardedAddress(t *testing.T) {
	c := proxied(t, "10.0.0.0/8")
	for i := 0; i < 30; i++ {
		viaProxy(c, "198.51.100.7:1", fmt.Sprintf("203.0.113.%d", i))
	}
	if w := viaProxy(c, "198.51.100.7:2", "203.0.113.250"); w.Code != 429 {
		t.Fatalf("spoofing bypassed the limit: %d", w.Code)
	}
}

func TestManyClientsBehindOneTrustedProxyHaveSeparateBuckets(t *testing.T) {
	c := proxied(t, "10.0.0.0/8")
	for i := 0; i < 100; i++ {
		client := fmt.Sprintf("203.0.113.%d", i)
		if i%2 == 1 {
			client = fmt.Sprintf("2001:db8:ffff::%x", i)
		}
		if w := viaProxy(c, "10.0.0.1:1", client); w.Code != 200 {
			t.Fatalf("client %d rejected", i)
		}
	}
}

func TestAbusiveClientBehindProxyIsLimitedWithoutBlockingOthers(t *testing.T) {
	c := proxied(t, "10.0.0.0/8")
	for i := 0; i < 30; i++ {
		viaProxy(c, "10.0.0.1:1", "203.0.113.1")
	}
	w := viaProxy(c, "10.0.0.1:1", "203.0.113.1")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("abuser: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	if w := viaProxy(c, "10.0.0.1:1", "203.0.113.2"); w.Code != 200 {
		t.Fatalf("innocent client blocked: %d", w.Code)
	}
}

func TestAggregateSafeguardBoundsSpreadOutClients(t *testing.T) {
	c := proxied(t, "10.0.0.0/8")
	for i := 0; i < AggregateRateLimit; i++ {
		client := fmt.Sprintf("2001:db8:ffff::%x", i+1)
		if w := viaProxy(c, "10.0.0.1:1", client); w.Code != 200 {
			t.Fatalf("attempt %d rejected below the aggregate cap", i+1)
		}
	}
	w := viaProxy(c, "10.0.0.1:1", "203.0.113.77")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("aggregate cap: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
}

func TestParseTrustedProxiesRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"nope", "10.0.0.0/33", "10.0.0.1,x"} {
		if _, err := ParseTrustedProxies(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if p, err := ParseTrustedProxies(" "); err != nil || len(p) != 0 {
		t.Errorf("blank: %v %v", p, err)
	}
}
