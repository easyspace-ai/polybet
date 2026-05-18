package risksvc

import "github.com/easyspace-ai/polybet/internal/polyexec"

// FloorCents1 rounds a cent value down to one decimal place (0.1¢), matching UI step and display.
func FloorCents1(c float64) float64 {
	return polyexec.FloorCents1(c)
}

// CentsFromPrice01 converts a 0–1 Polymarket price to stable cents (see polyexec).
func CentsFromPrice01(price01 float64) float64 {
	return polyexec.CentsFromPrice01(price01)
}

// TrailingStopCentsFromHW computes the trailing stop trigger from a floored high-water mark
// using a percent-only model. Kept for back-compat: callers that want the
// configurable absolute cent floor should use TrailingStopCentsFromHWWithAbs.
func TrailingStopCentsFromHW(highWaterCents, stopLossPct float64) float64 {
	return FloorCents1(highWaterCents * (1 - stopLossPct/100))
}

// TrailingStopCentsFromHWWithAbs returns the trailing stop trigger using the
// trader-friendly model:
//
//	trigger = max(percent-derived trail, hw - maxAbsDropCents)
//
// In words: maxAbsDropCents is the absolute "ceiling" on how far the price
// can drop from the high-water mark before stopping out. Whichever of the
// two constraints is tighter (higher trigger) wins.
//
// Why this matters: On a 95¢ favourite, a 10% percent trail equals a 9.5¢
// drop tolerance, which is loose for a market that typically only swings
// ±2–3¢. An operator can pin a hard ceiling via maxAbsDropCents=3 and the
// trail will never sit more than 3¢ below the high-water — regardless of
// what the price-band percent table says. On low-price contracts the
// percent-derived trail is usually tighter, so the abs ceiling is harmless.
//
// maxAbsDropCents <= 0 disables the absolute ceiling and the result equals
// the legacy percent-only computation, preserving existing behaviour for
// operators that have not opted in.
func TrailingStopCentsFromHWWithAbs(highWaterCents, stopLossPct, maxAbsDropCents float64) float64 {
	pct := highWaterCents * (1 - stopLossPct/100)
	out := pct
	if maxAbsDropCents > 0 {
		absTrail := highWaterCents - maxAbsDropCents
		if absTrail > out {
			out = absTrail
		}
	}
	if out < 0 {
		out = 0
	}
	return FloorCents1(out)
}
