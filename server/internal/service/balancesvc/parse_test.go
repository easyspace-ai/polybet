package balancesvc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseCollateralMicro(t *testing.T) {
	v, err := ParseCollateralMicro("5705489")
	if err != nil || v < 5.705 || v > 5.706 {
		t.Fatalf("micro parse: %v %v", v, err)
	}
	v2, err := ParseCollateralMicro("12.5")
	if err != nil || v2 != 12.5 {
		t.Fatalf("float parse: %v %v", v2, err)
	}
}

func TestSummaryJSONUsesDashboardKeys(t *testing.T) {
	x := 1.5
	s := &Summary{
		Polymarket: &x,
		PolymarketAccounts: []AccountRow{
			{ID: "a1", Name: "n", IsActive: true, Polymarket: &x},
		},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(b)
	if !strings.Contains(raw, `"polymarket"`) {
		t.Fatalf("expected camelCase polymarket, got %s", raw)
	}
	if !strings.Contains(raw, `"polymarketAccounts"`) {
		t.Fatalf("expected camelCase polymarketAccounts, got %s", raw)
	}
}
