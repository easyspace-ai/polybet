package risksvc

import (
	"context"
	"fmt"
	"strings"

	"github.com/easyspace-ai/polybet/internal/gammaclient"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/polywiring"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/tg"
)

func (s *Service) runCloseFOKSell(ctx context.Context, cl *polywiring.AuthedCLOB, task store.RiskTask, pos *store.RiskPosition, taskID, positionID, queueReason string, sellExtra int, evalBidCents, evalAskCents, trailCents float64, modeExtra *closeAttemptExtras) error {
	// Slippage gate: refuse to submit a SELL whose projected limit floor
	// would drop more than riskCloseMaxSlippageBps below the eval bid.
	// Counts as a failed attempt so the ladder advances (next tier with
	// looser bps cap, or hedge_fok_buy at the end which bypasses the gate).
	if projBps, gateErr := s.checkCloseSlippage(ctx, evalBidCents, sellExtra, riskCloseModeFOKSell); gateErr != nil {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		ex := *modeExtra
		ex.SlippageProjectedBps = projBps
		j, mErr := marshalCloseAttemptSnapshot(pos, "fok_slippage_gate", evalBidCents, evalAskCents, sellExtra, nil, &ex, gateErr, "")
		if mErr == nil {
			s.persistCloseAttemptDetail(ctx, taskID, j)
		}
		s.log.WithFields(logx.Pairs(
			"task_id", taskID, "position_id", positionID, "token_id", pos.TokenID,
			"projected_slippage_bps", projBps, "extra_ticks", sellExtra, "eval_bid_cents", evalBidCents,
		)).Warn("风控：FOK 卖单被滑点上限拦截，等待下一 tier")
		return gateErr
	}
	submitMaxAgeMs := s.st.GetBotConfigInt(ctx, botKeyOrderSubmitMaxAgeMs, 0)
	orderID, rep, err := polyexec.ExecuteFOKSellWithOpts(ctx, cl.Client, cl.Signer, pos.TokenID, pos.SizeShares, sellExtra, submitMaxAgeMs)
	if err != nil {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		j, mErr := marshalCloseAttemptSnapshot(pos, "fok_submit_error", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, err, "")
		if mErr == nil {
			s.persistCloseAttemptDetail(ctx, taskID, j)
		}
		logFields := logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "err", err.Error(), "execution_mode", riskCloseModeFOKSell)
		if rep != nil {
			logFields["clob_best_bid"] = rep.BestBid
			logFields["clob_best_ask"] = rep.BestAsk
			logFields["limit_price_decimal"] = rep.LimitPriceDecimal
			logFields["limit_price"] = rep.LimitPrice
			logFields["shares_submitted"] = rep.SharesSubmitted
			logFields["order_id"] = rep.OrderID
			logFields["error_step"] = rep.ErrorStep
		}
		if mErr == nil {
			logFields["close_attempt"] = j
		}
		s.log.WithFields(logFields).Warn("风控：FOK 卖单失败")
		logx.StopLoss().WithFields(logFields).Warn("风控：FOK 卖单失败")
		if s.rt != nil {
			s.rt.Publish("position", "warn", "position.close_failed", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
				"taskId": taskID, "err": err.Error(), "reason": queueReason,
			})
		}
		if done, derr := s.tryCompleteCloseOnUnsellableDust(ctx, taskID, positionID, pos, rep, err); done || derr != nil {
			return derr
		}
		if done, derr := s.tryCompleteCloseOnStaleCLOBBalance(ctx, taskID, positionID, pos, rep, err); done || derr != nil {
			return derr
		}
		if reason := s.evaluateCloseTaskAbort(ctx, pos, err); reason != "" {
			j2, mErr2 := marshalCloseAttemptSnapshot(pos, "fok_submit_then_abort", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, err, string(reason))
			if mErr2 == nil {
				s.persistCloseAttemptDetail(ctx, taskID, j2)
			}
			abortFields := logx.Pairs("task_id", taskID, "position_id", positionID, "abort_reason", string(reason), "underlying_err", err.Error())
			if mErr2 == nil {
				abortFields["close_attempt"] = j2
			}
			s.log.WithFields(abortFields).Info("风控：FOK 失败后任务终止（不再重试）")
			logx.StopLoss().WithFields(abortFields).Info("风控：FOK 失败后任务终止（不再重试）")
			return s.abortCloseTask(ctx, taskID, positionID, pos, reason, err)
		}
		return err
	}
	okJ, okErr := marshalCloseAttemptSnapshot(pos, "fok_submit_ok", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, nil, "")
	if okErr == nil {
		s.persistCloseAttemptDetail(ctx, taskID, okJ)
	}
	okFields := logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "order_id", orderID, "execution_mode", riskCloseModeFOKSell)
	if rep != nil {
		okFields["clob_best_bid"] = rep.BestBid
		okFields["clob_best_ask"] = rep.BestAsk
		okFields["limit_price_decimal"] = rep.LimitPriceDecimal
		okFields["limit_price"] = rep.LimitPrice
		okFields["shares_submitted"] = rep.SharesSubmitted
	}
	if okErr == nil {
		okFields["close_attempt"] = okJ
	}
	s.log.WithFields(okFields).Info("风控：FOK 卖单已提交（CLOB 已接单）")
	logx.StopLoss().WithFields(okFields).Info("风控：FOK 卖单已提交（CLOB 已接单）")
	// FOK is all-or-nothing: limit price is the exact fill price for the
	// full position size. realizedPnL = (filledShares × fillPrice) - costBasis.
	// Captured here so the kill-switch evaluator can include closed-today
	// losses, not just unrealized.
	realizedPnL := pos.SizeShares*rep.LimitPrice - pos.CostUSD
	if err := s.st.CloseRiskPositionPnL(ctx, positionID, realizedPnL); err != nil {
		s.log.WithFields(logx.Pairs("position_id", positionID, "err", err.Error())).Error("风控：数据库关闭持仓失败")
		return err
	}
	s.clearStopLossMarketEndedCooldown(positionID)
	doneJ, dErr := marshalCloseAttemptSnapshot(pos, "position_closed", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, nil, "")
	if dErr == nil {
		s.persistCloseAttemptDetail(ctx, taskID, doneJ)
	}
	// FOK fills entirely at the limit floor or not at all, so limitPrice is
	// the actual fill. expected = bestBid at decision time (evalBidCents/100).
	expected01 := evalBidCents / 100.0
	// Realized PnL = filledShares × fillPrice − costBasis. FOK guarantees
	// full size at the limit, so this is the exact realized number — fed
	// to trade_quality so /api/trade-quality/aggregate can show today's
	// realized PnL alongside slippage.
	fokRealizedPnL := pos.SizeShares*rep.LimitPrice - pos.CostUSD
	if modeExtra != nil {
		modeExtra.RealizedPnLUSD = fokRealizedPnL
	}
	_ = s.st.InsertTradeQuality(ctx, &store.TradeQuality{
		AccountID:      pos.AccountID,
		Side:           "sell",
		OrderType:      "FOK",
		TokenID:        pos.TokenID,
		ExpectedOdds:   expected01,
		FillOdds:       rep.LimitPrice,
		LimitOdds:      rep.LimitPrice,
		BestBid:        rep.BestBid,
		BestAsk:        rep.BestAsk,
		SlippageBps:    store.SlippageBpsSell(expected01, rep.LimitPrice),
		Size:           pos.SizeShares,
		RealizedPnLUSD: fokRealizedPnL,
		RiskTaskID:     taskID,
		Notes:          "runCloseFOKSell",
	})
	_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
	doneFields := logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "order_id", orderID,
		"closed_shares", pos.SizeShares, "limit_price_decimal", rep.LimitPriceDecimal, "trail_cents", trailCents, "execution_mode", riskCloseModeFOKSell,
		"slippage_bps", store.SlippageBpsSell(expected01, rep.LimitPrice))
	if dErr == nil {
		doneFields["close_attempt"] = doneJ
	}
	s.log.WithFields(doneFields).Info("风控：平仓成交")
	logx.StopLoss().WithFields(doneFields).Info("风控：平仓成交")
	if s.rt != nil {
		s.rt.Publish("position", "info", "position.closed", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
			"taskId": taskID, "reason": queueReason, "sizeShares": pos.SizeShares,
		})
	}
	tg.Notify(ctx, s.cfg, s.st, s.log, fmt.Sprintf(
		"Polybet 平仓成交\n%s\n份额 %.2f · token %s",
		strings.TrimSpace(pos.Title),
		pos.SizeShares,
		pos.TokenID,
	))
	tg.MaybeNotifyCollateralChanged(s.cfg, s.log, s.st)
	return s.st.SetRiskTaskSucceeded(ctx, taskID)
}

