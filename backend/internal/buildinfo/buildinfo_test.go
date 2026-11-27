package buildinfo

import (
	"strings"
	"testing"
)

func TestRevisionUnknownByDefault(t *testing.T) {
	if got := Revision(); got != Unknown {
		t.Fatalf("Revision() = %q, want %q", got, Unknown)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"":                      Unknown,
		"abc1234":               "abc1234",
		"1dc3ea7-dirty":         "1dc3ea7-dirty",
		"/home/user/src":        Unknown,
		"a b":                   Unknown,
		"postgres://u:p@h/db":   Unknown,
		strings.Repeat("a", 65): Unknown,
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
