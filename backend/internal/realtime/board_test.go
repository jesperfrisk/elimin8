package realtime

import "testing"

func TestBoardTopicAllowedOnlyAcceptsOneConcreteBoardTopic(t *testing.T) {
	for topic, want := range map[string]bool{
		"boards/abc123/updates":       true,
		"boards/+/updates":            false,
		"boards/#":                    false,
		"#":                           false,
		"+/+/+":                       false,
		"boards//updates":             false,
		"boards/abc/updates/extra":    false,
		"games/abc/player/updates":    false,
		"games/abc/organizer/updates": false,
		"$SYS/#":                      false,
		"boards/abc/other":            false,
		"boards/ab+c/updates":         false,
	} {
		if got := boardTopicAllowed(topic); got != want {
			t.Errorf("boardTopicAllowed(%q) = %v, want %v", topic, got, want)
		}
	}
}
