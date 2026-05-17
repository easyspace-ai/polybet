package polyexec

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/shopspring/decimal"
)

// CLOB market sells truncate share amounts to 2 decimal places (polysdk lotSizeScale).
const clobMarketShareLotScale = 2

// ErrSellSharesBelowCLOBLot means the raw balance is positive but truncates to zero at CLOB lot precision.
var ErrSellSharesBelowCLOBLot = errors.New("sell_shares_below_clob_lot")

// ErrZeroConditionalBalance means the CLOB conditional token balance is zero or unusable for a sell.
var ErrZeroConditionalBalance = errors.New("zero_conditional_balance")

// quantizeMarketSellShares rounds share size down to the CLOB market-order lot (2 dp).
func quantizeMarketSellShares(shares float64) (float64, error) {
	if !isFinite(shares) || shares <= 0 {
		return 0, fmt.Errorf("%w", ErrZeroConditionalBalance)
	}
	q := decimal.NewFromFloat(shares).Truncate(clobMarketShareLotScale)
	if q.Sign() <= 0 {
		return 0, fmt.Errorf("%w: raw=%g", ErrSellSharesBelowCLOBLot, shares)
	}
	out, _ := q.Float64()
	if !isFinite(out) || out <= 0 {
		return 0, fmt.Errorf("%w: raw=%g", ErrSellSharesBelowCLOBLot, shares)
	}
	return out, nil
}

// IsSellSharesBelowCLOBLot reports whether err is a dust-below-lot sell sizing failure.
func IsSellSharesBelowCLOBLot(err error) bool {
	return errors.Is(err, ErrSellSharesBelowCLOBLot)
}

// IsZeroConditionalBalance reports a zero or missing CLOB conditional balance for selling.
func IsZeroConditionalBalance(err error) bool {
	return errors.Is(err, ErrZeroConditionalBalance)
}

// IsCLOBInsufficientSellBalance reports create_order rejections when balance/allowance is too low.
func IsCLOBInsufficientSellBalance(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not enough balance") ||
		strings.Contains(msg, "balance is not enough") ||
		(strings.Contains(msg, "balance: 0") && strings.Contains(msg, "order amount"))
}

// resolveMarketSellShares caps requested size to on-chain balance and quantizes to the CLOB lot.
// onChain is the parsed conditional balance (0 when empty). errorStep is set on failure.
func resolveMarketSellShares(requested, onChain float64) (submitted float64, errorStep string, err error) {
	if !isFinite(onChain) || onChain <= 0 {
		return 0, "zero_balance", fmt.Errorf("%w", ErrZeroConditionalBalance)
	}
	sellAmount := math.Min(requested, onChain)
	if !isFinite(sellAmount) || sellAmount <= 0 {
		return 0, "zero_balance", fmt.Errorf("%w", ErrZeroConditionalBalance)
	}
	sellAmount, err = quantizeMarketSellShares(sellAmount)
	if err != nil {
		if errors.Is(err, ErrSellSharesBelowCLOBLot) {
			return 0, "below_min_lot", err
		}
		return 0, "zero_balance", err
	}
	return sellAmount, "", nil
}
