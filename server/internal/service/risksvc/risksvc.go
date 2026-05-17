package risksvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/data"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/riskruntime"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/polywarm"
	"github.com/easyspace-ai/polybet/internal/polywiring"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/tg"
	"github.com/google/uuid"
)

type Service struct {
	cfg        *config.Config
	st         *store.Store
	cache      *bookcache.Cache
	dataClient data.Client
	log        *logrus.Logger
	rt         *riskruntime.Bus
	closeMu    sync.Mutex
	closeLocks sync.Map // map[string]*sync.Mutex per-position locks for ensureCloseTask

	userWSConnected   atomic.Bool
	userWSConnecting  atomic.Bool
	userWSLastMsgMs   atomic.Int64
	restTradesLastMs  atomic.Int64
	userWSLastIssueMu sync.Mutex
	userWSLastIssue   string

	orderbookWSConnected  atomic.Bool
	orderbookWSConnecting atomic.Bool

	WSMeta *WSMetaCollector

	// Gamma /markets cache for risk UI (token id → last fetch).
	gammaMetaMu sync.Mutex
	gammaMeta   map[string]gammaMetaCache
}

func New(cfg *config.Config, st *store.Store, cache *bookcache.Cache, dataClient data.Client, log *logrus.Logger, rt *riskruntime.Bus) *Service {
	if log == nil {
		log = logrus.StandardLogger()
	}
	return &Service{cfg: cfg, st: st, cache: cache, dataClient: dataClient, log: log, rt: rt, WSMeta: NewWSMetaCollector()}
}

// SetUserWSState updates dashboard-facing User WS meta (best-effort).
func (s *Service) SetUserWSState(connecting, connected bool, lastIssue string) {
	s.userWSConnecting.Store(connecting)
	s.userWSConnected.Store(connected)
	s.userWSLastIssueMu.Lock()
	s.userWSLastIssue = lastIssue
	s.userWSLastIssueMu.Unlock()
}

// SetOrderbookWSState updates dashboard-facing Orderbook WS meta (best-effort).
func (s *Service) SetOrderbookWSState(connecting, connected bool) {
	s.orderbookWSConnecting.Store(connecting)
	s.orderbookWSConnected.Store(connected)
}

func (s *Service) OrderbookWSConnected() bool  { return s.orderbookWSConnected.Load() }
func (s *Service) OrderbookWSConnecting() bool { return s.orderbookWSConnecting.Load() }
func (s *Service) UserWSConnected() bool       { return s.userWSConnected.Load() }
func (s *Service) UserWSConnecting() bool      { return s.userWSConnecting.Load() }

// UserWSLastIssue returns the last user-channel issue string.
func (s *Service) UserWSLastIssue(out *string) {
	s.userWSLastIssueMu.Lock()
	*out = s.userWSLastIssue
	s.userWSLastIssueMu.Unlock()
}

// TouchUserWSMessage marks receipt of a user-channel message (for dashboard meta).
func (s *Service) TouchUserWSMessage() {
	s.userWSLastMsgMs.Store(time.Now().UnixMilli())
}

