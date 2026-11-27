package controllers

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

func playerSessionCookieName(gameID string) string { return "elimin8_remember_" + gameID }
func playerSessionCookiePath(gameID string) string {
	return "/api/games/" + url.PathEscape(gameID) + "/session"
}

// Only the immediate, explicitly trusted proxy may describe TLS termination.
// The deployment proxy must overwrite this header, not pass client input on.
func (c *GameController) sessionHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	c.mu.Lock()
	trusted := c.isTrusted(peer.Unmap().WithZone(""))
	c.mu.Unlock()
	values := r.Header.Values("X-Forwarded-Proto")
	return trusted && len(values) == 1 && values[0] == "https"
}

// Code login remains usable by non-browser clients without Origin, but a
// browser must not be tricked into installing a cross-origin login cookie.
func (c *GameController) playerLoginOriginAllowed(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return true
	}
	if len(values) != 1 {
		return false
	}
	u, err := url.Parse(values[0])
	scheme := "http"
	if c.sessionHTTPS(r) {
		scheme = "https"
	}
	return err == nil && u.Scheme == scheme && strings.EqualFold(u.Host, r.Host) && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}

func (c *GameController) setPlayerSessionCookie(w http.ResponseWriter, r *http.Request, gameID, secret string, expires time.Time) error {
	cookie := &http.Cookie{Name: playerSessionCookieName(gameID), Value: secret, Path: playerSessionCookiePath(gameID), Expires: expires, MaxAge: int(services.RememberDuration / time.Second), HttpOnly: true, Secure: c.sessionHTTPS(r), SameSite: http.SameSiteLaxMode}
	if err := cookie.Valid(); err != nil {
		return err
	}
	http.SetCookie(w, cookie)
	return nil
}
