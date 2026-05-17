package marketstream

import "testing"

func TestParseFeeStringWithBpsHeuristic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want float64
		ok   bool
	}{
		{"empty", "", 0, false},
		{"whitespace", "   ", 0, false},
		{"negative", "-0.01", 0, false},
		{"garbage", "not-a-number", 0, false},
		{"fraction_zero", "0", 0, true},
		{"fraction_low", "0.015", 0.015, true},
		// "1" or "1.0" is interpreted as 1 bps (= 0.0001) by the heuristic.
		// This is rare in practice but harmless: the result is a finite,
		// plausible fee. Documenting via test rather than rejecting outright
		// keeps the parser's rule set tight (just one branch).
		{"one_treated_as_one_bps", "1.0", 0.0001, true},
		{"bps_200", "200", 0.02, true},
		{"bps_50", "50", 0.005, true},
		{"bps_too_large_rejected", "12000", 0, false}, // 12000 / 10000 = 1.2 ≥ 1
		{"bps_string_with_spaces", "  150  ", 0.015, true},
		{"NaN_rejected", "NaN", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFeeStringWithBpsHeuristic(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got %v ok=%v want %v ok=%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestNewMarketEventFeeRate(t *testing.T) {
	t.Parallel()
	t.Run("nil_event", func(t *testing.T) {
		if _, ok := NewMarketEventFeeRate(nil); ok {
			t.Fatal("nil event must return ok=false")
		}
	})
	t.Run("fees_disabled_emits_zero", func(t *testing.T) {
		ev := &NewMarketEvent{FeesEnabled: false, TakerBaseFee: "200"}
		got, ok := NewMarketEventFeeRate(ev)
		if !ok || got != 0 {
			t.Fatalf("fees disabled: got=%v ok=%v want 0/true", got, ok)
		}
	})
	t.Run("fee_schedule_rate_wins", func(t *testing.T) {
		ev := &NewMarketEvent{
			FeesEnabled:  true,
			TakerBaseFee: "0.99",
			FeeSchedule:  FeeSchedule{Rate: "150"}, // 150 bps = 0.015
		}
		got, ok := NewMarketEventFeeRate(ev)
		if !ok || got != 0.015 {
			t.Fatalf("fee_schedule precedence: got=%v ok=%v want 0.015/true", got, ok)
		}
	})
	t.Run("falls_back_to_taker_base_fee", func(t *testing.T) {
		ev := &NewMarketEvent{
			FeesEnabled:  true,
			TakerBaseFee: "0.025",
		}
		got, ok := NewMarketEventFeeRate(ev)
		if !ok || got != 0.025 {
			t.Fatalf("taker_base_fee fallback: got=%v ok=%v want 0.025/true", got, ok)
		}
	})
	t.Run("all_missing_returns_false", func(t *testing.T) {
		ev := &NewMarketEvent{FeesEnabled: true}
		if _, ok := NewMarketEventFeeRate(ev); ok {
			t.Fatal("all-missing should return ok=false (caller falls back to default)")
		}
	})
}
