package risksvc

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/polywarm"
	"github.com/easyspace-ai/polybet/internal/polywiring"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/tg"
	"github.com/google/uuid"
)

type Service struct {
	cfg     *config.Config
	st      *store.Store
	cache   *bookcache.Cache
	log     *slog.Logger
	closeMu sync.Mutex // serializes ensureCloseTask inserts globally (acceptable for MVP)

	userWSConnected   atomic.Bool
	userWSConnecting  atomic.Bool
	userWSLastMsgMs   atomic.Int64
	restTradesLastMs  atomic.Int64
	userWSLastIssueMu sync.Mutex
	userWSLastIssue   string
}

func New(cfg *config.Config, st *store.Store, cache *bookcache.Cache, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{cfg: cfg, st: st, cache: cache, log: log}
}

// SetUserWSState updates dashboard-facing User WS meta (best-effort).
func (s *Service) SetUserWSState(connecting, connected bool, lastIssue string) {
	s.userWSConnecting.Store(connecting)
	s.userWSConnected.Store(connected)
	s.userWSLastIssueMu.Lock()
	s.userWSLastIssue = lastIssue
	s.userWSLastIssueMu.Unlock()
}

// TouchUserWSMessage marks receipt of a user-channel message (for dashboard meta).
func (s *Service) TouchUserWSMessage() {
	s.userWSLastMsgMs.Store(time.Now().UnixMilli())
}

func (s *Service) touchRESTTradesSync() {
	s.restTradesLastMs.Store(time.Now().UnixMilli())
}

func (s *Service) fillMeta(meta Meta) Meta {
	meta.UserWsConnected = s.userWSConnected.Load()
	meta.UserWsConnecting = s.userWSConnecting.Load()
	if ms := s.userWSLastMsgMs.Load(); ms > 0 {
		t := time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
		meta.UserWsLastMessageAt = &t
	}
	if ms := s.restTradesLastMs.Load(); ms > 0 {
		t := time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
		meta.RestTradesSyncLastAt = &t
	}
	s.userWSLastIssueMu.Lock()
	issue := s.userWSLastIssue
	s.userWSLastIssueMu.Unlock()
	if issue != "" {
		meta.UserWsLastIssue = &issue
	}
	return meta
}

func (s *Service) minShares(ctx context.Context) float64 {
	return s.st.GetBotConfigFloat(ctx, "minOpenRiskShares", 1)
}

func closeRetryMs(attempts int) int {
	n := max(1, attempts)
	if n <= 6 {
		v := 400 * int(math.Pow(2, float64(n-1)))
		if v > 10000 {
			return 10000
		}
		return v
	}
	v := 2000 * int(math.Pow(2, float64(minInt(n-6, 5))))
	if v > 60000 {
		return 60000
	}
	return v
}