func (s *Service) fillMeta(meta Meta) Meta {
	meta.UserWsConnected = s.userWSConnected.Load()
	meta.UserWsConnecting = s.userWSConnecting.Load()
	meta.OrderbookWsConnected = s.orderbookWSConnected.Load()
	meta.OrderbookWsConnecting = s.orderbookWSConnecting.Load()
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

// DashboardListingMeta returns dashboard-facing meta without loading positions
// (used when position listing degrades to an empty snapshot).
func (s *Service) DashboardListingMeta(ctx context.Context, base Meta) Meta {
	meta := s.fillMeta(base)
	meta.MinOpenRiskShares = s.minShares(ctx)
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

func (s *Service) BestBidCents(ctx context.Context, tokenID string) (float64, bool) {
	bid, _, ok := s.BestBidAskCents(ctx, tokenID)
	return bid, ok && bid > 0
}

// BestBidAskCents returns best bid and ask in cents from the in-memory book cache, or REST /book.
func (s *Service) BestBidAskCents(ctx context.Context, tokenID string) (bidCents, askCents float64, ok bool) {
	tid := strings.ToLower(strings.TrimSpace(tokenID))
	bb, ba, topOk := s.cache.TopOfBook(tid)
	bidCents, askCents = bb*100, ba*100
	if topOk && (bidCents > 0 || askCents > 0) {
		return bidCents, askCents, true
	}
	b, a, err := polywarm.BestBidAskCents(ctx, s.cfg.PolymarketAPIURL, s.cfg.HTTPPlatformProxy, tid)
	if err != nil {
		return 0, 0, false
	}
	return b, a, b > 0 || a > 0
}

func maxCentsRatchet(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// stopTriggerReferenceCents is the price used to compare against trailing stop: best bid when
// present (executable), else top-of-book mark so empty-bid books still evaluate stops.
func stopTriggerReferenceCents(bidCents, askCents float64) float64 {
	if bidCents > 0 {
		return bidCents
	}
	return maxCentsRatchet(bidCents, askCents)
}

// UpdateHighWaterAndMaybeQueueStop ratchets high-water using max(bid, ask) so it tracks the
// top of the quoted range since open; stop-loss compares triggerCents (best bid, or mark if bid empty) to trail.
func (s *Service) UpdateHighWaterAndMaybeQueueStop(ctx context.Context, p store.RiskPosition, bidCents, askCents float64) (hw float64, trail float64, cur *float64, err error) {
	hw = p.HighWaterCents
	mark := maxCentsRatchet(bidCents, askCents)
	if mark > hw {
		hw = mark
		if err := s.st.UpdateRiskPositionHighWater(ctx, p.ID, hw); err != nil {
			return 0, 0, nil, err
		}
	}
	trail = hw * (1 - p.StopLossPct/100)
	curVal := bidCents
	if curVal <= 0 && askCents > 0 {
		curVal = askCents
	}
	cur = &curVal
	triggerCents := stopTriggerReferenceCents(bidCents, askCents)
	if p.Status == "open" && p.SizeShares >= s.minShares(ctx) && triggerCents > 0 && triggerCents <= trail {
		if s.log != nil && bidCents <= 0 && mark > 0 {
			s.log.WithFields(logx.Pairs("position_id", p.ID, "token_id", p.TokenID, "trigger_cents", triggerCents, "trail", trail, "mark_cents", mark)).Info("风控：移动止损触发（无买盘，按盘口高点/卖价比较）")
		}
		if err := s.ensureCloseTask(ctx, p.ID, "stop_loss"); err != nil {
			return hw, trail, cur, err
		}
	}
	return hw, trail, cur, nil
}

// EnqueueClosePosition queues a manual close (same as Node enqueueClosePosition).
func (s *Service) EnqueueClosePosition(ctx context.Context, positionID string) error {
	s.log.WithFields(logx.Pairs("position_id", positionID)).Info("风控：平仓任务已入队")
	return s.ensureCloseTask(ctx, positionID, "manual")
}

// queueReason: "manual" | "stop_loss" | "" (silent, e.g. batch from close_all).
func (s *Service) ensureCloseTask(ctx context.Context, positionID, queueReason string) error {
	lockI, _ := s.closeLocks.LoadOrStore(positionID, &sync.Mutex{})
	lock := lockI.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	has, err := s.st.FindPendingCloseTask(ctx, positionID)
	if err != nil {
		s.log.WithFields(logx.Pairs("position_id", positionID, "err", err.Error())).Warn("风控：查询待处理平仓任务失败")
		return err
	}
	if has {
		s.log.WithFields(logx.Pairs("position_id", positionID)).Info("风控：该持仓已有待处理平仓任务")
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
	if queueReason != "" {
		t.Reason = sql.NullString{String: queueReason, Valid: true}
	}
	if err := s.st.InsertRiskTask(ctx, t); err != nil {
		s.log.WithFields(logx.Pairs("position_id", positionID, "err", err.Error())).Error("风控：写入平仓任务失败")
		return err
	}
	s.log.WithFields(logx.Pairs("position_id", positionID, "task_id", t.ID)).Info("风控：平仓任务已创建")
	if queueReason != "" {
		tg.Notify(ctx, s.cfg, s.st, s.log, formatCloseQueuedTelegram(queueReason, pos, positionID))
	}
	if s.rt != nil && pos != nil {
		d := map[string]any{"taskId": t.ID, "positionId": positionID, "queueReason": queueReason}
		switch queueReason {
		case "stop_loss":
			d["stopLossPct"] = pos.StopLossPct
			d["highWaterCents"] = pos.HighWaterCents
			s.rt.Publish("position", "warn", "position.stop_loss_triggered", pos.AccountID, "", pos.TokenID, t.ID, d)
		case "manual":
			s.rt.Publish("position", "info", "position.close_queued", pos.AccountID, "", pos.TokenID, t.ID, d)
		}
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

func (s *Service) ProcessRiskTasksOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tasks, err := s.st.ListDueRiskTasks(ctx, 20)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}

	cl, err := polysession.ResolveAuthedCLOB(ctx, s.cfg, s.st)
	if err != nil {
		s.log.WithFields(logx.Pairs("task_count", len(tasks), "err", err.Error())).Warn("风控：批量任务跳过（CLOB 会话不可用）")
		return err
	}

	sellExtra := s.st.GetBotConfigInt(ctx, "polymarketFokSellExtraTicks", 5)
	concurrency := s.st.GetBotConfigInt(ctx, "closeTaskConcurrency", 10)
	if concurrency <= 0 {
		concurrency = 10
	}

	s.log.WithFields(logx.Pairs("count", len(tasks), "concurrency", concurrency)).Info("风控：开始处理批量任务")

	// 按类型分组：close_all 串行执行（它内部会创建任务），close_position 并发执行
	var closeAllTasks []store.RiskTask
	var closePosTasks []store.RiskTask
	for _, t := range tasks {
		if t.Type == "close_all" {
			closeAllTasks = append(closeAllTasks, t)
		} else {
			closePosTasks = append(closePosTasks, t)
		}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	mu := sync.Mutex{}
	completed, failed := 0, 0

	// 并发执行 close_position 任务
	runClosePosTask := func(t store.RiskTask) {
		defer wg.Done()
		if ctx.Err() != nil {
			return
		}
		sem <- struct{}{}
		defer func() { <-sem }()
		if ctx.Err() != nil {
			return
		}

		_ = s.st.SetRiskTaskRunning(ctx, t.ID)
		s.log.WithFields(logx.Pairs("task_id", t.ID, "type", t.Type, "position_id", t.PositionID.String, "attempts", t.Attempts)).Info("风控：执行任务")

		runErr := s.runClosePosition(ctx, cl, t, sellExtra)
		if runErr != nil {
			if errors.Is(runErr, errCloseTaskAborted) {
				mu.Lock()
				completed++
				mu.Unlock()
				return
			}
			att := t.Attempts + 1
			delay := closeRetryMs(att)
			msg := runErr.Error()
			if len(msg) > 2000 {
				msg = msg[:2000]
			}
			s.log.WithFields(logx.Pairs("task_id", t.ID, "type", t.Type, "next_attempt", att, "retry_delay_ms", delay, "err", msg)).Warn("风控：任务失败将重试")
			_ = s.st.SetRiskTaskFailed(ctx, t.ID, att, msg, time.Now().UTC().Add(time.Duration(delay)*time.Millisecond))
			mu.Lock()
			failed++
			mu.Unlock()
		} else {
			s.log.WithFields(logx.Pairs("task_id", t.ID, "type", t.Type)).Info("风控：任务成功")
			mu.Lock()
			completed++
			mu.Unlock()
		}
	}

	for _, t := range closePosTasks {
		wg.Add(1)
		go runClosePosTask(t)
	}

	// 串行执行 close_all 任务
	for _, t := range closeAllTasks {
		wg.Add(1)
		go func(t store.RiskTask) {
			defer wg.Done()
			if ctx.Err() != nil {
				return
			}
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}

			_ = s.st.SetRiskTaskRunning(ctx, t.ID)
			s.log.WithFields(logx.Pairs("task_id", t.ID, "type", t.Type, "attempts", t.Attempts)).Info("风控：执行 close_all 任务")

			runErr := s.runCloseAll(ctx, t.ID)
			if runErr != nil {
				att := t.Attempts + 1
				delay := defaultBackoffMs(att)
				msg := runErr.Error()
				if len(msg) > 2000 {
					msg = msg[:2000]
				}
				s.log.WithFields(logx.Pairs("task_id", t.ID, "type", t.Type, "next_attempt", att, "retry_delay_ms", delay, "err", msg)).Warn("风控：任务失败将重试")
				_ = s.st.SetRiskTaskFailed(ctx, t.ID, att, msg, time.Now().UTC().Add(time.Duration(delay)*time.Millisecond))
				mu.Lock()
				failed++
				mu.Unlock()
			} else {
				s.log.WithFields(logx.Pairs("task_id", t.ID, "type", t.Type)).Info("风控：任务成功")
				mu.Lock()
				completed++
				mu.Unlock()
			}
		}(t)
	}

	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.log.WithFields(logx.Pairs("completed", completed, "failed", failed)).Info("风控：批量任务处理结束")
	return nil
}

func (s *Service) runClosePosition(ctx context.Context, cl *polywiring.AuthedCLOB, task store.RiskTask, sellExtra int) error {
	taskID := task.ID
	positionID := task.PositionID.String
	queueReason := ""
	if task.Reason.Valid {
		queueReason = task.Reason.String
	}

	pos, err := s.st.GetRiskPosition(ctx, positionID)
	if err != nil || pos == nil || pos.Status == "closed" {
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "reason", "missing_or_closed")).Info("风控：跳过平仓（已关闭或无持仓）")
		return s.st.SetRiskTaskSucceeded(ctx, taskID)
	}
	if reason := s.evaluateCloseTaskAbort(ctx, pos, nil); reason != "" {
		return s.abortCloseTask(ctx, taskID, positionID, pos, reason, nil)
	}
	if err := s.st.SetRiskPositionStatus(ctx, positionID, "closing"); err != nil {
		s.log.WithFields(logx.Pairs("position_id", positionID, "err", err.Error())).Warn("风控：更新持仓状态为 closing 失败")
		return err
	}
	s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "size_shares", pos.SizeShares, "extra_ticks", sellExtra)).Info("风控：发送 FOK 卖单平仓")
	_, err = polyexec.ExecuteFOKSell(ctx, cl.Client, cl.Signer, pos.TokenID, pos.SizeShares, sellExtra)
	if err != nil {
		_ = s.st.SetRiskPositionStatus(ctx, positionID, "open")
		s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID, "err", err.Error())).Warn("风控：FOK 卖单失败")
		if s.rt != nil {
			s.rt.Publish("position", "warn", "position.close_failed", pos.AccountID, "", pos.TokenID, taskID, map[string]any{
				"taskId": taskID, "err": err.Error(), "reason": queueReason,
			})
		}
		if reason := s.evaluateCloseTaskAbort(ctx, pos, err); reason != "" {
			return s.abortCloseTask(ctx, taskID, positionID, pos, reason, err)
		}
		return err
	}
	if err := s.st.CloseRiskPosition(ctx, positionID); err != nil {
		s.log.WithFields(logx.Pairs("position_id", positionID, "err", err.Error())).Error("风控：数据库关闭持仓失败")
		return err
	}
	_ = s.st.CancelOtherCloseTasks(ctx, positionID, taskID)
	s.log.WithFields(logx.Pairs("task_id", taskID, "position_id", positionID, "token_id", pos.TokenID)).Info("风控：平仓成交")
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

