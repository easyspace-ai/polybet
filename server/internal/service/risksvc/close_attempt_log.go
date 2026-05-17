package risksvc

import (
	"encoding/json"
	"time"

	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/store"
)

// closeAttemptExtras carries hedge / execution-mode fields not present on FOKSellReport alone.
type closeAttemptExtras struct {
	ExecutionMode string
	HedgeTokenID  string
	HedgeSizing   string // notional | shares
	BuyRep        *polyexec.FOKBuyReport
	LadderTier    string // populated by ladder mode: which tier executed this attempt
	LadderAttempt int    // 0-based attempt index that selected the tier

	// Hedge collateral telemetry (set by runCloseHedgeFOKBuy).
	HedgeRequestedUSDC   float64
	HedgeAvailableUSDC   float64
	HedgeCollateralClamp bool

	// Pre-submit slippage projection (only populated when the slippage
	// gate is configured > 0). Helps operators tune the cap.
	SlippageProjectedBps float64

	// Realized PnL recorded at SELL completion (= filledShares ×
	// fillPrice − costBasis). Surfaced in the forensic JSON so the
	// dashboard can render closure outcomes inline. Set by the close
	// paths after ExecuteFOK(/FAK)Sell succeeds.
	RealizedPnLUSD float64
>>>>>>> dcdb3c8 (feat(risk): SELL slippage cap so FOK/FAK refuses panic-dump prices)
}

// closeAttemptSnapshot is stored in risk_tasks.last_attempt_detail and returned to the API for replay.
type closeAttemptSnapshot struct {
	Phase                string  `json:"phase"`
	AtRFC3339Nano        string  `json:"at"`
	TrailCents           float64 `json:"trailCents,omitempty"`
	HighWaterCents       float64 `json:"highWaterCents,omitempty"`
	StopLossPct          float64 `json:"stopLossPct,omitempty"`
	EvalBidCents         float64 `json:"evalBidCents,omitempty"`
	EvalAskCents         float64 `json:"evalAskCents,omitempty"`
	ExecutionMode        string  `json:"executionMode,omitempty"`
	HedgeTokenID         string  `json:"hedgeTokenId,omitempty"`
	HedgeSizing          string  `json:"hedgeSizing,omitempty"`
	LadderTier           string  `json:"ladderTier,omitempty"`
	LadderAttempt        int     `json:"ladderAttempt,omitempty"`
	HedgeRequestedUSDC   float64 `json:"hedgeRequestedUSDC,omitempty"`
	HedgeAvailableUSDC   float64 `json:"hedgeAvailableUSDC,omitempty"`
	HedgeCollateralClamp bool    `json:"hedgeCollateralClamp,omitempty"`
	SlippageProjectedBps float64 `json:"slippageProjectedBps,omitempty"`
	RealizedPnLUSD       float64 `json:"realizedPnLUSD,omitempty"`
	Side                 string  `json:"side,omitempty"` // SELL | BUY
	CLOBTokenID          string  `json:"clobTokenId,omitempty"`
	TickSize             string  `json:"tickSize,omitempty"`
	ExtraTicks           int     `json:"extraTicks,omitempty"`
	BestBid              float64 `json:"bestBid,omitempty"`
	BestAsk              float64 `json:"bestAsk,omitempty"`
	LimitPrice           float64 `json:"limitPrice,omitempty"`
	LimitPriceDecimal    string  `json:"limitPriceDecimal,omitempty"`
	LimitPriceCents      float64 `json:"limitPriceCents,omitempty"`
	SharesSubmitted      float64 `json:"sharesSubmitted,omitempty"`
	PositionShares       float64 `json:"positionSharesRequested,omitempty"`
	OnChainBalanceShares float64 `json:"onChainBalanceShares,omitempty"`
	SizeUSDC             float64 `json:"sizeUSDC,omitempty"`
	ExpectedOdds         float64 `json:"expectedOdds,omitempty"`
	OrderType            string  `json:"orderType,omitempty"`
	WorstPriceConfigured float64 `json:"worstPriceConfigured,omitempty"`
	OrderID              string  `json:"orderId,omitempty"`
	ErrorStep            string  `json:"errorStep,omitempty"`
	Err                  string  `json:"error,omitempty"`
	AbortReason          string  `json:"abortReason,omitempty"`
}

