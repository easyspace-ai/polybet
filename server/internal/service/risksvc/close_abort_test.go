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

func TestGammaMarketEnded(t *testing.T) {
	t.Parallel()
	if gammaMarketEnded(gammaclient.TokenMarketDisplay{Closed: true, Active: true}, true) != true {
		t.Fatal("expected closed market")
	}
	if gammaMarketEnded(gammaclient.TokenMarketDisplay{Closed: false, Active: false}, true) != true {
		t.Fatal("expected inactive market")
	}
	if gammaMarketEnded(gammaclient.TokenMarketDisplay{Closed: false, Active: true}, true) != false {
		t.Fatal("expected active open market")
	}
	if gammaMarketEnded(gammaclient.TokenMarketDisplay{}, false) != false {
		t.Fatal("missing gamma row should not imply ended")
	}
}
