package polyexec

import (
	"errors"
	"fmt"
	"testing"
)

func TestResolveMarketSellShares(t *testing.T) {
	t.Parallel()
	got, step, err := resolveMarketSellShares(2, 2)
	if err != nil || step != "" || got != 2 {
		t.Fatalf("full balance: got=%v step=%q err=%v", got, step, err)
	}
	_, step, err = resolveMarketSellShares(2, 0)
	if !IsZeroConditionalBalance(err) || step != "zero_balance" {
		t.Fatalf("zero on-chain: step=%q err=%v", step, err)
	}
	got, step, err = resolveMarketSellShares(2, 1.5)
	if err != nil || step != "" || got != 1.5 {
		t.Fatalf("cap to on-chain: got=%v step=%q err=%v", got, step, err)
	}
}

func TestIsCLOBInsufficientSellBalance(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf(`[NET-002] bad request: {"error":"not enough balance / allowance: the balance is not enough -> balance: 0, order amount: 2000000"}`)
	if !IsCLOBInsufficientSellBalance(err) {
		t.Fatal("expected insufficient balance detection")
	}
	if IsCLOBInsufficientSellBalance(errors.New("other")) {
		t.Fatal("unexpected match")
	}
}

func TestQuantizeMarketSellShares(t *testing.T) {
	t.Parallel()
	got, err := quantizeMarketSellShares(3.129999999999999)
	if err != nil || got != 3.12 {
		t.Fatalf("quantize 3.13-ish: got %v err %v", got, err)
	}
	_, err = quantizeMarketSellShares(0.008306)
	if !IsSellSharesBelowCLOBLot(err) {
		t.Fatalf("expected below lot, got %v", err)
	}
	got, err = quantizeMarketSellShares(0.01)
	if err != nil || got != 0.01 {
		t.Fatalf("0.01 lot: got %v err %v", got, err)
	}
	_, err = quantizeMarketSellShares(0)
	if err == nil {
		t.Fatal("expected zero balance error")
	}
}
