package risksvc

import (
	"strings"
	"testing"

	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/store"
)

func TestMarshalCloseAttemptSnapshot_fok(t *testing.T) {
	pos := &store.RiskPosition{
		HighWaterCents: 88,
		StopLossPct:    20,
		SizeShares:     12.5,
	}
	rep := &polyexec.FOKSellReport{
		AtRFC3339Nano:     "2026-05-17T12:00:00Z",
		CLOBTokenID:       "abc",
		TickSize:          "0.01",
		ExtraTicks:        5,
		BestBid:           0.35,
		BestAsk:           0.36,
		LimitPrice:        0.30,
		LimitPriceDecimal: "0.3",
		SharesRequested:   12.5,
		SharesSubmitted:   12.5,
		OrderID:           "ord1",
	}
	j, err := marshalCloseAttemptSnapshot(pos, "fok_submit_ok", 34, 36, 5, rep, &closeAttemptExtras{ExecutionMode: "fok_sell"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j, `"trailCents":70.4`) {
		t.Fatalf("expected trail 70.4 in json: %s", j)
	}
	if !strings.Contains(j, `"executionMode":"fok_sell"`) {
		t.Fatalf("expected executionMode in json: %s", j)
	}
}