func defaultBackoffMs(attempts int) int {
	v := 2000 * int(math.Pow(2, float64(minInt(attempts, 5))))
	if v > 60000 {
		return 60000
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Service) bestBidCents(ctx context.Context, tokenID string) (float64, bool) {
	bb, _, ok := s.cache.TopOfBook(tokenID)
	if ok && bb > 0 {
		return bb * 100, true
	}
	cents, err := polywarm.BestBidCents(ctx, s.cfg.PolymarketAPIURL, s.cfg.HTTPPlatformProxy, tokenID)
	if err != nil {
		return 0, false
	}
	return cents, true
}

func (s *Service) UpdateHighWaterAndMaybeQueueStop(ctx context.Context, p store.RiskPosition, bidCents float64) (hw float64, trail float64, cur *float64, err error) {
	hw = p.HighWaterCents
	if bidCents > hw {
		hw = bidCents
		if err := s.st.UpdateRiskPositionHighWater(ctx, p.ID, hw); err != nil {
			return 0, 0, nil, err
		}
	}
	trail = hw * (1 - p.StopLossPct/100)
	curVal := bidCents
	cur = &curVal
	if p.Status == "open" && p.SizeShares >= s.minShares(ctx) && bidCents <= trail {
		if err := s.ensureCloseTask(ctx, p.ID, "stop_loss"); err != nil {
			return hw, trail, cur, err
		}
	}
	return hw, trail, cur, nil
}

// EnqueueClosePosition queues a manual close (same as Node enqueueClosePosition).
func (s *Service) EnqueueClosePosition(ctx context.Context, positionID string) error {
	s.log.Info("risk_close_enqueue", "position_id", positionID)
	return s.ensureCloseTask(ctx, positionID, "manual")
}

// queueReason: "manual" | "stop_loss" | "" (silent, e.g. batch from close_all).
func (s *Service) ensureCloseTask(ctx context.Context, positionID, queueReason string) error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	has, err := s.st.FindPendingCloseTask(ctx, positionID)
	if err != nil {
		s.log.Warn("risk_close_task_lookup_failed", "position_id", positionID, "err", err.Error())
		return err
	}
	if has {
		s.log.Info("risk_close_task_already_pending", "position_id", positionID)
		return nil
	}
	pos, _ := s.st.GetRiskPosition(ctx, positionID)
	t := &store.RiskTask{
		ID:         uuid.NewString(),
		Type:       "close_position",
		PositionID: sql.NullString{String: positionID, Valid: true},
		Status:     "pending",
		Attempts:   0,
		NextRunAt:  time.Now().UTC(),
	}
	if err := s.st.InsertRiskTask(ctx, t); err != nil {
		s.log.Error("risk_close_task_insert_failed", "position_id", positionID, "err", err.Error())
		return err
	}
	s.log.Info("risk_close_task_inserted", "position_id", positionID, "task_id", t.ID)
	if queueReason != "" {
		tg.Notify(ctx, s.cfg, s.st, s.log, formatCloseQueuedTelegram(queueReason, pos, positionID))
	}
	return nil
}

func formatCloseQueuedTelegram(reason string, pos *store.RiskPosition, positionID string) string {
	title := positionID
	if len(title) > 12 {
		title = title[:8] + "…"
	}
	shares := 0.0
	if pos != nil {
		if strings.TrimSpace(pos.Title) != "" {
			title = pos.Title
		}
		shares = pos.SizeShares
	}
	switch reason {
	case "manual":
		return fmt.Sprintf("Polybet 手动平仓已排队\n%s\n份额 %.2f", title, shares)
	case "stop_loss":
		return fmt.Sprintf("Polybet 移动止损触发，已排队平仓\n%s\n份额 %.2f", title, shares)
	default:
		return ""
	}
}

func (s *Service) RecordPolymarketBuyFill(ctx context.Context, outcomeID, tokenID, title, sideLabel string, fillOdds, costUsd float64) error {
	entry := fillOdds * 100
	if costUsd <= 0 || fillOdds <= 0 {
		return nil
	}
	newShares := costUsd / fillOdds
	if newShares <= 0 {
		return nil
	}
	_ = s.st.NormalizeDustRisk(ctx, 1e-9)
	stop := resolveStopLossPct(ctx, s.st, entry)
	return s.st.MergeOpenRiskBuy(ctx, tokenID, outcomeID, title, sideLabel, entry, newShares, costUsd, stop, "bot")
}

