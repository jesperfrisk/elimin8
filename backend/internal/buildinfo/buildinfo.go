// Package buildinfo exposes the source revision embedded in the binary at
// build time. It never inspects the environment or working directory.
package buildinfo

// Unknown is reported when no revision was embedded.
const Unknown = "unknown"

// revision is set at link time:
//
//	go build -ldflags "-X github.com/jesperfrisk/elimin8/backend/internal/buildinfo.revision=<sha>"
var revision string

// Revision returns the embedded revision, or Unknown if it is missing or not
// a plain identifier (letters, digits, '.', '_', '-', at most 64 chars).
func Revision() string {
	return sanitize(revision)
}

func sanitize(v string) string {
	if v == "" || len(v) > 64 {
		return Unknown
	}
	for _, r := range v {
		ok := r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '_' || r == '-'
		if !ok {
			return Unknown
		}
	}
	return v
}
