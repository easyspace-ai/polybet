package bookcache

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Level matches dashboard poly book ladder (fee-adjusted taker odds, USDC size).
type Level struct {
	Odds     float64 `json:"odds"`
	Size     float64 `json:"size"`
	Platform string  `json:"platform"`
}

type tokenBook struct {
	asks map[string]float64
	bids map[string]float64
	ts   int64
	// bestBid / bestAsk are the cached top-of-book (0–1 probability prices).
	// Tracked separately from the level maps so:
	//   - ApplyTopOfBook (which lacks size info) can update the top without
	//     polluting the maps with size=0 placeholder entries that previously
	//     leaked into the levels ladder and showed up as "$0" rows.
	//   - TopOfBook is O(1) instead of O(N) per call (called on every WS tick
	//     and every risk evaluation).
	bestBid float64
	bestAsk float64
}

// Cache mirrors bot polymarketBookCache (asks → taker buy ladder).
type Cache struct {
	mu       sync.RWMutex
	books    map[string]*tokenBook
	feeRates map[string]float64
	topN     int
}

func New(topN int) *Cache {
	if topN < 3 {
		topN = 10
	}
	if topN > 25 {
		topN = 25
	}
	return &Cache{
		books:    make(map[string]*tokenBook),
		feeRates: make(map[string]float64),
		topN:     topN,
	}
}

func (c *Cache) SetTopN(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n >= 3 && n <= 25 {
		c.topN = n
	}
}

func applyFee(p, feeRate float64) float64 {
	if feeRate == 0 {
		return p
	}
	return p + feeRate*p*(1-p)
}

func (c *Cache) SetFeeRate(tokenID string, r float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.feeRates[tokenID] = r
}

func (c *Cache) FeeRate(tokenID string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.feeRates[tokenID]
}

// ApplyTopOfBook records best bid/ask from CLOB best_bid_ask or price_change
// when full depth is not included. Updates the cached top fields directly
// rather than injecting size=0 placeholder rows into the level maps so the
// levels ladder (consumed by routersvc + dashboard UI) is not polluted with
// phantom $0-size entries.
func (c *Cache) ApplyTopOfBook(tokenID string, bestBid, bestAsk float64, ts int64) {
	if tokenID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	tb := c.books[tokenID]
	if tb == nil {
		tb = &tokenBook{asks: map[string]float64{}, bids: map[string]float64{}}
		c.books[tokenID] = tb
	}
	if ts > 0 && ts < tb.ts {
		return
	}
	if ts > 0 {
		tb.ts = ts
	} else if tb.ts == 0 {
		tb.ts = time.Now().UnixMilli()
	}
	if bestBid > 0 && isFinite(bestBid) {
		tb.bestBid = bestBid
	}
	if bestAsk > 0 && isFinite(bestAsk) {
		tb.bestAsk = bestAsk
	}
}

func priceLevelKey(p float64) string {
	return strconv.FormatFloat(p, 'f', -1, 64)
}

func (c *Cache) ReplaceBook(tokenID string, bids, asks []struct{ Price, Size string }, ts int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tb := c.books[tokenID]
	if tb == nil {
		tb = &tokenBook{asks: map[string]float64{}, bids: map[string]float64{}}
		c.books[tokenID] = tb
	}
	if ts < tb.ts {
		return
	}
	tb.asks = map[string]float64{}
	tb.bids = map[string]float64{}
	bestAsk := 0.0
	for _, a := range asks {
		sz, _ := strconv.ParseFloat(strings.TrimSpace(a.Size), 64)
		if sz > 0 {
			price := strings.TrimSpace(a.Price)
			tb.asks[price] = sz
			if p, err := strconv.ParseFloat(price, 64); err == nil && p > 0 {
				if bestAsk == 0 || p < bestAsk {
					bestAsk = p
				}
			}
		}
	}
	bestBid := 0.0
	for _, b := range bids {
		sz, _ := strconv.ParseFloat(strings.TrimSpace(b.Size), 64)
		if sz > 0 {
			price := strings.TrimSpace(b.Price)
			tb.bids[price] = sz
			if p, err := strconv.ParseFloat(price, 64); err == nil && p > 0 {
				if p > bestBid {
					bestBid = p
				}
			}
		}
	}
	tb.bestBid = bestBid
	tb.bestAsk = bestAsk
	tb.ts = ts
}

