package risksvc

import (
	"context"
	"math"
	"strings"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/store"
)

// tryCompleteCloseOnUnsellableDust closes the DB position when on-chain balance is below the
// CLOB market sell lot (0.01 shares) so retry loops do not hammer build with "amount must be positive".
func (s *Service) tryCompleteCloseOnUnsellableDust(ctx context.Context, taskID, positionID string, pos *store.RiskPosition, rep *polyexec.FOKSellReport, err error) (bool, error) {
	if pos == nil || err == nil {
		return false, nil
	}
	belowLot := polyexec.IsSellSharesBelowCLOBLot(err)
	if !belowLot && rep != nil && rep.ErrorStep == "below_min_lot" {
		belowLot = true
	}
	if !belowLot {
		return false, nil
	}
	if syncErr := s.SyncPositionsFromDataAPI(ctx, pos.AccountID); syncErr != nil && s.log != nil {
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "err", syncErr.Error())).Warn("风控：dust 平仓前同步持仓失败")
	}
	if err := s.st.CloseRiskPosition(ctx, positionID); err != nil {
		return false, err
	}
	s.clearStopLossMarketEndedCooldown(positionID)
	_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
	fields := logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID)
	if rep != nil {
		fields["on_chain_shares"] = rep.OnChainBalanceShares
		fields["shares_submitted"] = rep.SharesSubmitted
	}
	if strings.Contains(err.Error(), "raw=") {
		fields["underlying_err"] = err.Error()
	}
	s.log.WithFields(fields).Info("风控：链上 dust 不可卖，已关闭本地持仓")
	logx.StopLoss().WithFields(fields).Info("风控：链上 dust 不可卖，已关闭本地持仓")
	if s.rt != nil {
		s.rt.Publish("position", "info", "position.closed", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
			"taskId": taskID, "reason": "unsellable_dust",
		})
	}
	return true, s.st.SetRiskTaskSucceeded(ctx, taskID)
}

// isStaleCLOBBalanceSellFailure is true when CLOB reports no sellable conditional balance
// while the local position still shows shares (stale DB or race at create_order).
func isStaleCLOBBalanceSellFailure(rep *polyexec.FOKSellReport, err error) bool {
	if err == nil {
		return false
	}
	if polyexec.IsZeroConditionalBalance(err) {
		return true
	}
	return rep != nil && rep.ErrorStep == "create_order" && polyexec.IsCLOBInsufficientSellBalance(err)
}

// staleCLOBBalanceReconcileAction decides the next step after syncing from the Data API.
// Returns retry (caller should re-queue), complete (position already gone), or close_ghost.
func staleCLOBBalanceReconcileAction(prevShares, freshShares float64, freshClosed bool, min float64) string {
	if freshClosed {
		return "complete"
	}
	if freshShares < min {
		return "close_dust"
	}
	if prevShares-freshShares >= min*0.5 && freshShares >= min {
		return "retry"
	}
	return "close_ghost"
}

// tryCompleteCloseOnStaleCLOBBalance syncs official positions and stops retry loops when
// CLOB balance is zero but the DB row still shows sellable shares.
func (s *Service) tryCompleteCloseOnStaleCLOBBalance(ctx context.Context, taskID, positionID string, pos *store.RiskPosition, rep *polyexec.FOKSellReport, err error) (bool, error) {
	if pos == nil || err == nil || !isStaleCLOBBalanceSellFailure(rep, err) {
		return false, nil
	}
	prevShares := pos.SizeShares
	if syncErr := s.SyncPositionsFromDataAPI(ctx, pos.AccountID); syncErr != nil && s.log != nil {
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "err", syncErr.Error())).Warn("风控：零余额平仓前同步持仓失败")
	}
	fresh, ferr := s.st.GetRiskPosition(ctx, positionID)
	if ferr != nil {
		return false, ferr
	}
	min := s.minShares(ctx)
	freshClosed := fresh == nil || fresh.Status == "closed"
	freshShares := 0.0
	if fresh != nil {
		freshShares = fresh.SizeShares
	}
	switch staleCLOBBalanceReconcileAction(prevShares, freshShares, freshClosed, min) {
	case "complete":
		s.clearStopLossMarketEndedCooldown(positionID)
		_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
		fields := logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "reason", "clob_zero_balance_synced_gone")
		s.log.WithFields(fields).Info("风控：CLOB 零余额同步后官方已无持仓，任务完成")
		logx.StopLoss().WithFields(fields).Info("风控：CLOB 零余额同步后官方已无持仓，任务完成")
		return true, s.st.SetRiskTaskSucceeded(ctx, taskID)
	case "retry":
		fields := logx.Pairs("task_id", taskID, "position_id", positionID, "prev_shares", prevShares, "fresh_shares", freshShares)
		s.log.WithFields(fields).Info("风控：CLOB 零余额同步后份额已修正，将重试平仓")
		logx.StopLoss().WithFields(fields).Info("风控：CLOB 零余额同步后份额已修正，将重试平仓")
		return false, nil
	default:
		if fresh != nil && fresh.Status != "closed" {
			if cerr := s.st.CloseRiskPosition(ctx, positionID); cerr != nil {
				return false, cerr
			}
		}
		s.clearStopLossMarketEndedCooldown(positionID)
		_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
		fields := logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "prev_shares", prevShares, "fresh_shares", freshShares)
		if rep != nil {
			fields["on_chain_shares"] = rep.OnChainBalanceShares
			fields["shares_submitted"] = rep.SharesSubmitted
		}
		reason := "clob_zero_balance_ghost_closed"
		if math.Abs(freshShares) < min {
			reason = "clob_zero_balance_synced_dust"
		}
		fields["reason"] = reason
		s.log.WithFields(fields).Info("风控：CLOB 零余额已关闭本地持仓（不再重试）")
		logx.StopLoss().WithFields(fields).Info("风控：CLOB 零余额已关闭本地持仓（不再重试）")
		if s.rt != nil {
			s.rt.Publish("position", "info", "position.closed", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
				"taskId": taskID, "reason": reason,
			})
		}
		return true, s.st.SetRiskTaskSucceeded(ctx, taskID)
	}
}
