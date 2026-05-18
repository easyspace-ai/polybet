package polyexec

import "testing"

func TestCentsFromPrice01(t *testing.T) {
	t.Parallel()
	tests := []struct {
		price01 float64
		want    float64
	}{
		{0.29, 29.0},
		{0.28999999999999995, 29.0},
		{0.55, 55.0},
		{0.559, 55.9},
		{0, 0},
		{-0.1, 0},
	}
	for _, tc := range tests {
		if g := CentsFromPrice01(tc.price01); g != tc.want {
			t.Fatalf("CentsFromPrice01(%v) = %v, want %v", tc.price01, g, tc.want)
		}
	}
}

func TestFloorCents1(t *testing.T) {
	t.Parallel()
	if g := FloorCents1(55.989999999999995); g != 55.9 {
		t.Fatalf("FloorCents1 noise = %v, want 55.9", g)
	}
}

func TestCentsFromPrice01VsRawMultiply(t *testing.T) {
	t.Parallel()
	price := 0.28999999999999995
	raw := price * 100
	stable := CentsFromPrice01(price)
	if stable != 29.0 {
		t.Fatalf("CentsFromPrice01 = %v, want 29.0", stable)
	}
	if raw >= 29.0 {
		t.Fatalf("raw multiply %v should carry float noise below 29", raw)
	}
}
