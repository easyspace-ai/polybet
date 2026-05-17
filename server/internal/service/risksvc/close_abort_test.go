package risksvc

import (
	"errors"
	"testing"

	"github.com/easyspace-ai/polybet/internal/gammaclient"
)

func TestIsNoOrderbookError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("other"), false},
		{errors.New(`get orderbook: api error: {"error":"No orderbook exists for the requested token id"}\n (status=404)`), true},
		{errors.New("get orderbook: not found (status=404)"), true},
	}
	for _, tc := range cases {
		if got := isNoOrderbookError(tc.err); got != tc.want {
			t.Fatalf("isNoOrderbookError(%q) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestGammaMarketEndedForAbort(t *testing.T) {
	t.Parallel()
	if !gammaMarketEndedForAbort(gammaclient.TokenMarketDisplay{TokenID: "t", Closed: true, Active: true}, true) {
		t.Fatal("expected closed market")
	}
	if !gammaMarketEndedForAbort(gammaclient.TokenMarketDisplay{TokenID: "t", Closed: false, Active: false}, true) {
		t.Fatal("expected inactive market")
	}
	if gammaMarketEndedForAbort(gammaclient.TokenMarketDisplay{TokenID: "t", Closed: false, Active: true}, true) {
		t.Fatal("expected active open market")
	}
	if gammaMarketEndedForAbort(gammaclient.TokenMarketDisplay{}, false) {
		t.Fatal("missing gamma row should not imply ended")
	}
	// Cache placeholder: key present but zero-value Gamma row must not read as inactive/ended.
	if gammaMarketEndedForAbort(gammaclient.TokenMarketDisplay{}, true) {
		t.Fatal("empty meta with found=true should not imply ended")
	}
}

func TestGammaMarketRowPresent(t *testing.T) {
	t.Parallel()
	if !gammaMarketRowPresent(gammaclient.TokenMarketDisplay{TokenID: "x"}) {
		t.Fatal("token id should count")
	}
	if !gammaMarketRowPresent(gammaclient.TokenMarketDisplay{Question: "q"}) {
		t.Fatal("question should count")
	}
	if !gammaMarketRowPresent(gammaclient.TokenMarketDisplay{ConditionID: "c"}) {
		t.Fatal("condition id should count")
	}
	if gammaMarketRowPresent(gammaclient.TokenMarketDisplay{}) {
		t.Fatal("zero meta should not count as present")
	}
}

// closeAbortWouldEndForGamma is the Gamma+liquidity slice of evaluateCloseTaskAbort (no store).
func closeAbortWouldEndForGamma(meta gammaclient.TokenMarketDisplay, found, hasLiquidity bool) bool {
	if !gammaMarketEndedForAbort(meta, found) {
		return false
	}
	return !hasLiquidity
}

func TestCloseAbortMarketEndedPolicy(t *testing.T) {
	t.Parallel()
	meta := gammaclient.TokenMarketDisplay{TokenID: "t", Closed: false, Active: false}
	if !closeAbortWouldEndForGamma(meta, true, false) {
		t.Fatal("inactive + no liquidity should end")
	}
	if closeAbortWouldEndForGamma(meta, true, true) {
		t.Fatal("inactive + liquidity veto should not end")
	}
	if closeAbortWouldEndForGamma(gammaclient.TokenMarketDisplay{TokenID: "t", Closed: false, Active: true}, true, false) {
		t.Fatal("active market should not end")
	}
}