func (c *Cache) ApplyPriceChange(tokenID string, side string, price string, size string, ts int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tb := c.books[tokenID]
	if tb == nil {
		tb = &tokenBook{asks: map[string]float64{}, bids: map[string]float64{}}
		c.books[tokenID] = tb
	}
	if ts < tb.ts {
		return
	}
	isSell := strings.EqualFold(side, "SELL")
	target := tb.bids
	if isSell {
		target = tb.asks
	}
	priceTrim := strings.TrimSpace(price)
	priceFloat, _ := strconv.ParseFloat(priceTrim, 64)
	sz, _ := strconv.ParseFloat(strings.TrimSpace(size), 64)
	if size == "0" || sz <= 0 {
		delete(target, priceTrim)
		// If we removed the cached top, recompute from the remaining levels.
		// Keeping the cached top in sync after deletes is the only place a
		// scan is unavoidable; affects only the price_change pathway.
		if isSell && tb.bestAsk > 0 && priceFloat == tb.bestAsk {
			tb.bestAsk = recomputeBestAsk(tb.asks)
		} else if !isSell && tb.bestBid > 0 && priceFloat == tb.bestBid {
			tb.bestBid = recomputeBestBid(tb.bids)
		}
	} else {
		target[priceTrim] = sz
		// Incremental top update for non-deletes.
		if isSell {
			if tb.bestAsk == 0 || (priceFloat > 0 && priceFloat < tb.bestAsk) {
				tb.bestAsk = priceFloat
			}
		} else {
			if priceFloat > tb.bestBid {
				tb.bestBid = priceFloat
			}
		}
	}
	tb.ts = ts
}

func recomputeBestBid(bids map[string]float64) float64 {
	best := 0.0
	for p := range bids {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v <= 0 {
			continue
		}
		if v > best {
			best = v
		}
	}
	return best
}

func recomputeBestAsk(asks map[string]float64) float64 {
	best := 0.0
	for p := range asks {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v <= 0 {
			continue
		}
		if best == 0 || v < best {
			best = v
		}
	}
	return best
}

// BookAge returns time since last book update for tokenID.
func (c *Cache) BookAge(tokenID string) (time.Duration, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tb := c.books[tokenID]
	if tb == nil || tb.ts <= 0 {
		return 0, false
	}
	updated := time.UnixMilli(tb.ts)
	if tb.ts < 1_000_000_000_000 {
		updated = time.Unix(tb.ts, 0)
	}
	return time.Since(updated), true
}

// BookUpdatedAtMs returns the last book update unix-ms for tokenID, or 0 if unknown.
func (c *Cache) BookUpdatedAtMs(tokenID string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tb := c.books[tokenID]
	if tb == nil || tb.ts <= 0 {
		return 0
	}
	return tb.ts
}

// LastBookUpdateMs returns the latest book timestamp across all tokens (ms), or 0.
func (c *Cache) LastBookUpdateMs() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var max int64
	for _, tb := range c.books {
		if tb != nil && tb.ts > max {
			max = tb.ts
		}
	}
	return max
}

func (c *Cache) GetLevels(tokenID string) []Level {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getLevelsLocked(tokenID)
}

func (c *Cache) getLevelsLocked(tokenID string) []Level {
	tb := c.books[tokenID]
	if tb == nil {
		return nil
	}
	fee := c.feeRates[tokenID]
	type pair struct {
		odds float64
		size float64
	}
	var lv []pair
	for pStr, shares := range tb.asks {
		raw, err := strconv.ParseFloat(pStr, 64)
		if err != nil || raw <= 0 || !isFinite(raw) || !isFinite(shares) {
			continue
		}
		// Defensive: drop any zero-size level that may have slipped through
		// historical paths so the levels ladder does not surface phantom
		// $0 rows to the router or UI.
		if shares <= 0 {
			continue
		}
		lv = append(lv, pair{odds: applyFee(raw, fee), size: shares * raw})
	}
	sort.Slice(lv, func(i, j int) bool { return lv[i].odds < lv[j].odds })
	n := c.topN
	if len(lv) < n {
		n = len(lv)
	}
	out := make([]Level, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Level{Odds: lv[i].odds, Size: lv[i].size})
	}
	return out
}