func (s *Service) ProcessRiskTasksOnce(ctx context.Context) error {
	tasks, err := s.st.ListDueRiskTasks(ctx, 20)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	cl, err := polysession.ResolveAuthedCLOB(ctx, s.cfg, s.st)
	if err != nil {
		s.log.Warn("risk_task_batch_skip_no_clob", "task_count", len(tasks), "err", err.Error())
		return err
	}
	s.log.Info("risk_task_batch_start", "count", len(tasks))
	sellExtra := s.st.GetBotConfigInt(ctx, "polymarketFokSellExtraTicks", 5)
	for i := range tasks {
		t := tasks[i]
		_ = s.st.SetRiskTaskRunning(ctx, t.ID)
		var runErr error
		logOk := true
		switch t.Type {
		case "close_position":
			if t.PositionID.Valid {
				s.log.Info("risk_task_run", "task_id", t.ID, "type", t.Type, "position_id", t.PositionID.String, "attempts", t.Attempts)
				runErr = s.runClosePosition(ctx, cl, t.ID, t.PositionID.String, sellExtra)
			}
		case "close_all":
			s.log.Info("risk_task_run", "task_id", t.ID, "type", t.Type, "attempts", t.Attempts)
			runErr = s.runCloseAll(ctx, t.ID)
		default:
			logOk = false
			s.log.Warn("risk_task_unknown_type", "task_id", t.ID, "type", t.Type)
			_ = s.st.SetRiskTaskFailed(ctx, t.ID, t.Attempts+1, "unknown_task_type:"+t.Type, time.Now().UTC().Add(24*time.Hour))
			runErr = nil
		}
		if runErr != nil {
			att := t.Attempts + 1
			delay := closeRetryMs(att)
			if t.Type != "close_position" {
				delay = defaultBackoffMs(att)
			}
			msg := runErr.Error()
			if len(msg) > 2000 {
				msg = msg[:2000]
			}
			s.log.Warn("risk_task_failed", "task_id", t.ID, "type", t.Type, "next_attempt", att, "retry_delay_ms", delay, "err", msg)
			_ = s.st.SetRiskTaskFailed(ctx, t.ID, att, msg, time.Now().UTC().Add(time.Duration(delay)*time.Millisecond))
		} else if logOk {
			s.log.Info("risk_task_ok", "task_id", t.ID, "type", t.Type)
		}
	}
	return nil
}

