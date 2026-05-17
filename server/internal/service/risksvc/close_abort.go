package risksvc

import (
	"context"
	"errors"
	"strings"

	"github.com/easyspace-ai/polybet/internal/gammaclient"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/polywarm"
	"github.com/easyspace-ai/polybet/internal/store"
)

// errCloseTaskAborted signals the task was cancelled intentionally (no retry).
var errCloseTaskAborted = errors.New("close_task_aborted")

// closeTaskAbortReason is a stable machine reason stored on risk_tasks.last_error.
type closeTaskAbortReason string

const (
	closeAbortPositionClosed closeTaskAbortReason = "aborted:position_closed"
	closeAbortNotMonitored   closeTaskAbortReason = "aborted:not_monitored"
	closeAbortMarketEnded    closeTaskAbortReason = "aborted:market_ended"
)

func isNoOrderbookError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no orderbook exists") ||
		(strings.Contains(msg, "get orderbook") && strings.Contains(msg, "404"))
}

// gammaMarketRowPresent distinguishes a real Gamma /markets row from an in-memory
// cache placeholder (fetch miss / error) where TokenMarketDisplay is the zero value
// but the token key was still inserted into the batch map.
func gammaMarketRowPresent(meta gammaclient.TokenMarketDisplay) bool {
	return strings.TrimSpace(meta.TokenID) != "" ||
		strings.TrimSpace(meta.Question) != "" ||
		strings.TrimSpace(meta.ConditionID) != ""
}

// gammaMarketEndedForAbort is true when Gamma credibly reports the market is not tradeable.
func gammaMarketEndedForAbort(meta gammaclient.TokenMarketDisplay, found bool) bool {
	if !found || !gammaMarketRowPresent(meta) {
		return false
	}
	return meta.Closed || !meta.Active
}

// closeTaskHasLiveLiquiditySignals is true when the CLOB still exposes a book for this token:
// cached or REST top-of-book has a non-zero bid or ask, or GET /book returns 200 with decodable JSON
// (orderbook exists even if both sides are empty).
func (s *Service) closeTaskHasLiveLiquiditySignals(ctx context.Context, tokenID string) bool {
	if s == nil || s.cfg == nil {
		return false
	}
	if b, a, ok := s.BestBidAskCents(ctx, tokenID); ok && (b > 0 || a > 0) {
		return true
	}
	tid := store.NormalizeRiskCLOBTokenID(tokenID)
	if tid == "" {
		return false
	}
	return polywarm.BookJSONHTTPOK(ctx, s.cfg.PolymarketAPIURL, s.cfg.HTTPPlatformProxy, tid)
}

// evaluateCloseTaskAbort re-checks live state after a failed close (or before retry).
// Returns a non-empty reason when the task should stop retrying.
func (s *Service) evaluateCloseTaskAbort(ctx context.Context, pos *store.RiskPosition, closeErr error) closeTaskAbortReason {
	if pos == nil {
		return closeAbortPositionClosed
	}

	if closeErr != nil && ctx.Err() == nil {
		if syncErr := s.SyncPositionsFromDataAPI(ctx, ""); syncErr != nil && s.log != nil {
			s.log.WithFields(logx.Pairs("position_id", pos.ID, "err", syncErr.Error())).Debug("风控：平仓失败后同步持仓跳过")
		}
	}

	fresh, err := s.st.GetRiskPosition(ctx, pos.ID)
	if err != nil {
		return ""
	}
	if fresh == nil || fresh.Status == "closed" {
		return closeAbortPositionClosed
	}
	pos = fresh

	acct, err := s.st.GetActivePolymarketAccount(ctx)
	if err == nil && acct != nil {
		hid, herr := s.st.IsRiskPositionHidden(ctx, acct.ID, pos.TokenID, pos.SideLabel)
		if herr != nil && s.log != nil {
			s.log.WithFields(logx.Pairs("position_id", pos.ID, "err", herr.Error())).Warn("风控：查询监控隐藏状态失败")
		} else if hid {
			return closeAbortNotMonitored
		}
	}

	metaByTok := s.gammaMetaBatch(ctx, []string{pos.TokenID})
	tok := store.NormalizeRiskCLOBTokenID(pos.TokenID)
	meta, found := metaByTok[tok]
	if !found {
		meta, found = metaByTok[pos.TokenID]
	}
	gammaEnded := gammaMarketEndedForAbort(meta, found)
	if !gammaEnded {
		return ""
	}
	if s.closeTaskHasLiveLiquiditySignals(ctx, pos.TokenID) {
		return ""
	}
	return closeAbortMarketEnded
}

func (s *Service) abortCloseTask(ctx context.Context, taskID, positionID string, pos *store.RiskPosition, reason closeTaskAbortReason, closeErr error) error {
	msg := string(reason)
	if closeErr != nil && reason == closeAbortMarketEnded {
		errDetail := closeErr.Error()
		if len(errDetail) > 500 {
			errDetail = errDetail[:500]
		}
		msg = msg + ": " + errDetail
	}
	_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
	if err := s.st.SetRiskTaskCancelled(ctx, taskID, msg); err != nil {
		return err
	}
	if reason == closeAbortMarketEnded {
		s.setStopLossMarketEndedCooldown(ctx, positionID)
	}
	fields := logx.Pairs("task_id", taskID, "position_id", positionID, "reason", msg)
	if pos != nil {
		fields["token_id"] = pos.TokenID
	}
	s.log.WithFields(fields).Info("风控：平仓任务已终止（不再重试）")
	logx.StopLoss().WithFields(fields).Info("风控：平仓任务已终止（不再重试）")
	if s.rt != nil && pos != nil {
		s.rt.Publish("position", "info", "position.close_aborted", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
			"taskId": taskID, "abortReason": string(reason),
		})
	}
	return errCloseTaskAborted
}