func (c *Cache) TopOfBook(tokenID string) (bestBid, bestAsk float64, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tb := c.books[tokenID]
	if tb == nil {
		return 0, 0, false
	}
	// Fast path: cached top tracked by every mutation. Falls through to a
	// scan only if a legacy code path bypassed the cached fields (defensive).
	if tb.bestBid > 0 || tb.bestAsk > 0 {
		return tb.bestBid, tb.bestAsk, true
	}
	for p := range tb.asks {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v <= 0 {
			continue
		}
		if !isFinite(bestAsk) || bestAsk == 0 || v < bestAsk {
			bestAsk = v
		}
	}
	for p := range tb.bids {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v <= 0 {
			continue
		}
		if !isFinite(bestBid) || v > bestBid {
			bestBid = v
		}
	}
	return bestBid, bestAsk, isFinite(bestBid) || isFinite(bestAsk)
}

func (c *Cache) TakerOdds(tokenID string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	levels := c.getLevelsLocked(tokenID)
	if len(levels) == 0 {
		return 0, false
	}
	return levels[0].Odds, true
}

func (c *Cache) GetBidsAsks(tokenID string, n int) (bids, asks []Level) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tb := c.books[tokenID]
	if tb == nil {
		return nil, nil
	}
	fee := c.feeRates[tokenID]

	// Asks (Sellers)
	for pStr, shares := range tb.asks {
		raw, _ := strconv.ParseFloat(pStr, 64)
		if raw > 0 {
			asks = append(asks, Level{Odds: applyFee(raw, fee), Size: shares * raw, Platform: "polymarket"})
		}
	}
	sort.Slice(asks, func(i, j int) bool { return asks[i].Odds < asks[j].Odds })
	if len(asks) > n {
		asks = asks[:n]
	}

	// Bids (Buyers)
	for pStr, shares := range tb.bids {
		raw, _ := strconv.ParseFloat(pStr, 64)
		if raw > 0 {
			bids = append(bids, Level{Odds: applyFee(raw, fee), Size: shares * raw, Platform: "polymarket"})
		}
	}
	sort.Slice(bids, func(i, j int) bool { return bids[i].Odds > bids[j].Odds })
	if len(bids) > n {
		bids = bids[:n]
	}

	return bids, asks
}

// SnapshotLevels returns JSON-serializable levels for WS relay.
func (c *Cache) SnapshotLevels(tokenID string) []Level {
	return c.GetLevels(tokenID)
}

// PruneIdle evicts tokens whose last update is older than maxAge. Returns
// the number of entries removed. Intended to be called periodically (e.g.
// every few minutes) so accounts that switch tokens or stop following a
// market do not leave their books in memory forever.
//
// maxAge <= 0 disables (no-op). Tokens still actively subscribed via the
// market WS will reappear on the next book event so eviction is safe.
func (c *Cache) PruneIdle(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	cutoff := time.Now().UnixMilli() - maxAge.Milliseconds()
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for tid, tb := range c.books {
		if tb == nil {
			delete(c.books, tid)
			removed++
			continue
		}
		if tb.ts > 0 && tb.ts < cutoff {
			delete(c.books, tid)
			// Companion fee-rate entry is also dropped so feeRates does not
			// outlive the book; reseeding happens on next subscribe.
			delete(c.feeRates, tid)
			removed++
		}
	}
	return removed
}

// Size returns the number of tokens currently cached. Useful for /api/health
// and tests.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.books)
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// MarshalBook returns stable JSON for DB liquidity_levels column.
func MarshalBook(levels []Level) string {
	if len(levels) == 0 {
		return ""
	}
	b, _ := json.Marshal(levels)
	return string(b)
}
