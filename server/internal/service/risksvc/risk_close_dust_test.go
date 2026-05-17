package risksvc

import (
	"errors"
	"fmt"
	"testing"

	"github.com/easyspace-ai/polybet/internal/polyexec"
)

func TestIsStaleCLOBBalanceSellFailure(t *testing.T) {
	t.Parallel()
	rep := &polyexec.FOKSellReport{ErrorStep: "create_order"}
	err := fmt.Errorf(`[NET-002] bad request: {"error":"not enough balance / allowance: balance: 0, order amount: 2000000"}`)
	if !isStaleCLOBBalanceSellFailure(rep, err) {
		t.Fatal("expected create_order insufficient balance")
	}
	if !isStaleCLOBBalanceSellFailure(&polyexec.FOKSellReport{ErrorStep: "zero_balance"}, polyexec.ErrZeroConditionalBalance) {
		t.Fatal("expected zero_balance")
	}
	if isStaleCLOBBalanceSellFailure(rep, errors.New("other")) {
		t.Fatal("unexpected match")
	}
}

func TestStaleCLOBBalanceReconcileAction(t *testing.T) {
	t.Parallel()
	const min = 0.1
	if staleCLOBBalanceReconcileAction(2, 0, true, min) != "complete" {
		t.Fatal("closed position should complete")
	}
	if staleCLOBBalanceReconcileAction(2, 0.05, false, min) != "close_dust" {
		t.Fatal("sub-min shares should close dust")
	}
	if staleCLOBBalanceReconcileAction(2, 1, false, min) != "retry" {
		t.Fatal("meaningful share reduction should retry")
	}
	if staleCLOBBalanceReconcileAction(2, 2, false, min) != "close_ghost" {
		t.Fatal("unchanged shares after zero CLOB should close ghost")
	}
}
