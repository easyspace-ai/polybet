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

// FOKSellReport captures inputs and the limit price actually submitted to the CLOB (or the last computed step on error).
type FOKSellReport struct {
	AtRFC3339Nano        string               `json:"at"`
	CLOBTokenID          string               `json:"clobTokenId,omitempty"`
	TickSize             string               `json:"tickSize,omitempty"`
	ExtraTicks           int                  `json:"extraTicks"`
	OrderType            string               `json:"orderType,omitempty"` // FOK | FAK
	WorstPriceConfigured float64              `json:"worstPriceConfigured,omitempty"`
	BestBid              float64              `json:"bestBid,omitempty"` // 0–1
	BestAsk              float64              `json:"bestAsk,omitempty"` // 0–1
	LimitPrice           float64              `json:"limitPrice,omitempty"`
	LimitPriceDecimal    string               `json:"limitPriceDecimal,omitempty"`
	SharesRequested      float64              `json:"positionSharesRequested"`
	SharesSubmitted      float64              `json:"sharesSubmitted,omitempty"`
	OnChainBalanceShares float64              `json:"onChainBalanceShares,omitempty"`
	OrderID              string               `json:"orderId,omitempty"`
	ErrorStep            string               `json:"errorStep,omitempty"` // token_id | orderbook | no_bid | balance | zero_balance | below_min_lot | build | create_order
	SubmitRefresh        *SubmitRefreshReport `json:"submitRefresh,omitempty"`
}

// FOKBuyReport captures FOK buy telemetry (hedge path and trades).
type FOKBuyReport struct {
	AtRFC3339Nano        string               `json:"at"`
	CLOBTokenID          string               `json:"clobTokenId,omitempty"`
	TickSize             string               `json:"tickSize,omitempty"`
	ExtraTicks           int                  `json:"extraTicks"`
	BestBid              float64              `json:"bestBid,omitempty"`
	BestAsk              float64              `json:"bestAsk,omitempty"`
	LimitPrice           float64              `json:"limitPrice,omitempty"`
	LimitPriceDecimal    string               `json:"limitPriceDecimal,omitempty"`
	SizeUSDC             float64              `json:"sizeUSDC,omitempty"`
	ExpectedOdds         float64              `json:"expectedOdds,omitempty"`
	OrderID              string               `json:"orderId,omitempty"`
	ErrorStep            string               `json:"errorStep,omitempty"` // token_id | orderbook | build | create_order
	SubmitRefresh        *SubmitRefreshReport `json:"submitRefresh,omitempty"`
}

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

// ExecuteFOKSell mirrors bot executePolymarketSell with submit-time staleness disabled.
// Use ExecuteFOKSellWithOpts when staleness protection is desired.
func ExecuteFOKSell(ctx context.Context, client clob.Client, signer auth.Signer, tokenID string, sizeShares float64, sellExtraTicks int) (orderID string, rep *FOKSellReport, err error) {
	return ExecuteFOKSellWithOpts(ctx, client, signer, tokenID, sizeShares, sellExtraTicks, 0)
}

