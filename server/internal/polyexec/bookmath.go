package polyexec

import (
	"math"
	"strconv"
	"strings"

	"github.com/easyspace-ai/polysdk/pkg/clob/clobtypes"
	"github.com/shopspring/decimal"
)

func BestBidPrice(bids []clobtypes.PriceLevel) (float64, bool) {
	if len(bids) == 0 {
		return 0, false
	}
	best := -math.MaxFloat64
	for _, b := range bids {
		p, err := strconv.ParseFloat(b.Price, 64)
		if err != nil || !isFinite(p) {
			continue
		}
		if p > best {
			best = p
		}
	}
	if best <= 0 || math.IsInf(best, 0) {
		return 0, false
	}
	return best, true
}

func BestAskPrice(asks []clobtypes.PriceLevel) (float64, bool) {
	if len(asks) == 0 {
		return 0, false
	}
	best := math.MaxFloat64
	for _, a := range asks {
		p, err := strconv.ParseFloat(a.Price, 64)
		if err != nil || !isFinite(p) || p <= 0 {
			continue
		}
		if p < best {
			best = p
		}
	}
	if best >= math.MaxFloat64 || best <= 0 {
		return 0, false
	}
	return best, true
}

func ParseTickSize(s string) float64 {
	t, _ := strconv.ParseFloat(s, 64)
	if !isFinite(t) || t <= 0 {
		return 0.01
	}
	return t
}

// TruncatePriceDecimalToTick rounds the limit price down to the book's tick precision (same scale as CLOB tick_size).
func TruncatePriceDecimalToTick(price decimal.Decimal, tickSize string) decimal.Decimal {
	tick, err := decimal.NewFromString(strings.TrimSpace(tickSize))
	if err != nil || !tick.IsPositive() {
		return price
	}
	exp := tick.Exponent()
	var places int32
	if exp < 0 {
		places = int32(-exp)
	}
	return price.Truncate(places)
}
