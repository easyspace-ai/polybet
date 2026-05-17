package store

// SlippageBpsBuy returns the buy-side slippage in basis points: > 0 means the
// fill was worse than expected (paid more for the same outcome). When inputs
// are non-positive or NaN, returns 0.
func SlippageBpsBuy(expected, fill float64) float64 {
	if expected <= 0 || fill <= 0 {
		return 0
	}
	return (fill - expected) / expected * 10000.0
}

// SlippageBpsSell returns the sell-side slippage in basis points: > 0 means
// the fill was worse than expected (received less). When inputs are
// non-positive or NaN, returns 0.
func SlippageBpsSell(expected, fill float64) float64 {
	if expected <= 0 || fill <= 0 {
		return 0
	}
	return (expected - fill) / expected * 10000.0
}