// ExecuteFOKSellWithOpts mirrors bot executePolymarketSell. When submitMaxAgeMs > 0
// and the elapsed time between the initial /book fetch and the build step
// exceeds the threshold, the book is refreshed and the limit floor is
// recomputed before signing. rep is always non-nil and populated through the
// last successful step (even when err != nil).
func ExecuteFOKSellWithOpts(ctx context.Context, client clob.Client, signer auth.Signer, tokenID string, sizeShares float64, sellExtraTicks, submitMaxAgeMs int) (orderID string, rep *FOKSellReport, err error) {
	now := time.Now().UTC()
	rep = &FOKSellReport{
		AtRFC3339Nano:   now.Format(time.RFC3339Nano),
		ExtraTicks:      sellExtraTicks,
		SharesRequested: sizeShares,
	}
	tokenID, err = MustCLOBAssetIDForAPI(tokenID)
	if err != nil {
		rep.ErrorStep = "token_id"
		return "", rep, err
	}
	rep.CLOBTokenID = tokenID
	bookFetchedAt := time.Now()
	book, err := client.OrderBook(ctx, &clobtypes.BookRequest{TokenID: tokenID})
	if err != nil {
		rep.ErrorStep = "orderbook"
		return "", rep, fmt.Errorf("get orderbook: %w", err)
	}
	rep.TickSize = strings.TrimSpace(book.TickSize)
	tick := ParseTickSize(book.TickSize)
	bestBid, bidOK := BestBidPrice(book.Bids)
	if bidOK {
		rep.BestBid = bestBid
	}
	if bestAsk, askOK := BestAskPrice(book.Asks); askOK {
		rep.BestAsk = bestAsk
	}
	if !bidOK || bestBid <= 0 {
		rep.ErrorStep = "no_bid"
		return "", rep, fmt.Errorf("no_bid_liquidity token=%s", tokenID)
	}
	bal, err := client.BalanceAllowance(ctx, &clobtypes.BalanceAllowanceRequest{
		AssetType: clobtypes.AssetTypeConditional,
		TokenID:   tokenID,
	})
	if err != nil {
		rep.ErrorStep = "balance"
		return "", rep, fmt.Errorf("balance-allowance: %w", err)
	}
	onChain := ConditionalBalanceShares(bal.Balance)
	if isFinite(onChain) {
		rep.OnChainBalanceShares = onChain
	}
	sellAmount, step, err := resolveMarketSellShares(sizeShares, onChain)
	if err != nil {
		rep.ErrorStep = step
		return "", rep, err
	}
	rep.SharesSubmitted = sellAmount

	// Pre-submit freshness re-check. This is the small but high-value safeguard
	// against build-then-submit races where bestBid moves down between fetch
	// and signing, leaving us with a limit floor that won't fill.
	if submitMaxAgeMs > 0 {
		fresh, refreshed, refErr := maybeRefreshBookForSubmit(ctx, client, tokenID, book, bookFetchedAt, submitMaxAgeMs)
		refReport := &SubmitRefreshReport{
			Refreshed: refreshed,
			ElapsedMs: time.Since(bookFetchedAt).Milliseconds(),
		}
		if refErr != nil {
			refReport.Err = refErr.Error()
		}
		if refreshed {
			refReport.BidMoveDownTicks = bookBestBidMovedDownTicks(book, fresh)
			book = fresh
			rep.TickSize = strings.TrimSpace(book.TickSize)
			tick = ParseTickSize(book.TickSize)
			if nb, ok := BestBidPrice(book.Bids); ok {
				bestBid = nb
				rep.BestBid = nb
			}
			if na, ok := BestAskPrice(book.Asks); ok {
				rep.BestAsk = na
			}
			if !bidOK || bestBid <= 0 {
				rep.ErrorStep = "no_bid"
				rep.SubmitRefresh = refReport
				return "", rep, fmt.Errorf("no_bid_liquidity_after_refresh token=%s", tokenID)
			}
		}
		rep.SubmitRefresh = refReport
	}

	floor := math.Max(tick, bestBid-float64(sellExtraTicks)*tick)
	floorDec, _ := decimal.NewFromString(fmt.Sprintf("%g", floor))
	floorDec = TruncatePriceDecimalToTick(floorDec, book.TickSize)
	rep.LimitPriceDecimal = floorDec.String()
	rep.LimitPrice, _ = floorDec.Float64()

	signable, err := clob.NewOrderBuilder(client, signer).
		TokenID(tokenID).
		Side("SELL").
		AmountShares(sellAmount).
		PriceDec(floorDec).
		OrderType(clobtypes.OrderTypeFOK).
		BuildMarketWithContext(ctx)
	if err != nil {
		rep.ErrorStep = "build"
		return "", rep, fmt.Errorf("build market sell: %w", err)
	}
	resp, err := client.CreateOrderFromSignable(ctx, signable)
	if err != nil {
		rep.ErrorStep = "create_order"
		return "", rep, err
	}
	id := strings.TrimSpace(resp.ID)
	if id == "" {
		id = "order_" + strconv.FormatInt(timeNowMs(), 10)
	}
	rep.OrderType = "FOK"
	rep.OrderID = id
	return id, rep, nil
}

