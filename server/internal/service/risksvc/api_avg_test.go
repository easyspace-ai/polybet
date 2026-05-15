package risksvc

import (
	"testing"

	"github.com/easyspace-ai/polysdk/pkg/data"
	"github.com/shopspring/decimal"
)

func mustDec(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAvgPriceUSDFromDataPosition_prefersOfficialAvg(t *testing.T) {
	p := data.Position{
		Size:         mustDec(t, "10"),
		AvgPrice:     mustDec(t, "0.33"),
		InitialValue: mustDec(t, "5"),
	}
	if g := avgPriceUSDFromDataPosition(p); g != 0.33 {
		t.Fatalf("got %v want 0.33", g)
	}
}

func TestAvgPriceUSDFromDataPosition_infersFromInitialWhenAvgZero(t *testing.T) {
	p := data.Position{
		Size:         mustDec(t, "10"),
		AvgPrice:     mustDec(t, "0"),
		InitialValue: mustDec(t, "4.5"),
	}
	if g := avgPriceUSDFromDataPosition(p); g < 0.449 || g > 0.451 {
		t.Fatalf("got %v want ~0.45", g)
	}
}

func TestAvgPriceUSDFromDataPosition_zeroSize(t *testing.T) {
	p := data.Position{
		Size:     mustDec(t, "0"),
		AvgPrice: mustDec(t, "0.4"),
	}
	if g := avgPriceUSDFromDataPosition(p); g != 0.4 {
		t.Fatalf("got %v want 0.4", g)
	}
}