func (s *Service) runCloseAll(ctx context.Context, taskID string) error {
	acct, err := s.st.GetActivePolymarketAccount(ctx)
	if err != nil || acct == nil {
		s.log.WithFields(logx.Pairs("task_id", taskID, "err", err)).Warn("风控：一键平仓跳过（无活跃账户）")
		return s.st.SetRiskTaskSucceeded(ctx, taskID)
	}
	min := s.minShares(ctx)
	rows, err := s.st.ListOpenRiskPositionsMinShares(ctx, min, acct.ID)
	if err != nil {
		return err
	}
	s.log.WithFields(logx.Pairs("task_id", taskID, "open_positions", len(rows), "min_shares", min)).Info("风控：一键平仓扫描持仓")
	if len(rows) == 0 {
		return s.st.SetRiskTaskSucceeded(ctx, taskID)
	}
	if s.rt != nil {
		s.rt.Publish("position", "info", "position.close_all_started", acct.ID, "", "", taskID, map[string]any{
			"taskId": taskID, "openPositions": len(rows),
		})
	}

	// 并发创建平仓任务
	concurrency := s.st.GetBotConfigInt(ctx, "closeAllConcurrency", 20)
	if concurrency <= 0 {
		concurrency = 20
	}
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed int

	for _, p := range rows {
		wg.Add(1)
		go func(positionID string) {
			defer wg.Done()
			sem <- struct{}{}        // 获取信号量
			defer func() { <-sem }() // 释放

			if err := s.ensureCloseTask(ctx, positionID, ""); err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				s.log.WithFields(logx.Pairs("position_id", positionID, "err", err.Error())).Warn("风控：一键平仓子任务入队失败")
			}
		}(p.ID)
	}
	wg.Wait()

	if s.rt != nil {
		s.rt.Publish("position", "info", "position.close_all_done", acct.ID, "", "", taskID, map[string]any{
			"taskId": taskID, "enqueued": len(rows) - failed, "failed": failed, "candidates": len(rows),
		})
	}

	if err := s.st.SetRiskTaskSucceeded(ctx, taskID); err != nil {
		return err
	}
	s.log.WithFields(logx.Pairs("task_id", taskID, "enqueued_closes", len(rows)-failed, "failed", failed)).Info("风控：一键平仓完成")
	tg.Notify(ctx, s.cfg, s.st, s.log, fmt.Sprintf("Polybet 一键平仓\n已为 %d 个持仓创建平仓任务", len(rows)-failed))
	return nil
}