// ExecuteFAKSell submits a FAK market-style sell with worst-price limit
// (minimum acceptable price, 0–1) and submit-time staleness disabled. Use
// ExecuteFAKSellWithOpts to enable the pre-submit refresh safeguard.
func ExecuteFAKSell(ctx context.Context, client clob.Client, signer auth.Signer, tokenID string, sizeShares float64, worstPrice float64) (orderID string, rep *FOKSellReport, err error) {
	return ExecuteFAKSellWithOpts(ctx, client, signer, tokenID, sizeShares, worstPrice, 0)
}

// ExecuteFAKSellWithOpts is the staleness-aware FAK sell. Unlike FOK, the
// worst-price limit is NOT recomputed on refresh — the operator-configured
// floor is the contract; the refresh only updates the tick truncation and
// telemetry. Remaining size may stay on-chain; callers should Sync and
// re-check balances.
func ExecuteFAKSellWithOpts(ctx context.Context, client clob.Client, signer auth.Signer, tokenID string, sizeShares float64, worstPrice float64, submitMaxAgeMs int) (orderID string, rep *FOKSellReport, err error) {
	now := time.Now().UTC()
	rep = &FOKSellReport{
		AtRFC3339Nano:        now.Format(time.RFC3339Nano),
		SharesRequested:      sizeShares,
		OrderType:            "FAK",
		WorstPriceConfigured: worstPrice,
	}
	tokenID, err = MustCLOBAssetIDForAPI(tokenID)
	if err != nil {
		rep.ErrorStep = "token_id"
		return "", rep, err
	}
	rep.CLOBTokenID = tokenID
	bookFetchedAt := time.Now()
	book, err := client.OrderBook(ctx, &clobtypes.BookRequest{TokenID: tokenID})
	if err != nil {
		rep.ErrorStep = "orderbook"
		return "", rep, fmt.Errorf("get orderbook: %w", err)
	}
	rep.TickSize = strings.TrimSpace(book.TickSize)
	tick := ParseTickSize(book.TickSize)
	if bestBid, bidOK := BestBidPrice(book.Bids); bidOK {
		rep.BestBid = bestBid
	}
	if bestAsk, askOK := BestAskPrice(book.Asks); askOK {
		rep.BestAsk = bestAsk
	}
	bal, err := client.BalanceAllowance(ctx, &clobtypes.BalanceAllowanceRequest{
		AssetType: clobtypes.AssetTypeConditional,
		TokenID:   tokenID,
	})
	if err != nil {
		rep.ErrorStep = "balance"
		return "", rep, fmt.Errorf("balance-allowance: %w", err)
	}
	onChain := ConditionalBalanceShares(bal.Balance)
	if isFinite(onChain) {
		rep.OnChainBalanceShares = onChain
	}
	sellAmount, step, err := resolveMarketSellShares(sizeShares, onChain)
	if err != nil {
		rep.ErrorStep = step
		return "", rep, err
	}
	rep.SharesSubmitted = sellAmount

	if submitMaxAgeMs > 0 {
		fresh, refreshed, refErr := maybeRefreshBookForSubmit(ctx, client, tokenID, book, bookFetchedAt, submitMaxAgeMs)
		refReport := &SubmitRefreshReport{
			Refreshed: refreshed,
			ElapsedMs: time.Since(bookFetchedAt).Milliseconds(),
		}
		if refErr != nil {
			refReport.Err = refErr.Error()
		}
		if refreshed {
			refReport.BidMoveDownTicks = bookBestBidMovedDownTicks(book, fresh)
			book = fresh
			rep.TickSize = strings.TrimSpace(book.TickSize)
			tick = ParseTickSize(book.TickSize)
			if nb, ok := BestBidPrice(book.Bids); ok {
				rep.BestBid = nb
			}
			if na, ok := BestAskPrice(book.Asks); ok {
				rep.BestAsk = na
			}
		}
		rep.SubmitRefresh = refReport
	}

	limit := worstPrice
	if isFinite(tick) && tick > 0 {
		limit = math.Max(tick, math.Min(1-tick, limit))
	} else {
		limit = math.Max(0.0001, math.Min(0.9999, limit))
	}
	limitDec, _ := decimal.NewFromString(fmt.Sprintf("%g", limit))
	limitDec = TruncatePriceDecimalToTick(limitDec, book.TickSize)
	rep.LimitPriceDecimal = limitDec.String()
	rep.LimitPrice, _ = limitDec.Float64()

	signable, err := clob.NewOrderBuilder(client, signer).
		TokenID(tokenID).
		Side("SELL").
		AmountShares(sellAmount).
		PriceDec(limitDec).
		OrderType(clobtypes.OrderTypeFAK).
		BuildMarketWithContext(ctx)
	if err != nil {
		rep.ErrorStep = "build"
		return "", rep, fmt.Errorf("build market sell fak: %w", err)
	}
	resp, err := client.CreateOrderFromSignable(ctx, signable)
	if err != nil {
		rep.ErrorStep = "create_order"
		return "", rep, err
	}
	id := strings.TrimSpace(resp.ID)
	if id == "" {
		id = "order_" + strconv.FormatInt(timeNowMs(), 10)
	}
	rep.OrderID = id
	return id, rep, nil
}