func (s *Service) runClosePosition(ctx context.Context, cl *polywiring.AuthedCLOB, taskID, positionID string, sellExtra int) error {
	pos, err := s.st.GetRiskPosition(ctx, positionID)
	if err != nil || pos == nil || pos.Status == "closed" {
		s.log.Info("risk_close_skip_done", "task_id", taskID, "position_id", positionID, "reason", "missing_or_closed")
		return s.st.SetRiskTaskSucceeded(ctx, taskID)
	}
	if err := s.st.SetRiskPositionStatus(ctx, positionID, "closing"); err != nil {
		s.log.Warn("risk_close_status_closing_failed", "position_id", positionID, "err", err.Error())
		return err
	}
	s.log.Info("risk_close_fok_sell_send", "task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "size_shares", pos.SizeShares, "extra_ticks", sellExtra)
	_, err = polyexec.ExecuteFOKSell(ctx, cl.Client, cl.Signer, pos.TokenID, pos.SizeShares, sellExtra)
	if err != nil {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		s.log.Warn("risk_close_fok_sell_failed", "task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "err", err.Error())
		return err
	}
	if err := s.st.CloseRiskPosition(ctx, positionID); err != nil {
		s.log.Error("risk_close_db_close_failed", "position_id", positionID, "err", err.Error())
		return err
	}
	_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
	s.log.Info("risk_close_filled", "task_id", taskID, "position_id", positionID, "token_id", pos.TokenID)
	tg.Notify(ctx, s.cfg, s.st, s.log, fmt.Sprintf(
		"Polybet 平仓成交\n%s\n份额 %.2f · token %s",
		strings.TrimSpace(pos.Title),
		pos.SizeShares,
		pos.TokenID,
	))
	tg.MaybeNotifyCollateralChanged(s.cfg, s.log, s.st)
	return s.st.SetRiskTaskSucceeded(ctx, taskID)
}

func (s *Service) runCloseAll(ctx context.Context, taskID string) error {
	min := s.minShares(ctx)
	rows, err := s.st.ListOpenRiskPositionsMinShares(ctx, min)
	if err != nil {
		return err
	}
	s.log.Info("risk_close_all_run", "task_id", taskID, "open_positions", len(rows), "min_shares", min)
	for _, p := range rows {
		_ = s.ensureCloseTask(ctx, p.ID, "")
	}
	if err := s.st.SetRiskTaskSucceeded(ctx, taskID); err != nil {
		return err
	}
	s.log.Info("risk_close_all_done", "task_id", taskID, "enqueued_closes", len(rows))
	if len(rows) > 0 {
		tg.Notify(ctx, s.cfg, s.st, s.log, fmt.Sprintf("Polybet 一键平仓\n已为 %d 个持仓创建平仓任务", len(rows)))
	}
	return nil
}

func (s *Service) RiskEvaluateTokenAfterBookUpdate(ctx context.Context, tokenID string) error {
	min := s.minShares(ctx)
	rows, err := s.st.ListOpenRiskPositionsByToken(ctx, tokenID)
	if err != nil {
		return err
	}
	for _, p := range rows {
		if p.SizeShares < min {
			continue
		}
		bid, ok := s.bestBidCents(ctx, tokenID)
		if !ok {
			continue
		}
		_, _, _, err := s.UpdateHighWaterAndMaybeQueueStop(ctx, p, bid)
		if err != nil {
			s.log.Warn("risk evaluate", "err", err)
		}
	}
	return nil
}

func (s *Service) ApplyClobTradeIfNew(ctx context.Context, trade struct {
	ID, AssetID, Side, Size, Price, Status string
	Market, Outcome                        string
}) (bool, error) {
	st := strings.ToUpper(strings.TrimSpace(trade.Status))
	if st != "MATCHED" && st != "MINED" && st != "CONFIRMED" {
		return false, nil
	}
	_ = s.st.NormalizeDustRisk(ctx, 1e-9)
	ok, err := s.st.InsertRiskAppliedTrade(ctx, trade.ID)
	if err != nil || !ok {
		return false, err
	}
	size, _ := strconv.ParseFloat(strings.TrimSpace(trade.Size), 64)
	price, _ := strconv.ParseFloat(strings.TrimSpace(trade.Price), 64)
	if trade.AssetID == "" || size <= 0 || price <= 0 {
		return true, nil
	}
	side := strings.ToUpper(trade.Side)
	min := s.minShares(ctx)
	if side == "BUY" {
		entry := price * 100
		cost := size * price
		stop := resolveStopLossPct(ctx, s.st, entry)
		oid, has, _ := s.st.FindOutcomeIDByToken(ctx, trade.AssetID)
		outcomeID := ""
		if has {
			outcomeID = oid
		}
		title := "Polymarket"
		if trade.Market != "" {
			title = "CLOB " + trade.Market[:minInt(len(trade.Market), 10)]
		}
		sideLabel := trade.Outcome
		if sideLabel == "" {
			sideLabel = "YES"
		}
		if err := s.st.MergeOpenRiskBuy(ctx, trade.AssetID, outcomeID, title, sideLabel, entry, size, cost, stop, "polymarket_clob"); err != nil {
			return true, err
		}
		tg.Notify(ctx, s.cfg, s.st, s.log, fmt.Sprintf(
			"Polybet 成交同步（买入）\n%s\n%.2f 股 @ %.1f¢ · 成本约 $%.2f · trade %s",
			title, size, entry, cost, trade.ID,
		))
		tg.MaybeNotifyCollateralChanged(s.cfg, s.log, s.st)
		return true, nil
	}
	if side == "SELL" {
		if err := s.st.ReduceOpenRiskSell(ctx, trade.AssetID, size, price, min); err != nil {
			return true, err
		}
		tg.Notify(ctx, s.cfg, s.st, s.log, fmt.Sprintf(
			"Polybet 成交同步（卖出）\nasset %s\n%.2f 股 @ %.4f · trade %s",
			trade.AssetID, size, price, trade.ID,
		))
		tg.MaybeNotifyCollateralChanged(s.cfg, s.log, s.st)
		return true, nil
	}
	return true, nil
}
