package badgerdb

import (
	"strings"
	"time"
)

// IsKnownStartTime reports whether t is a meaningful market start time.
func IsKnownStartTime(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 2000
}

// ParseTimeFlexible parses RFC3339-ish timestamps stored in documents.
func ParseTimeFlexible(s string) time.Time {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999-07:00",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			return t
		}
	}
	return time.Time{}
}