func (s *Service) runCloseFAKSell(ctx context.Context, cl *polywiring.AuthedCLOB, task store.RiskTask, pos *store.RiskPosition, taskID, positionID, queueReason string, sellExtra int, evalBidCents, evalAskCents, trailCents float64, modeExtra *closeAttemptExtras) error {
	worst := s.st.GetBotConfigFloat(ctx, botKeyRiskCloseFakWorstPrice, 0.01)
	return s.runCloseFAKSellWithWorst(ctx, cl, task, pos, taskID, positionID, queueReason, sellExtra, worst, evalBidCents, evalAskCents, trailCents, modeExtra)
}

// runCloseFAKSellWithWorst is the underlying FAK sell driver with an
// explicit worstPrice (0–1) so the ladder can override per-tier without
// touching the global riskCloseFakWorstPrice config.
func (s *Service) runCloseFAKSellWithWorst(ctx context.Context, cl *polywiring.AuthedCLOB, task store.RiskTask, pos *store.RiskPosition, taskID, positionID, queueReason string, sellExtra int, worst, evalBidCents, evalAskCents, trailCents float64, modeExtra *closeAttemptExtras) error {
	_ = trailCents
	// Slippage gate (same rule as FOK; hedge_fok_buy is exempt).
	if projBps, gateErr := s.checkCloseSlippage(ctx, evalBidCents, sellExtra, riskCloseModeFAKSell); gateErr != nil {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		ex := *modeExtra
		ex.SlippageProjectedBps = projBps
		j, mErr := marshalCloseAttemptSnapshot(pos, "fak_slippage_gate", evalBidCents, evalAskCents, sellExtra, nil, &ex, gateErr, "")
		if mErr == nil {
			s.persistCloseAttemptDetail(ctx, taskID, j)
		}
		s.log.WithFields(logx.Pairs(
			"task_id", taskID, "position_id", positionID, "token_id", pos.TokenID,
			"projected_slippage_bps", projBps, "extra_ticks", sellExtra, "eval_bid_cents", evalBidCents,
		)).Warn("风控：FAK 卖单被滑点上限拦截，等待下一 tier")
		return gateErr
	}
	submitMaxAgeMs := s.st.GetBotConfigInt(ctx, botKeyOrderSubmitMaxAgeMs, 0)
	orderID, rep, err := polyexec.ExecuteFAKSellWithOpts(ctx, cl.Client, cl.Signer, pos.TokenID, pos.SizeShares, worst, submitMaxAgeMs)
	if err != nil {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		j, mErr := marshalCloseAttemptSnapshot(pos, "fak_submit_error", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, err, "")
		if mErr == nil {
			s.persistCloseAttemptDetail(ctx, taskID, j)
		}
		logFields := logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "err", err.Error(), "execution_mode", riskCloseModeFAKSell)
		if rep != nil {
			logFields["clob_best_bid"] = rep.BestBid
			logFields["clob_best_ask"] = rep.BestAsk
			logFields["limit_price_decimal"] = rep.LimitPriceDecimal
			logFields["limit_price"] = rep.LimitPrice
			logFields["shares_submitted"] = rep.SharesSubmitted
			logFields["order_id"] = rep.OrderID
			logFields["error_step"] = rep.ErrorStep
		}
		if mErr == nil {
			logFields["close_attempt"] = j
		}
		s.log.WithFields(logFields).Warn("风控：FAK 卖单失败")
		logx.StopLoss().WithFields(logFields).Warn("风控：FAK 卖单失败")
		if s.rt != nil {
			s.rt.Publish("position", "warn", "position.close_failed", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
				"taskId": taskID, "err": err.Error(), "reason": queueReason,
			})
		}
		if done, derr := s.tryCompleteCloseOnUnsellableDust(ctx, taskID, positionID, pos, rep, err); done || derr != nil {
			return derr
		}
		if done, derr := s.tryCompleteCloseOnStaleCLOBBalance(ctx, taskID, positionID, pos, rep, err); done || derr != nil {
			return derr
		}
		if reason := s.evaluateCloseTaskAbort(ctx, pos, err); reason != "" {
			j2, mErr2 := marshalCloseAttemptSnapshot(pos, "fak_submit_then_abort", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, err, string(reason))
			if mErr2 == nil {
				s.persistCloseAttemptDetail(ctx, taskID, j2)
			}
			return s.abortCloseTask(ctx, taskID, positionID, pos, reason, err)
		}
		return err
	}
	okJ, okErr := marshalCloseAttemptSnapshot(pos, "fak_submit_ok", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, nil, "")
	if okErr == nil {
		s.persistCloseAttemptDetail(ctx, taskID, okJ)
	}
	if syncErr := s.SyncPositionsFromDataAPI(ctx, pos.AccountID); syncErr != nil && s.log != nil {
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "err", syncErr.Error())).Warn("风控：FAK 成交后同步持仓失败（将按本地份额重试）")
	}
	min := s.minShares(ctx)
	fresh, ferr := s.st.GetRiskPosition(ctx, positionID)
	if ferr != nil || fresh == nil || fresh.Status == "closed" {
		s.clearStopLossMarketEndedCooldown(positionID)
		_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
		doneJ, dErr := marshalCloseAttemptSnapshot(pos, "position_closed", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, nil, "")
		if dErr == nil {
			s.persistCloseAttemptDetail(ctx, taskID, doneJ)
		}
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "order_id", orderID)).Info("风控：FAK 后同步显示已无持仓，任务完成")
		return s.st.SetRiskTaskSucceeded(ctx, taskID)
	}
	if (fresh.Status == "open" || fresh.Status == "closing") && fresh.SizeShares >= min {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		partialJ, pErr := marshalCloseAttemptSnapshot(pos, "fak_partial_remaining", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, errPartialFillRemaining, "")
		if pErr == nil {
			s.persistCloseAttemptDetail(ctx, taskID, partialJ)
		}
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "remaining_shares", fresh.SizeShares, "min_shares", min)).Warn("风控：FAK 部分成交，保留持仓并重试")
		return errPartialFillRemaining
	}
	// FAK fill price is unknown without a per-order detail call. We use the
	// limit floor as a conservative proxy (worst-case the trader actually
	// got) so realized PnL is never overstated for the kill switch.
	realizedPnLFAK := pos.SizeShares*rep.LimitPrice - pos.CostUSD
	if err := s.st.CloseRiskPositionPnL(ctx, positionID, realizedPnLFAK); err != nil {
		s.log.WithFields(logx.Pairs("position_id", positionID, "err", err.Error())).Error("风控：FAK 后关闭 dust 持仓失败")
		return err
	}
	s.clearStopLossMarketEndedCooldown(positionID)
	doneJ, dErr := marshalCloseAttemptSnapshot(pos, "position_closed", evalBidCents, evalAskCents, sellExtra, rep, modeExtra, nil, "")
	if dErr == nil {
		s.persistCloseAttemptDetail(ctx, taskID, doneJ)
	}
	// FAK fill price is unknown without a per-order detail call; the limit
	// is the worst-case fill, so use it as a conservative proxy. expected =
	// best bid at decision time. Marked notes="proxy" so analytics can
	// distinguish from FOK exact-fill rows. RealizedPnL uses the same
	// conservative proxy: never overstate realized profit.
	expected01 := evalBidCents / 100.0
	fakRealizedPnL := pos.SizeShares*rep.LimitPrice - pos.CostUSD
	if modeExtra != nil {
		modeExtra.RealizedPnLUSD = fakRealizedPnL
	}
	_ = s.st.InsertTradeQuality(ctx, &store.TradeQuality{
		AccountID:      pos.AccountID,
		Side:           "sell",
		OrderType:      "FAK",
		TokenID:        pos.TokenID,
		ExpectedOdds:   expected01,
		FillOdds:       rep.LimitPrice, // proxy
		LimitOdds:      rep.LimitPrice,
		BestBid:        rep.BestBid,
		BestAsk:        rep.BestAsk,
		SlippageBps:    store.SlippageBpsSell(expected01, rep.LimitPrice),
		Size:           pos.SizeShares,
		RealizedPnLUSD: fakRealizedPnL,
		RiskTaskID:     taskID,
		Notes:          "runCloseFAKSell:proxy_limit_as_fill",
	})
	_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
	s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "order_id", orderID,
		"slippage_bps", store.SlippageBpsSell(expected01, rep.LimitPrice))).Info("风控：FAK 平仓完成（剩余 dust）")
	if s.rt != nil {
		s.rt.Publish("position", "info", "position.closed", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
			"taskId": taskID, "reason": queueReason, "sizeShares": pos.SizeShares,
		})
	}
	tg.Notify(ctx, s.cfg, s.st, s.log, fmt.Sprintf(
		"Polybet 平仓成交（FAK）\n%s\n份额 %.2f · token %s",
		strings.TrimSpace(pos.Title),
		pos.SizeShares,
		pos.TokenID,
	))
	tg.MaybeNotifyCollateralChanged(s.cfg, s.log, s.st)
	return s.st.SetRiskTaskSucceeded(ctx, taskID)
}

