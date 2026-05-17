package store

import (
	"testing"
	"time"
)

func TestIsKnownStartTime(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   time.Time
		want bool
	}{
		{"zero", time.Time{}, false},
		{"year_0001", time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC), false},
		{"unix_epoch", time.Unix(0, 0).UTC(), false},
		{"year_1999", time.Date(1999, time.December, 31, 23, 59, 59, 0, time.UTC), false},
		{"year_2000_minute_0", time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC), true},
		{"future", time.Date(2030, time.June, 15, 12, 0, 0, 0, time.UTC), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsKnownStartTime(tc.in); got != tc.want {
				t.Fatalf("IsKnownStartTime(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