// ExecuteFOKBuy mirrors bot executePolymarketOrder (BUY) without submit-time
// staleness protection. Use ExecuteFOKBuyWithOpts to enable refresh.
func ExecuteFOKBuy(ctx context.Context, client clob.Client, signer auth.Signer, tokenID string, sizeUSDC, expectedOdds float64, buyExtraTicks int) (orderID string, fillOdds float64, rep *FOKBuyReport, err error) {
	return ExecuteFOKBuyWithOpts(ctx, client, signer, tokenID, sizeUSDC, expectedOdds, buyExtraTicks, 0)
}

// ExecuteFOKBuyWithOpts is the staleness-aware FOK buy.
func ExecuteFOKBuyWithOpts(ctx context.Context, client clob.Client, signer auth.Signer, tokenID string, sizeUSDC, expectedOdds float64, buyExtraTicks, submitMaxAgeMs int) (orderID string, fillOdds float64, rep *FOKBuyReport, err error) {
	now := time.Now().UTC()
	rep = &FOKBuyReport{
		AtRFC3339Nano: now.Format(time.RFC3339Nano),
		ExtraTicks:    buyExtraTicks,
		SizeUSDC:      sizeUSDC,
		ExpectedOdds:  expectedOdds,
	}
	tokenID, err = MustCLOBAssetIDForAPI(tokenID)
	if err != nil {
		rep.ErrorStep = "token_id"
		return "", 0, rep, err
	}
	rep.CLOBTokenID = tokenID
	bookFetchedAt := time.Now()
	book, err := client.OrderBook(ctx, &clobtypes.BookRequest{TokenID: tokenID})
	if err != nil {
		rep.ErrorStep = "orderbook"
		return "", 0, rep, fmt.Errorf("get orderbook: %w", err)
	}
	rep.TickSize = strings.TrimSpace(book.TickSize)
	tick := ParseTickSize(book.TickSize)
	if bb, ok := BestBidPrice(book.Bids); ok {
		rep.BestBid = bb
	}
	bestAsk, ok := BestAskPrice(book.Asks)
	if ok {
		rep.BestAsk = bestAsk
	}

	if submitMaxAgeMs > 0 {
		fresh, refreshed, refErr := maybeRefreshBookForSubmit(ctx, client, tokenID, book, bookFetchedAt, submitMaxAgeMs)
		refReport := &SubmitRefreshReport{
			Refreshed: refreshed,
			ElapsedMs: time.Since(bookFetchedAt).Milliseconds(),
		}
		if refErr != nil {
			refReport.Err = refErr.Error()
		}
		if refreshed {
			refReport.AskMoveUpTicks = bookBestAskMovedUpTicks(book, fresh)
			book = fresh
			rep.TickSize = strings.TrimSpace(book.TickSize)
			tick = ParseTickSize(book.TickSize)
			if bb, ok := BestBidPrice(book.Bids); ok {
				rep.BestBid = bb
			}
			if na, askOK := BestAskPrice(book.Asks); askOK {
				bestAsk = na
				ok = true
				rep.BestAsk = na
			}
		}
		rep.SubmitRefresh = refReport
	}

	limitPrice := expectedOdds
	if ok && isFinite(tick) && tick > 0 {
		padded := bestAsk + float64(buyExtraTicks)*tick
		cap := 1 - tick
		limitPrice = math.Min(cap, math.Max(expectedOdds, padded))
	}
	limitPrice = math.Max(tick, math.Min(1-tick, limitPrice))
	limitDec, _ := decimal.NewFromString(fmt.Sprintf("%g", limitPrice))
	limitDec = TruncatePriceDecimalToTick(limitDec, book.TickSize)
	rep.LimitPriceDecimal = limitDec.String()
	rep.LimitPrice, _ = limitDec.Float64()

	signable, err := clob.NewOrderBuilder(client, signer).
		TokenID(tokenID).
		Side("BUY").
		AmountUSDC(sizeUSDC).
		PriceDec(limitDec).
		OrderType(clobtypes.OrderTypeFOK).
		BuildMarketWithContext(ctx)
	if err != nil {
		rep.ErrorStep = "build"
		return "", 0, rep, fmt.Errorf("build market buy: %w", err)
	}
	resp, err := client.CreateOrderFromSignable(ctx, signable)
	if err != nil {
		rep.ErrorStep = "create_order"
		return "", 0, rep, err
	}
	id := strings.TrimSpace(resp.ID)
	if id == "" {
		id = "order_" + strconv.FormatInt(timeNowMs(), 10)
	}
	rep.OrderID = id
	fillOdds, _ = limitDec.Float64()
	return id, fillOdds, rep, nil
}

