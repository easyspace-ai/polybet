package polyexec

import (
	"context"
	"math"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/clob"
	"github.com/easyspace-ai/polysdk/pkg/clob/clobtypes"
)

// SubmitRefreshReport summarises a pre-submit book refresh. Embedded in the
// per-order report so operators can see whether the safeguard fired.
type SubmitRefreshReport struct {
	Refreshed        bool   `json:"refreshed"`
	ElapsedMs        int64  `json:"elapsedMs,omitempty"`
	BidMoveDownTicks int    `json:"bidMoveDownTicks,omitempty"`
	AskMoveUpTicks   int    `json:"askMoveUpTicks,omitempty"`
	Err              string `json:"err,omitempty"`
}

// minRefreshIntervalMs is the lower bound on the configured freshness window.
// Below this, refetching is more harmful than helpful (extra round-trip on
// every submit). Operators can set submitMaxAgeMs to 0 to fully disable.
const minRefreshIntervalMs = 250

// maybeRefreshBookForSubmit returns the freshest book to use for signing.
//
//   - If submitMaxAgeMs <= 0 or elapsed since fetchedAt is below the
//     threshold (clamped at minRefreshIntervalMs), the original book is
//     returned untouched and didRefresh=false.
//   - Otherwise it issues a fresh /book call and returns the new payload.
//     If the refresh fails, the original book is returned with the error
//     surfaced so callers can decide whether to abort or proceed cautiously.
//
// This is the small but high-value safeguard against the build-then-submit
// stall: by the time we have a signed FOK, the book may have moved enough
// that the limit price is unreachable. The risk-side cost of one extra
// /book call (~30–80ms) is acceptable on the close path; the legacy code
// silently let race-prone fills slip through.
func maybeRefreshBookForSubmit(
	ctx context.Context,
	client clob.Client,
	tokenID string,
	original clobtypes.OrderBookResponse,
	fetchedAt time.Time,
	submitMaxAgeMs int,
) (book clobtypes.OrderBookResponse, didRefresh bool, err error) {
	if submitMaxAgeMs <= 0 {
		return original, false, nil
	}
	threshold := submitMaxAgeMs
	if threshold < minRefreshIntervalMs {
		threshold = minRefreshIntervalMs
	}
	elapsed := time.Since(fetchedAt).Milliseconds()
	if elapsed < int64(threshold) {
		return original, false, nil
	}
	fresh, ferr := client.OrderBook(ctx, &clobtypes.BookRequest{TokenID: tokenID})
	if ferr != nil {
		return original, false, ferr
	}
	return fresh, true, nil
}

// bookBestBidMovedDownTicks reports how many ticks the best bid has dropped
// from the original to the fresh book. A positive value means the SELL
// path's pre-computed limit floor would now be too HIGH (book worsened
// against us); the caller should re-derive limit price.
//
// Returns 0 when either book is missing best bid; the caller can choose to
// treat that as "no movement" and proceed.
func bookBestBidMovedDownTicks(original, fresh clobtypes.OrderBookResponse) int {
	origBid, oOk := BestBidPrice(original.Bids)
	freshBid, fOk := BestBidPrice(fresh.Bids)
	if !oOk || !fOk {
		return 0
	}
	tick := ParseTickSize(fresh.TickSize)
	if tick <= 0 {
		return 0
	}
	delta := origBid - freshBid
	if delta <= 0 {
		return 0
	}
	return int(math.Round(delta / tick))
}

// bookBestAskMovedUpTicks mirrors bookBestBidMovedDownTicks for the BUY path.
func bookBestAskMovedUpTicks(original, fresh clobtypes.OrderBookResponse) int {
	origAsk, oOk := BestAskPrice(original.Asks)
	freshAsk, fOk := BestAskPrice(fresh.Asks)
	if !oOk || !fOk {
		return 0
	}
	tick := ParseTickSize(fresh.TickSize)
	if tick <= 0 {
		return 0
	}
	delta := freshAsk - origAsk
	if delta <= 0 {
		return 0
	}
	return int(math.Round(delta / tick))
}
