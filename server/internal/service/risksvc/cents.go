package risksvc

import "math"

// FloorCents1 rounds a cent value down to one decimal place (0.1¢), matching UI step and display.
func FloorCents1(c float64) float64 {
	if c <= 0 || math.IsNaN(c) || math.IsInf(c, 0) {
		return c
	}
	return math.Floor(c*10+1e-9) / 10
}

// TrailingStopCentsFromHW computes the trailing stop trigger from a floored high-water mark.
func TrailingStopCentsFromHW(highWaterCents, stopLossPct float64) float64 {
	return FloorCents1(highWaterCents * (1 - stopLossPct/100))
}