// HedgeSizingResult carries the resolved USDC budget plus optional clamp
// telemetry for the close-attempt forensic log. When CollateralClamped is
// true, the BUY was downsized because the account did not have enough
// available collateral to fund the full hedge — the operator may want to
// cover the gap with another wallet or accept a partial hedge.
type HedgeSizingResult struct {
	SizeUSDC             float64 `json:"sizeUSDC"`
	ExpectedOdds         float64 `json:"expectedOdds"`
	RequestedUSDC        float64 `json:"requestedUSDC,omitempty"`
	AvailableCollateral  float64 `json:"availableCollateral,omitempty"`
	ReserveUSDC          float64 `json:"reserveUSDC,omitempty"`
	CollateralClamped    bool    `json:"collateralClamped,omitempty"`
}

// HedgeFOKBuySizing computes USDC budget and expected odds (0–1) for hedge_fok_buy on the opponent outcome token.
// markPrice01 is the held outcome mark in 0–1 (e.g. max(bid,ask) on YES); required for notional sizing.
//
// Backwards-compat wrapper. New callers should use
// HedgeFOKBuySizingWithCollateral so the planner cannot exceed available
// collateral.
func HedgeFOKBuySizing(ctx context.Context, client clob.Client, oppTokenID string, heldShares, markPrice01 float64, sizing string, buyExtraTicks int) (sizeUSDC, expectedOdds float64, err error) {
	tid, err := MustCLOBAssetIDForAPI(oppTokenID)
	if err != nil {
		return 0, 0, err
	}
	book, err := client.OrderBook(ctx, &clobtypes.BookRequest{TokenID: tid})
	if err != nil {
		return 0, 0, fmt.Errorf("hedge opponent orderbook: %w", err)
	}
	tick := ParseTickSize(book.TickSize)
	bestAsk, askOK := BestAskPrice(book.Asks)
	if !askOK || !isFinite(bestAsk) || bestAsk <= 0 {
		return 0, 0, fmt.Errorf("hedge: no ask on opponent token")
	}
	expectedOdds = bestAsk
	switch strings.TrimSpace(strings.ToLower(sizing)) {
	case "shares":
		cap := math.Min(1-tick, bestAsk+float64(buyExtraTicks)*tick)
		if !isFinite(cap) || cap <= 0 {
			return 0, 0, fmt.Errorf("hedge: invalid cap")
		}
		sizeUSDC = heldShares * cap
	default:
		if !isFinite(markPrice01) || markPrice01 <= 0 {
			return 0, 0, fmt.Errorf("hedge: zero mark price for notional sizing")
		}
		sizeUSDC = heldShares * markPrice01
	}
	if !isFinite(sizeUSDC) || sizeUSDC <= 0 {
		return 0, 0, fmt.Errorf("hedge: zero sizeUSDC")
	}
	return sizeUSDC, expectedOdds, nil
}

