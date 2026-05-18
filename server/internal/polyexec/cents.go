package polyexec

import "math"

// FloorCents1 rounds a cent value down to one decimal place (0.1¢), matching UI step and display.
func FloorCents1(c float64) float64 {
	if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
		return c
	}
	return math.Floor(c*10+1e-9) / 10
}

// CentsFromPrice01 converts a 0–1 Polymarket price to cents (×100), stable against float noise.
// Result is rounded to 0.1¢ to align with FloorCents1 / UI step (e.g. 0.29 → 29.0, not 28.999…).
func CentsFromPrice01(price01 float64) float64 {
	if price01 <= 0 || math.IsNaN(price01) || math.IsInf(price01, 0) {
		return 0
	}
	return math.Round(price01*1000) / 10
}
