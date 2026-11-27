package controllers

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrustedProxies parses a comma-separated list of IPs and CIDR prefixes
// (IPv4 or IPv6). Blank input yields no trusted proxies. Any malformed entry
// returns an error so a typo cannot silently widen or drop trust.
func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(part)
		if err != nil {
			return nil, err
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// SetTrustedProxies sets the peers whose X-Forwarded-For header is believed.
// With none configured (the default) forwarding headers are always ignored.
func (c *GameController) SetTrustedProxies(p []netip.Prefix) {
	c.mu.Lock()
	c.trusted = p
	c.mu.Unlock()
}

func (c *GameController) isTrusted(a netip.Addr) bool {
	for _, p := range c.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// clientAddress returns the address used as the rate-limit identity. It starts
// from the socket peer. Only if that peer is a trusted proxy is X-Forwarded-For
// consulted, and it is walked right to left: each trusted hop is skipped and the
// first untrusted valid address is the client. The left-hand entries, which a
// client can freely forge, are therefore never believed past an untrusted hop.
// A malformed entry stops the walk and the last valid hop is used, so garbage
// cannot choose its own bucket. Callers must not hold c.mu.
func (c *GameController) clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	cur, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	cur = cur.Unmap().WithZone("")
	c.mu.Lock()
	trusted := c.trusted
	c.mu.Unlock()
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	if !isTrusted(cur) {
		return cur.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		cur = a.Unmap().WithZone("")
		if !isTrusted(cur) {
			break
		}
	}
	return cur.String()
}