// HedgeFOKBuySizingWithCollateral wraps HedgeFOKBuySizing and clamps the
// resolved sizeUSDC to the account's available CLOB collateral minus a
// configurable reserve fraction. Prevents the FOK BUY from being rejected
// by CLOB for "not enough balance" mid-flight, which would force the
// operator to manually retry the hedge.
//
// reservePct is a fraction in [0, 0.5) representing the share of collateral
// to KEEP UNUSED (e.g. 0.05 = leave 5% headroom for fees and rounding).
// Out-of-range values are clamped to 0 (no reserve).
//
// minHedgeUSDC is the smallest hedge the caller is willing to submit. When
// the available collateral after reserve is below this, the function
// returns ok=false so the close path can abort cleanly rather than fire a
// dust-sized hedge that the CLOB lot size would reject.
func HedgeFOKBuySizingWithCollateral(
	ctx context.Context,
	client clob.Client,
	oppTokenID string,
	heldShares, markPrice01 float64,
	sizing string,
	buyExtraTicks int,
	availableCollateralUSDC float64,
	reservePct float64,
	minHedgeUSDC float64,
) (HedgeSizingResult, error) {
	requested, expected, err := HedgeFOKBuySizing(ctx, client, oppTokenID, heldShares, markPrice01, sizing, buyExtraTicks)
	if err != nil {
		return HedgeSizingResult{}, err
	}
	if reservePct < 0 || reservePct >= 0.5 {
		reservePct = 0
	}
	reserve := availableCollateralUSDC * reservePct
	usable := availableCollateralUSDC - reserve
	if usable < 0 {
		usable = 0
	}
	out := HedgeSizingResult{
		SizeUSDC:            requested,
		ExpectedOdds:        expected,
		RequestedUSDC:       requested,
		AvailableCollateral: availableCollateralUSDC,
		ReserveUSDC:         reserve,
	}
	if availableCollateralUSDC <= 0 {
		// Fail-open path used when the caller could not retrieve a balance:
		// pass through the requested sizing and let CLOB reject if it must.
		// Ledger 0 is treated as "unknown" rather than "broke".
		return out, nil
	}
	if requested > usable {
		out.SizeUSDC = usable
		out.CollateralClamped = true
	}
	if out.SizeUSDC < minHedgeUSDC {
		return out, fmt.Errorf("hedge_collateral_insufficient: requested=%.2f available=%.2f reserve=%.2f min=%.2f", requested, availableCollateralUSDC, reserve, minHedgeUSDC)
	}
	return out, nil
}

func timeNowMs() int64 { return time.Now().UnixMilli() }
