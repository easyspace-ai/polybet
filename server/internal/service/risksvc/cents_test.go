package risksvc

import "testing"

func TestFloorCents1(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{55.989999999999995, 55.9},
		{55.98, 55.9},
		{55.91, 55.9},
		{55.9, 55.9},
		{0, 0},
		{-1, -1},
	}
	for _, tc := range tests {
		if g := FloorCents1(tc.in); g != tc.want {
			t.Fatalf("FloorCents1(%v) = %v, want %v", tc.in, g, tc.want)
		}
	}
}

func TestTrailingStopCentsFromHW(t *testing.T) {
	// 20% stop from floored HW 55.9 → 44.72 → floor 44.7
	if g := TrailingStopCentsFromHW(55.989999999999995, 20); g != 44.7 {
		t.Fatalf("got %v want 44.7", g)
	}
	if g := TrailingStopCentsFromHW(55.9, 20); g != 44.7 {
		t.Fatalf("got %v want 44.7", g)
	}
}
