package polyexec

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/auth"
	"github.com/easyspace-ai/polysdk/pkg/clob"
	"github.com/easyspace-ai/polysdk/pkg/clob/clobtypes"
	"github.com/shopspring/decimal"
)

func ConditionalBalanceShares(balanceStr string) float64 {
	s := strings.TrimSpace(balanceStr)
	if s == "" || s == "0" {
		return 0
	}
	bi, err := decimal.NewFromString(s)
	if err != nil {
		f, err2 := strconv.ParseFloat(s, 64)
		if err2 != nil || !isFinite(f) {
			return 0
		}
		return f
	}
	sh := bi.Shift(-6)
	f, _ := sh.Float64()
	if !isFinite(f) || f < 0 {
		return 0
	}
	return f
}

// ExecuteFOKSell mirrors bot executePolymarketSell.
func ExecuteFOKSell(ctx context.Context, client clob.Client, signer auth.Signer, tokenID string, sizeShares float64, sellExtraTicks int) (orderID string, err error) {
	book, err := client.OrderBook(ctx, &clobtypes.BookRequest{TokenID: tokenID})
	if err != nil {
		return "", fmt.Errorf("get orderbook: %w", err)
	}
	tick := ParseTickSize(book.TickSize)
	bestBid, ok := BestBidPrice(book.Bids)
	if !ok || bestBid <= 0 {
		return "", fmt.Errorf("no_bid_liquidity token=%s", tokenID)
	}
	bal, err := client.BalanceAllowance(ctx, &clobtypes.BalanceAllowanceRequest{
		AssetType: clobtypes.AssetTypeConditional,
		TokenID:   tokenID,
	})
	if err != nil {
		return "", fmt.Errorf("balance-allowance: %w", err)
	}
	onChain := ConditionalBalanceShares(bal.Balance)
	sellAmount := sizeShares
	if isFinite(onChain) && onChain > 0 {
		sellAmount = math.Min(sizeShares, onChain)
	}
	if !isFinite(sellAmount) || sellAmount <= 0 {
		return "", fmt.Errorf("zero_conditional_balance")
	}
	floor := math.Max(tick, bestBid-float64(sellExtraTicks)*tick)
	floorDec, _ := decimal.NewFromString(fmt.Sprintf("%g", floor))
	floorDec = TruncatePriceDecimalToTick(floorDec, book.TickSize)

	signable, err := clob.NewOrderBuilder(client, signer).
		TokenID(tokenID).
		Side("SELL").
		AmountShares(sellAmount).
		PriceDec(floorDec).
		OrderType(clobtypes.OrderTypeFOK).
		BuildMarketWithContext(ctx)
	if err != nil {
		return "", fmt.Errorf("build market sell: %w", err)
	}
	resp, err := client.CreateOrderFromSignable(ctx, signable)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(resp.ID)
	if id == "" {
		id = "order_" + strconv.FormatInt(timeNowMs(), 10)
	}
	return id, nil
}

// ExecuteFOKBuy mirrors bot executePolymarketOrder (BUY).
func ExecuteFOKBuy(ctx context.Context, client clob.Client, signer auth.Signer, tokenID string, sizeUSDC, expectedOdds float64, buyExtraTicks int) (orderID string, fillOdds float64, err error) {
	book, err := client.OrderBook(ctx, &clobtypes.BookRequest{TokenID: tokenID})
	if err != nil {
		return "", 0, fmt.Errorf("get orderbook: %w", err)
	}
	tick := ParseTickSize(book.TickSize)
	bestAsk, ok := BestAskPrice(book.Asks)
	limitPrice := expectedOdds
	if ok && isFinite(tick) && tick > 0 {
		padded := bestAsk + float64(buyExtraTicks)*tick
		cap := 1 - tick
		limitPrice = math.Min(cap, math.Max(expectedOdds, padded))
	}
	limitPrice = math.Max(tick, math.Min(1-tick, limitPrice))
	limitDec, _ := decimal.NewFromString(fmt.Sprintf("%g", limitPrice))
	limitDec = TruncatePriceDecimalToTick(limitDec, book.TickSize)

	signable, err := clob.NewOrderBuilder(client, signer).
		TokenID(tokenID).
		Side("BUY").
		AmountUSDC(sizeUSDC).
		PriceDec(limitDec).
		OrderType(clobtypes.OrderTypeFOK).
		BuildMarketWithContext(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("build market buy: %w", err)
	}
	resp, err := client.CreateOrderFromSignable(ctx, signable)
	if err != nil {
		return "", 0, err
	}
	id := strings.TrimSpace(resp.ID)
	if id == "" {
		id = "order_" + strconv.FormatInt(timeNowMs(), 10)
	}
	fillOdds, _ = limitDec.Float64()
	return id, fillOdds, nil
}

func timeNowMs() int64 { return time.Now().UnixMilli() }
