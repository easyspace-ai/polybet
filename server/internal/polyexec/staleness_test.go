package polyexec

import (
	"testing"

	"github.com/easyspace-ai/polysdk/pkg/clob/clobtypes"
)

func bookFromBidAsk(bid, ask, tick string) clobtypes.OrderBookResponse {
	out := clobtypes.OrderBookResponse{TickSize: tick}
	if bid != "" {
		out.Bids = []clobtypes.PriceLevel{{Price: bid, Size: "10"}}
	}
	if ask != "" {
		out.Asks = []clobtypes.PriceLevel{{Price: ask, Size: "10"}}
	}
	return out
}

func TestBookBestBidMovedDownTicks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		old, new clobtypes.OrderBookResponse
		want     int
	}{
		{
			name: "no_move",
			old:  bookFromBidAsk("0.70", "0.72", "0.01"),
			new:  bookFromBidAsk("0.70", "0.72", "0.01"),
			want: 0,
		},
		{
			name: "down_one_tick",
			old:  bookFromBidAsk("0.70", "0.72", "0.01"),
			new:  bookFromBidAsk("0.69", "0.72", "0.01"),
			want: 1,
		},
		{
			name: "down_three_ticks",
			old:  bookFromBidAsk("0.70", "0.72", "0.01"),
			new:  bookFromBidAsk("0.67", "0.72", "0.01"),
			want: 3,
		},
		{
			name: "up_returns_zero",
			old:  bookFromBidAsk("0.70", "0.72", "0.01"),
			new:  bookFromBidAsk("0.71", "0.72", "0.01"),
			want: 0,
		},
		{
			name: "missing_bid",
			old:  bookFromBidAsk("", "0.72", "0.01"),
			new:  bookFromBidAsk("0.69", "0.72", "0.01"),
			want: 0,
		},
		{
			name: "fine_tick_size",
			old:  bookFromBidAsk("0.700", "0.720", "0.001"),
			new:  bookFromBidAsk("0.695", "0.720", "0.001"),
			want: 5,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bookBestBidMovedDownTicks(tc.old, tc.new); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestBookBestAskMovedUpTicks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		old, new clobtypes.OrderBookResponse
		want     int
	}{
		{
			name: "no_move",
			old:  bookFromBidAsk("0.70", "0.72", "0.01"),
			new:  bookFromBidAsk("0.70", "0.72", "0.01"),
			want: 0,
		},
		{
			name: "up_two_ticks",
			old:  bookFromBidAsk("0.70", "0.72", "0.01"),
			new:  bookFromBidAsk("0.70", "0.74", "0.01"),
			want: 2,
		},
		{
			name: "down_returns_zero",
			old:  bookFromBidAsk("0.70", "0.72", "0.01"),
			new:  bookFromBidAsk("0.70", "0.71", "0.01"),
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bookBestAskMovedUpTicks(tc.old, tc.new); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