func marshalCloseAttemptSnapshot(
	pos *store.RiskPosition,
	phase string,
	evalBidCents, evalAskCents float64,
	sellExtra int,
	sellRep *polyexec.FOKSellReport,
	extras *closeAttemptExtras,
	apiErr error,
	abortReason string,
) (string, error) {
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if sellRep != nil && sellRep.AtRFC3339Nano != "" {
		at = sellRep.AtRFC3339Nano
	}
	if extras != nil && extras.BuyRep != nil && extras.BuyRep.AtRFC3339Nano != "" {
		at = extras.BuyRep.AtRFC3339Nano
	}
	snap := closeAttemptSnapshot{
		Phase:         phase,
		AtRFC3339Nano: at,
		ExtraTicks:    sellExtra,
		EvalBidCents:  evalBidCents,
		EvalAskCents:  evalAskCents,
	}
	if abortReason != "" {
		snap.AbortReason = abortReason
	}
	if pos != nil {
		snap.HighWaterCents = FloorCents1(pos.HighWaterCents)
		snap.StopLossPct = pos.StopLossPct
		snap.TrailCents = TrailingStopCentsFromHW(snap.HighWaterCents, pos.StopLossPct)
		snap.PositionShares = pos.SizeShares
	}
	if extras != nil {
		snap.ExecutionMode = extras.ExecutionMode
		snap.HedgeTokenID = extras.HedgeTokenID
		snap.HedgeSizing = extras.HedgeSizing
		snap.LadderTier = extras.LadderTier
		snap.LadderAttempt = extras.LadderAttempt
		snap.HedgeRequestedUSDC = extras.HedgeRequestedUSDC
		snap.HedgeAvailableUSDC = extras.HedgeAvailableUSDC
		snap.HedgeCollateralClamp = extras.HedgeCollateralClamp
		snap.SlippageProjectedBps = extras.SlippageProjectedBps
		snap.RealizedPnLUSD = extras.RealizedPnLUSD
	}
	if sellRep != nil {
		snap.Side = "SELL"
		snap.CLOBTokenID = sellRep.CLOBTokenID
		snap.TickSize = sellRep.TickSize
		if sellRep.ExtraTicks != 0 {
			snap.ExtraTicks = sellRep.ExtraTicks
		}
		snap.BestBid = sellRep.BestBid
		snap.BestAsk = sellRep.BestAsk
		snap.LimitPrice = sellRep.LimitPrice
		snap.LimitPriceDecimal = sellRep.LimitPriceDecimal
		if sellRep.LimitPrice > 0 {
			snap.LimitPriceCents = sellRep.LimitPrice * 100
		}
		snap.SharesSubmitted = sellRep.SharesSubmitted
		if sellRep.SharesRequested > 0 {
			snap.PositionShares = sellRep.SharesRequested
		}
		snap.OnChainBalanceShares = sellRep.OnChainBalanceShares
		snap.OrderID = sellRep.OrderID
		snap.ErrorStep = sellRep.ErrorStep
		snap.OrderType = sellRep.OrderType
		snap.WorstPriceConfigured = sellRep.WorstPriceConfigured
	}
	if extras != nil && extras.BuyRep != nil {
		br := extras.BuyRep
		snap.Side = "BUY"
		snap.CLOBTokenID = br.CLOBTokenID
		snap.TickSize = br.TickSize
		if br.ExtraTicks != 0 {
			snap.ExtraTicks = br.ExtraTicks
		}
		snap.BestBid = br.BestBid
		snap.BestAsk = br.BestAsk
		snap.LimitPrice = br.LimitPrice
		snap.LimitPriceDecimal = br.LimitPriceDecimal
		if br.LimitPrice > 0 {
			snap.LimitPriceCents = br.LimitPrice * 100
		}
		snap.SizeUSDC = br.SizeUSDC
		snap.ExpectedOdds = br.ExpectedOdds
		snap.OrderID = br.OrderID
		snap.ErrorStep = br.ErrorStep
		snap.OrderType = "FOK"
	}
	if apiErr != nil {
		snap.Err = apiErr.Error()
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
