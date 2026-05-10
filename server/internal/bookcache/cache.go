package bookcache

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Level matches dashboard poly book ladder (fee-adjusted taker odds, USDC size).
type Level struct {
	Odds float64 `json:"odds"`
	Size float64 `json:"size"`
}

type tokenBook struct {
	asks map[string]float64
	bids map[string]float64
	ts   int64
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
	for _, a := range asks {
		sz, _ := strconv.ParseFloat(strings.TrimSpace(a.Size), 64)
		if sz > 0 {
			tb.asks[strings.TrimSpace(a.Price)] = sz
		}
	}
	for _, b := range bids {
		sz, _ := strconv.ParseFloat(strings.TrimSpace(b.Size), 64)
		if sz > 0 {
			tb.bids[strings.TrimSpace(b.Price)] = sz
		}
	}
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
	target := tb.bids
	if strings.EqualFold(side, "SELL") {
		target = tb.asks
	}
	sz, _ := strconv.ParseFloat(strings.TrimSpace(size), 64)
	if size == "0" || sz <= 0 {
		delete(target, strings.TrimSpace(price))
	} else {
		target[strings.TrimSpace(price)] = sz
	}
	tb.ts = ts
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

// SnapshotLevels returns JSON-serializable levels for WS relay.
func (c *Cache) SnapshotLevels(tokenID string) []Level {
	return c.GetLevels(tokenID)
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