func (s *Service) RiskEvaluateTokenAfterBookUpdate(ctx context.Context, tokenID string) error {
	acct, err := s.st.GetActivePolymarketAccount(ctx)
	if err != nil || acct == nil {
		return nil
	}
	min := s.minShares(ctx)
	rows, err := s.st.ListOpenRiskPositionsByToken(ctx, tokenID, acct.ID)
	if err != nil {
		return err
	}
	for _, p := range rows {
		if p.SizeShares < min {
			continue
		}
		hid, herr := s.st.IsRiskPositionHidden(ctx, acct.ID, p.TokenID, p.SideLabel)
		if herr != nil {
			s.log.WithFields(logx.Pairs("err", herr.Error())).Warn("风控：评估止损时查询隐藏持仓失败")
		} else if hid {
			continue
		}
		bid, ask, ok := s.BestBidAskCents(ctx, tokenID)
		if !ok {
			continue
		}
		_, _, _, err := s.UpdateHighWaterAndMaybeQueueStop(ctx, p, bid, ask)
		if err != nil {
			s.log.WithFields(logx.Pairs("err", err)).Warn("风控：评估止损失败")
		}
	}
	return nil
}

// ApplyClobTradeIfNew deduplicates CLOB trade events. It no longer aggregates
// positions locally; callers should trigger SyncPositionsFromDataAPI after it
// returns applied=true.
func (s *Service) ApplyClobTradeIfNew(ctx context.Context, trade struct {
	ID, AssetID, Side, Size, Price, Status string
	Market, Outcome                        string
}, accountID string) (bool, error) {
	st := strings.ToUpper(strings.TrimSpace(trade.Status))
	if st != "MATCHED" && st != "MINED" && st != "CONFIRMED" {
		return false, nil
	}
	_ = s.st.NormalizeDustRisk(ctx, 1e-9)
	ok, err := s.st.InsertRiskAppliedTrade(ctx, trade.ID, accountID)
	if err != nil || !ok {
		return false, err
	}
	return true, nil
}