func (s *Service) runCloseHedgeFOKBuy(ctx context.Context, cl *polywiring.AuthedCLOB, task store.RiskTask, pos *store.RiskPosition, taskID, positionID, queueReason string, sellExtra int, evalBidCents, evalAskCents, trailCents float64, modeExtra *closeAttemptExtras) error {
	_ = trailCents
	buyExtra := s.st.GetBotConfigInt(ctx, "polymarketFokBuyExtraTicks", 5)
	hedgeSizing := effectiveRiskHedgeBuySizing(ctx, s.st)
	opp, terr := gammaclient.OppositeCLOBTokenID(ctx, s.cfg.HTTPPlatformProxy, pos.TokenID)
	if terr != nil {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		ex := &closeAttemptExtras{ExecutionMode: modeExtra.ExecutionMode, HedgeSizing: hedgeSizing}
		j, mErr := marshalCloseAttemptSnapshot(pos, "hedge_token_error", evalBidCents, evalAskCents, sellExtra, nil, ex, terr, "")
		if mErr == nil {
			s.persistCloseAttemptDetail(ctx, taskID, j)
		}
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "err", terr.Error())).Warn("风控：对冲模式无法解析对手 token")
		return terr
	}
	mark01 := markPrice01FromEvalCents(evalBidCents, evalAskCents)
	// Read the live CLOB collateral so the hedge BUY can never request more
	// USDC than the account has — without this clamp, the FOK was sometimes
	// rejected by CLOB for "not enough balance" leaving the original
	// position uncovered.
	availableCollateral, balErr := balancesvc.FetchCLOBCollateralUSD(ctx, cl)
	if balErr != nil && s.log != nil {
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "err", balErr.Error())).Warn("风控：读取 CLOB 抵押余额失败，回退按请求名义对冲")
		availableCollateral = 0 // 0 = "unknown" → fail-open in HedgeFOKBuySizingWithCollateral
	}
	reservePct := s.st.GetBotConfigFloat(ctx, "riskHedgeCollateralReservePct", 0.05)
	minHedge := s.st.GetBotConfigFloat(ctx, "riskHedgeMinUSDC", 1.0)
	hedgeRes, serr := polyexec.HedgeFOKBuySizingWithCollateral(ctx, cl.Client, opp, pos.SizeShares, mark01, hedgeSizing, buyExtra, availableCollateral, reservePct, minHedge)
	if serr != nil {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		ex := &closeAttemptExtras{
			ExecutionMode:        modeExtra.ExecutionMode,
			HedgeTokenID:         opp,
			HedgeSizing:          hedgeSizing,
			HedgeRequestedUSDC:   hedgeRes.RequestedUSDC,
			HedgeAvailableUSDC:   hedgeRes.AvailableCollateral,
			HedgeCollateralClamp: hedgeRes.CollateralClamped,
		}
		j, mErr := marshalCloseAttemptSnapshot(pos, "hedge_sizing_error", evalBidCents, evalAskCents, sellExtra, nil, ex, serr, "")
		if mErr == nil {
			s.persistCloseAttemptDetail(ctx, taskID, j)
		}
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "err", serr.Error())).Warn("风控：对冲预算计算失败")
		return serr
	}
	sizeUSDC := hedgeRes.SizeUSDC
	expectedOdds := hedgeRes.ExpectedOdds
	submitMaxAgeMs := s.st.GetBotConfigInt(ctx, botKeyOrderSubmitMaxAgeMs, 0)
	orderID, fillOdds, buyRep, err := polyexec.ExecuteFOKBuyWithOpts(ctx, cl.Client, cl.Signer, opp, sizeUSDC, expectedOdds, buyExtra, submitMaxAgeMs)
	extras := &closeAttemptExtras{
		ExecutionMode:        modeExtra.ExecutionMode,
		HedgeTokenID:         opp,
		HedgeSizing:          hedgeSizing,
		BuyRep:               buyRep,
		HedgeRequestedUSDC:   hedgeRes.RequestedUSDC,
		HedgeAvailableUSDC:   hedgeRes.AvailableCollateral,
		HedgeCollateralClamp: hedgeRes.CollateralClamped,
	}
	if hedgeRes.CollateralClamped && s.log != nil {
		s.log.WithFields(logx.Pairs(
			"task_id", taskID, "position_id", positionID,
			"requested_usdc", hedgeRes.RequestedUSDC, "size_usdc", sizeUSDC,
			"available_collateral", hedgeRes.AvailableCollateral, "reserve_usdc", hedgeRes.ReserveUSDC,
		)).Warn("风控：对冲预算被抵押余额限制下调（部分对冲）")
	}
	if err != nil {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		j, mErr := marshalCloseAttemptSnapshot(pos, "hedge_submit_error", evalBidCents, evalAskCents, buyExtra, nil, extras, err, "")
		if mErr == nil {
			s.persistCloseAttemptDetail(ctx, taskID, j)
		}
		logFields := logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "hedge_token", opp, "err", err.Error())
		if mErr == nil {
			logFields["close_attempt"] = j
		}
		s.log.WithFields(logFields).Warn("风控：对冲 FOK 买单失败")
		logx.StopLoss().WithFields(logFields).Warn("风控：对冲 FOK 买单失败")
		if s.rt != nil {
			s.rt.Publish("position", "warn", "position.hedge_failed", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
				"taskId": taskID, "err": err.Error(), "reason": queueReason,
			})
		}
		if reason := s.evaluateCloseTaskAbort(ctx, pos, err); reason != "" {
			j2, mErr2 := marshalCloseAttemptSnapshot(pos, "hedge_submit_then_abort", evalBidCents, evalAskCents, buyExtra, nil, extras, err, string(reason))
			if mErr2 == nil {
				s.persistCloseAttemptDetail(ctx, taskID, j2)
			}
			return s.abortCloseTask(ctx, taskID, positionID, pos, reason, err)
		}
		return err
	}
	okJ, okErr := marshalCloseAttemptSnapshot(pos, "hedge_submit_ok", evalBidCents, evalAskCents, buyExtra, nil, extras, nil, "")
	if okErr == nil {
		s.persistCloseAttemptDetail(ctx, taskID, okJ)
	}
	if syncErr := s.SyncPositionsFromDataAPI(ctx, pos.AccountID); syncErr != nil && s.log != nil {
		s.log.WithFields(logx.Pairs("task_id", taskID, "err", syncErr.Error())).Warn("风控：对冲后同步持仓失败（继续完成任务）")
	}
	_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
	if riskHedgeAutoHideDefaultTrue(ctx, s.st) {
		if herr := s.st.UpsertRiskHiddenPosition(ctx, pos.AccountID, pos.TokenID, pos.SideLabel); herr != nil && s.log != nil {
			s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "err", herr.Error())).Warn("风控：对冲成功后写入「不再监控」失败")
		}
	}
	// Persist execution-quality for the hedge BUY leg. expectedOdds was the
	// asker the hedge-sizing pre-fetch saw; fillOdds is the actual limit.
	_ = s.st.InsertTradeQuality(ctx, &store.TradeQuality{
		AccountID:    pos.AccountID,
		Side:         "buy",
		OrderType:    "hedge_fok_buy",
		TokenID:      opp,
		ExpectedOdds: expectedOdds,
		FillOdds:     fillOdds,
		LimitOdds:    fillOdds,
		SlippageBps:  store.SlippageBpsBuy(expectedOdds, fillOdds),
		Size:         sizeUSDC,
		RiskTaskID:   taskID,
		Notes:        "runCloseHedgeFOKBuy",
	})
	_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
	s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "order_id", orderID,
		"hedge_token", opp, "size_usdc", sizeUSDC, "fill_odds", fillOdds, "execution_mode", riskCloseModeHedgeFOKBuy,
		"slippage_bps", store.SlippageBpsBuy(expectedOdds, fillOdds))).Info("风控：对冲 FOK 买单已提交（原 YES 仓仍在）")
	logx.StopLoss().WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "order_id", orderID)).Info("风控：对冲 FOK 买单完成")
	if s.rt != nil {
		s.rt.Publish("position", "info", "position.hedge_done", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
			"taskId": taskID, "hedgeToken": opp, "sizeUSDC": sizeUSDC, "orderId": orderID, "reason": queueReason,
		})
	}
	tg.Notify(ctx, s.cfg, s.st, s.log, fmt.Sprintf(
		"Polybet 对冲买单（FOK）\n%s\n原持仓仍在 · 对手 token\n%s\n约 $%.2f @ 限价 ~%.1f¢ · order %s",
		strings.TrimSpace(pos.Title),
		opp,
		sizeUSDC,
		fillOdds*100,
		orderID,
	))
	tg.MaybeNotifyCollateralChanged(s.cfg, s.log, s.st)
	return s.st.SetRiskTaskSucceeded(ctx, taskID)
}
