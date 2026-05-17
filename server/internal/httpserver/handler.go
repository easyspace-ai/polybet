package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/debounce"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/memcache"
	"github.com/easyspace-ai/polybet/internal/polyprov"
	"github.com/easyspace-ai/polybet/internal/riskruntime"
	"github.com/easyspace-ai/polybet/internal/service/initsvc"
	"github.com/easyspace-ai/polybet/internal/service/logsvc"
	"github.com/easyspace-ai/polybet/internal/service/marketsvc"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/service/routersvc"
	"github.com/easyspace-ai/polybet/internal/service/tradesvc"
	"github.com/easyspace-ai/polybet/internal/store"
	mktSync "github.com/easyspace-ai/polybet/internal/sync"
	"github.com/easyspace-ai/polybet/internal/tg"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

type Handler struct {
	cfg          *config.Config
	db           *sql.DB
	st           *store.Store
	cache        *bookcache.Cache
	hub          *wsrelay.Hub
	riskHub      *wsrelay.Hub
	risk         *risksvc.Service
	debounce     *debounce.Debouncer
	balanceCache *memcache.BalanceCache
	riskCache    *memcache.RiskCache
	initService  *initsvc.Service
	logService   *logsvc.Service
	sportsCache  *mktSync.SportsCache
	riskRuntime  *riskruntime.Bus
	app          interface {
		InvalidateAndRebuildCache()
		SyncAndBroadcastMarkets(ctx context.Context, force bool) error
		RequestRestart()
		ForceWSReconnect(channel string)
		EnsureOrderbookToken(tokenID string)
		OpenRiskPositionCount(ctx context.Context) int
	}
}

func riskMetaForAPI(m risksvc.Meta) memcache.RiskMeta {
	return memcache.RiskMeta{
		UserWsConnected:         m.UserWsConnected,
		UserWsConnecting:        m.UserWsConnecting,
		OutboundProxyConfigured: m.OutboundProxyConfigured,
		MinOpenRiskShares:       m.MinOpenRiskShares,
		RiskCloseExecutionMode:  m.RiskCloseExecutionMode,
		RiskCloseFakWorstPrice:  m.RiskCloseFakWorstPrice,
		RiskHedgeBuySizing:      m.RiskHedgeBuySizing,
	}
}

// riskPositionsFetchResult loads enriched positions. DB/list failures degrade to an
// empty list with live meta so GET /api/risk/positions stays 200 for non-auth cases.
func riskPositionsFetchResult(ctx context.Context, requestID string, risk *risksvc.Service, accountID string, meta risksvc.Meta) (memcache.RiskFetchResult, error) {
	if risk == nil {
		return memcache.RiskFetchResult{
			Positions: []map[string]any{},
			Meta: memcache.RiskMeta{
				OutboundProxyConfigured: meta.OutboundProxyConfigured,
			},
		}, nil
	}
	ctxBase := context.WithoutCancel(ctx)
	rows, m, err := risk.ListRiskPositionsEnriched(ctxBase, meta, accountID)
	if err != nil {
		fields := logx.Pairs("request_id", requestID, "err", err.Error())
		logrus.WithFields(fields).Warn("风控持仓：列举失败，返回空列表")
		logx.Position().WithFields(fields).Warn("风控持仓：列举失败，返回空列表")
		snap := risk.DashboardListingMeta(ctxBase, meta)
		return memcache.RiskFetchResult{Positions: []map[string]any{}, Meta: riskMetaForAPI(snap)}, nil
	}
	return memcache.RiskFetchResult{Positions: rows, Meta: riskMetaForAPI(m)}, nil
}

func validateBotConfigUpdate(key, value string) error {
	k := strings.TrimSpace(key)
	v := strings.TrimSpace(value)
	switch k {
	case "riskCloseExecutionMode":
		switch strings.ToLower(v) {
		case "fok_sell", "fak_sell", "hedge_fok_buy":
			return nil
		default:
			return fmt.Errorf("riskCloseExecutionMode must be fok_sell, fak_sell, or hedge_fok_buy")
		}
	case "riskHedgeBuySizing":
		switch strings.ToLower(v) {
		case "notional", "shares":
			return nil
		default:
			return fmt.Errorf("riskHedgeBuySizing must be notional or shares")
		}
	case "riskHedgeAutoHidePosition":
		if _, err := strconv.ParseBool(v); err != nil {
			return fmt.Errorf("riskHedgeAutoHidePosition must be a boolean")
		}
		return nil
	case "riskCloseFakWorstPrice":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("riskCloseFakWorstPrice must be a number")
		}
		if f <= 0 || f >= 1 {
			return fmt.Errorf("riskCloseFakWorstPrice must be between 0 and 1 exclusive")
		}
		return nil
	default:
		return nil
	}
}

func NewHandler(d Deps) *Handler {
	return &Handler{
		cfg:          d.Cfg,
		db:           d.DB,
		st:           d.Store,
		cache:        d.Cache,
		hub:          d.Hub,
		riskHub:      d.RiskHub,
		risk:         d.Risk,
		debounce:     d.Debounce,
		balanceCache: d.BalanceCache,
		riskCache:    d.RiskCache,
		initService:  d.InitService,
		logService:   d.LogService,
		sportsCache:  d.SportsCache,
		riskRuntime:  d.RiskRuntime,
		app:          d.App,
	}
}

type simpleCache struct {
	mu   sync.RWMutex
	data map[string]cacheItem
	ttl  time.Duration
}

type cacheItem struct {
	value     any
	expiresAt int64 // unix nano
}

func newSimpleCache(ttl time.Duration) *simpleCache {
	return &simpleCache{
		data: make(map[string]cacheItem),
		ttl:  ttl,
	}
}

func (c *simpleCache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.data[key]
	if !ok {
		return nil, false
	}
	if time.Now().UnixNano() > item.expiresAt {
		return nil, false
	}
	return item.value, true
}

func (c *simpleCache) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = cacheItem{
		value:     value,
		expiresAt: time.Now().Add(c.ttl).UnixNano(),
	}
}

func (c *simpleCache) deleteExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, item := range c.data {
		if now.UnixNano() > item.expiresAt {
			delete(c.data, k)
		}
	}
}

func (c *simpleCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

var accountsCache = newSimpleCache(10 * time.Second)
var tasksCache = newSimpleCache(10 * time.Second)

func tradeResultsLog(rs []tradesvc.TradeResult) string {
	if len(rs) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		s := r.Platform + ":" + r.Status + ":" + r.TradeID
		if r.FailureReason != "" {
			s += ":" + r.FailureReason
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ";")
}

func (h *Handler) handleHealth(c *gin.Context) {
	if err := h.db.PingContext(c); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "db": "unreachable", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "db": "connected"})
}

func (h *Handler) handleRestart(c *gin.Context) {
	logrus.WithFields(logx.Pairs("request_id", c.GetString("request_id"))).Info("收到 API 重启请求")
	if h.app != nil {
		h.app.RequestRestart()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "restarting"})
}

func (h *Handler) handleBalances(c *gin.Context) {
	sum, fromCache, err := h.balanceCache.GetWithRefresh(c)
	if err != nil {
		c.JSON(500, gin.H{"error": "balances_failed", "message": err.Error()})
		return
	}
	accts := make([]gin.H, 0, len(sum.PolymarketAccounts))
	for _, x := range sum.PolymarketAccounts {
		accts = append(accts, gin.H{
			"id": x.ID, "name": x.Name, "isActive": x.IsActive, "polymarket": x.Polymarket,
		})
	}
	c.JSON(200, gin.H{"polymarket": sum.Polymarket, "polymarketAccounts": accts, "cached": fromCache})
}

func (h *Handler) handleConfig(c *gin.Context) {
	rows, err := h.st.ListBotConfig(c)
	if err != nil {
		c.JSON(500, gin.H{"error": "config"})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{"key": r.Key, "value": r.Value})
	}
	c.JSON(200, out)
}

func (h *Handler) handleUpdateConfig(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	_ = c.BindJSON(&body)
	key := c.Param("key")
	if err := validateBotConfigUpdate(key, body.Value); err != nil {
		c.JSON(400, gin.H{"error": "invalid_config", "message": err.Error()})
		return
	}
	if err := h.st.UpsertBotConfig(c, key, body.Value); err != nil {
		c.JSON(500, gin.H{"error": "update_failed"})
		return
	}
	c.JSON(200, gin.H{"key": key, "value": body.Value})
}

func (h *Handler) handleTelegramTest(c *gin.Context) {
	token, chat := tg.ResolveTelegramCreds(c, h.cfg, h.st)
	rid := c.GetString("request_id")
	logrus.WithFields(logx.Pairs("request_id", rid, "token_set", token != "", "chat_set", chat != "", "proxy", h.cfg.HTTPPlatformProxy)).Info("Telegram 连通性测试请求")
	if token == "" || chat == "" {
		c.JSON(400, gin.H{"error": "telegram_not_configured", "message": "请先配置 Bot Token 和 Chat ID"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	form := url.Values{}
	form.Set("chat_id", chat)
	form.Set("text", "hello, 我是你的polymarket. 助手")
	u := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", url.PathEscape(token))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		logrus.WithFields(logx.Pairs("request_id", rid, "err", err.Error())).Warn("Telegram 测试：构造请求失败")
		c.JSON(500, gin.H{"error": "request_failed", "message": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var hc *http.Client
	if h.cfg.HTTPPlatformProxy != "" {
		if proxyURL, err := url.Parse(h.cfg.HTTPPlatformProxy); err == nil {
			logrus.WithFields(logx.Pairs("request_id", rid, "proxy", h.cfg.HTTPPlatformProxy)).Info("Telegram 测试：使用 HTTP 代理")
			hc = &http.Client{
				Timeout:   14 * time.Second,
				Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
			}
		}
	}
	if hc == nil {
		logrus.WithFields(logx.Pairs("request_id", rid)).Info("Telegram 测试：直连（无代理）")
		hc = &http.Client{Timeout: 14 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		logrus.WithFields(logx.Pairs("request_id", rid, "err", err.Error())).Warn("Telegram 测试：发送失败")
		c.JSON(502, gin.H{"error": "send_failed", "message": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	logrus.WithFields(logx.Pairs("request_id", rid, "status", resp.StatusCode, "body", string(body))).Info("Telegram 测试：HTTP 响应")
	if resp.StatusCode != 200 {
		c.JSON(502, gin.H{"error": "telegram_api_error", "message": fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "测试消息已发送"})
}

func (h *Handler) handleStatus(c *gin.Context) {
	if h.initService == nil {
		c.JSON(500, gin.H{"error": "service_not_available"})
		return
	}
	initStatus := h.initService.GetStatus()
	hubSize := 0
	if h.hub != nil {
		hubSize = h.hub.ClientCount()
	}
	c.JSON(200, gin.H{
		"initStatus": initStatus,
		"wsClients":  hubSize,
		"serverTime": time.Now().Format("2006-01-02 15:04:05"),
	})
}

func (h *Handler) handleWSStatus(c *gin.Context) {
	hubSize := 0
	if h.hub != nil {
		hubSize = h.hub.ClientCount()
	}
	openN := 0
	if h.app != nil {
		openN = h.app.OpenRiskPositionCount(c.Request.Context())
	}
	c.JSON(200, buildWSStatusJSON(h.risk, h.cache, hubSize, openN))
}

func (h *Handler) handleWSReconnect(c *gin.Context) {
	var body struct {
		Channel string `json:"channel"`
	}
	_ = c.ShouldBindJSON(&body)
	if h.app != nil {
		h.app.ForceWSReconnect(body.Channel)
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) handleSetupStatus(c *gin.Context) {
	v, ok, _ := h.st.GetBotConfig(c, "onboardingComplete")
	onboardingDone := ok && strings.TrimSpace(v) == "true"
	_, polyErr := polysession.ResolveAuthedCLOB(c, h.cfg, h.st)
	needs := !onboardingDone && polyErr != nil
	c.JSON(200, gin.H{
		"needsOnboarding":      needs,
		"proxyConfigured":      h.cfg.HTTPPlatformProxy != "",
		"polymarketConfigured": polyErr == nil,
	})
}

func (h *Handler) handleInitStatus(c *gin.Context) {
	if h.initService == nil {
		c.JSON(500, gin.H{"error": "init_service_not_available"})
		return
	}
	status := h.initService.GetStatus()
	c.JSON(200, status)
}

func (h *Handler) handleComplete(c *gin.Context) {
	_ = h.st.UpsertBotConfig(c, "onboardingComplete", "true")
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) handleSetupComplete(c *gin.Context) {
	_ = h.st.UpsertBotConfig(c, "onboardingComplete", "true")
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) handleLogs(c *gin.Context) {
	if h.logService == nil {
		c.JSON(500, gin.H{"error": "log_service_not_available"})
		return
	}
	logs := h.logService.GetAll()
	c.JSON(200, gin.H{"logs": logs})
}

func (h *Handler) handleLogErrors(c *gin.Context) {
	if h.logService == nil {
		c.JSON(500, gin.H{"error": "log_service_not_available"})
		return
	}
	logs := h.logService.GetErrors()
	c.JSON(200, gin.H{"logs": logs})
}

func (h *Handler) handleLogClear(c *gin.Context) {
	if h.logService == nil {
		c.JSON(500, gin.H{"error": "log_service_not_available"})
		return
	}
	h.logService.Clear()
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) handleCacheRefresh(c *gin.Context) {
	if h.app != nil {
		h.app.InvalidateAndRebuildCache()
	}
	c.JSON(200, gin.H{"ok": true, "message": "cache_refreshed"})
}

func (h *Handler) handleMarkets(c *gin.Context) {
	var sportIcons map[string]string
	if sports, err := h.sportsCache.Get(c); err == nil {
		sportIcons = marketsvc.BuildSportIconMap(sports)
	}
	data, err := marketsvc.BuildMarketsPayload(c, h.st, h.cache, sportIcons)
	if err != nil {
		c.JSON(500, gin.H{"error": "markets_failed"})
		return
	}
	c.JSON(200, data)
}

func (h *Handler) handleSports(c *gin.Context) {
	sports, err := h.sportsCache.Get(c)
	if err != nil {
		c.JSON(500, gin.H{"error": "sports_fetch_failed"})
		return
	}
	c.JSON(200, sports)
}

func (h *Handler) handleMarketsRefresh(c *gin.Context) {
	rid := c.GetString("request_id")
	if h.cfg.ReadOnlyMode {
		logrus.WithFields(logx.Pairs("request_id", rid)).Warn("市场刷新：只读模式已阻止")
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	logrus.WithFields(logx.Pairs("request_id", rid)).Info("市场刷新：收到请求")
	if h.logService != nil {
		h.logService.Info("市场同步", "用户触发强制刷新")
	}
	// Default: bypass throttle (empty ?force=). Opt out with ?force=0|false|no.
	fq := strings.TrimSpace(strings.ToLower(c.Query("force")))
	force := fq == "" || fq == "1" || fq == "true" || fq == "yes"
	if fq == "0" || fq == "false" || fq == "no" {
		force = false
	}
	if err := h.app.SyncAndBroadcastMarkets(c, force); err != nil {
		c.JSON(500, gin.H{"error": "sync_failed", "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "message": "markets_refreshed"})
}

func (h *Handler) handleOrderbook(c *gin.Context) {
	oid := c.Query("outcomeId")
	if oid == "" {
		c.JSON(400, gin.H{"error": "outcomeId required"})
		return
	}
	rows, err := h.st.ListRouterPolySiblings(c, oid)
	if err != nil || len(rows) == 0 {
		c.JSON(200, gin.H{"levels": []any{}, "polyTokenId": nil})
		return
	}

	var polyTok string
	for _, o := range rows {
		if o.ExternalID.Valid {
			polyTok = o.ExternalID.String
			break
		}
	}
	if polyTok == "" {
		c.JSON(200, gin.H{"levels": []any{}, "polyTokenId": nil})
		return
	}

	// Try cache first
	levels := h.cache.GetLevels(polyTok)
	if len(levels) == 0 {
		// Cache miss -> fetch from Polymarket REST once
		h.fetchAndCachePolyBook(c.Request.Context(), polyTok)
		levels = h.cache.GetLevels(polyTok)
	}

	type lvl struct {
		Odds     float64 `json:"odds"`
		Size     float64 `json:"size"`
		Platform string  `json:"platform"`
	}
	out := make([]lvl, 0, len(levels))
	for _, L := range levels {
		out = append(out, lvl{
			Odds:     L.Odds,
			Size:     L.Size,
			Platform: "polymarket",
		})
	}
	c.JSON(200, gin.H{"levels": out, "polyTokenId": polyTok})
}

func (h *Handler) fetchAndCachePolyBook(ctx context.Context, tokenID string) {
	clobAPI := "https://clob.polymarket.com/book?token_id=" + tokenID
	client := &http.Client{Timeout: 5 * time.Second}
	if strings.TrimSpace(h.cfg.HTTPPlatformProxy) != "" {
		pu, err := url.Parse(h.cfg.HTTPPlatformProxy)
		if err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(pu)}
		}
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, clobAPI, nil)
	resp, err := client.Do(req)
	if err != nil {
		fields := logx.Pairs("token_id", tokenID, "err", err)
		logrus.WithFields(fields).Warn("订单簿：从 Polymarket 拉取失败")
		logx.Trade().WithFields(fields).Warn("订单簿：从 Polymarket 拉取失败")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var data struct {
		Bids []struct{ Price, Size string } `json:"bids"`
		Asks []struct{ Price, Size string } `json:"asks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return
	}

	h.cache.ReplaceBook(tokenID, data.Bids, data.Asks, time.Now().UnixMilli())
}

func (h *Handler) handleTradePreview(c *gin.Context) {
	oid, side, sizeS := c.Query("outcomeId"), c.Query("side"), c.Query("size")
	rid := c.GetString("request_id")
	if oid == "" || side == "" || sizeS == "" {
		c.JSON(400, gin.H{"error": "outcomeId, side, and size are required"})
		return
	}
	size, _ := strconv.ParseFloat(sizeS, 64)
	if size <= 0 {
		c.JSON(400, gin.H{"error": "size must be positive"})
		return
	}
	res := routersvc.BuildAllocationPlan(c, h.st, h.cache, oid, side, size)
	if !res.OK {
		st := mapRouterErr(res.Error)
		fields := logx.Pairs(
			"request_id", rid, "outcome_id", oid, "side", side, "size", size,
			"router_code", res.Error.Code, "router_message", res.Error.Message, "detail", res.Error.Detail, "http_status", st,
		)
		logrus.WithFields(fields).Warn("交易预览失败")
		logx.Trade().WithFields(fields).Warn("交易预览失败")
		c.JSON(st, gin.H{"error": res.Error.Code, "message": res.Error.Message, "detail": res.Error.Detail})
		return
	}
	fields := logx.Pairs("request_id", rid, "outcome_id", oid, "side", side, "size", size, "allocations", len(res.Plan.Allocations))
	logrus.WithFields(fields).Info("交易预览成功")
	logx.Trade().WithFields(fields).Info("交易预览成功")
	c.JSON(200, res.Plan)
}

func (h *Handler) handleTradeExecute(c *gin.Context) {
	rid := c.GetString("request_id")
	if h.cfg.ReadOnlyMode {
		fields := logx.Pairs("request_id", rid)
		logrus.WithFields(fields).Warn("交易执行：只读模式已阻止")
		logx.Trade().WithFields(fields).Warn("交易执行：只读模式已阻止")
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	var body struct {
		OutcomeID string  `json:"outcomeId"`
		Side      string  `json:"side"`
		Size      float64 `json:"size"`
	}
	bindErr := c.BindJSON(&body)
	if bindErr != nil || body.OutcomeID == "" || body.Side == "" || body.Size <= 0 {
		if bindErr != nil {
			logrus.WithFields(logx.Pairs("request_id", rid, "bind_err", bindErr.Error())).Warn("交易执行：请求体无效")
		} else {
			logrus.WithFields(logx.Pairs("request_id", rid, "reason", "missing_fields")).Warn("交易执行：缺少必填字段")
		}
		c.JSON(400, gin.H{"error": "outcomeId, side, and size are required"})
		return
	}
	if strings.ToLower(body.Side) != "buy" {
		logrus.WithFields(logx.Pairs("request_id", rid, "side", body.Side)).Warn("交易执行：暂不支持该方向")
		c.JSON(400, gin.H{"error": "only_buy_supported"})
		return
	}
	// NOTE: account-level trade gate runs in tradeGateMiddleware before this
	// handler is reached. Per-token checks (book staleness, post-kickoff)
	// happen inside tradesvc.ExecutePlan once the per-leg tokenID resolves.
	execFields := logx.Pairs("request_id", rid, "outcome_id", body.OutcomeID, "side", body.Side, "size", body.Size)
	logrus.WithFields(execFields).Info("交易执行：收到下单请求")
	logx.Trade().WithFields(execFields).Info("交易执行：收到下单请求")
	if h.logService != nil {
		h.logService.Info("交易", fmt.Sprintf("用户下单: %s $%.2f", body.OutcomeID, body.Size))
	}
	res := routersvc.BuildAllocationPlan(c, h.st, h.cache, body.OutcomeID, body.Side, body.Size)
	if !res.OK {
		st := mapRouterErr(res.Error)
		fields := logx.Pairs(
			"request_id", rid, "outcome_id", body.OutcomeID, "size", body.Size,
			"router_code", res.Error.Code, "router_message", res.Error.Message, "detail", res.Error.Detail, "http_status", st,
		)
		logrus.WithFields(fields).Warn("交易执行：路由计划失败")
		logx.Trade().WithFields(fields).Warn("交易执行：路由计划失败")
		c.JSON(st, gin.H{"error": res.Error.Code, "message": res.Error.Message, "detail": res.Error.Detail})
		return
	}
	if _, err := polysession.ResolveAuthedCLOB(c, h.cfg, h.st); err != nil {
		logrus.WithFields(logx.Pairs("request_id", rid, "err", err.Error())).Warn("交易执行：Polymarket 会话未就绪")
		c.JSON(503, gin.H{"error": "polymarket_not_configured", "message": err.Error()})
		return
	}
	resp, code, err := tradesvc.ExecutePlan(c, h.cfg, h.st, h.cache, h.risk, res.Plan, body.Side)
	if err != nil {
		logrus.WithFields(logx.Pairs("request_id", rid, "outcome_id", body.OutcomeID, "err", err.Error())).Error("交易执行：内部错误")
		if h.logService != nil {
			h.logService.Error("交易", fmt.Sprintf("执行失败: %s", err.Error()))
		}
		c.JSON(500, gin.H{"error": "trade_failed", "message": err.Error()})
		return
	}
	if code == 422 {
		fields := logx.Pairs("request_id", rid, "outcome_id", body.OutcomeID, "response_status", resp.Status,
			"allocations", len(resp.Trades), "trades", tradeResultsLog(resp.Trades))
		logrus.WithFields(fields).Warn("交易执行：未完全成交")
		logx.Trade().WithFields(fields).Warn("交易执行：未完全成交")
	} else {
		fields := logx.Pairs("request_id", rid, "outcome_id", body.OutcomeID, "http_status", code,
			"response_status", resp.Status, "trades", tradeResultsLog(resp.Trades))
		logrus.WithFields(fields).Info("交易执行：已完成")
		logx.Trade().WithFields(fields).Info("交易执行：已完成")
		if code == 201 {
			logx.Open().WithFields(fields).Info("交易执行：开仓成交")
		}
	}
	if h.logService != nil {
		filled := 0
		for _, t := range resp.Trades {
			if t.Status == "filled" {
				filled++
			}
		}
		if filled > 0 {
			h.logService.Info("交易", fmt.Sprintf("成交 %d/%d 笔, 状态: %s", filled, len(resp.Trades), resp.Status))
		} else {
			h.logService.Warn("交易", fmt.Sprintf("下单失败: %s", resp.Message))
		}
	}
	c.JSON(code, resp)
}

func (h *Handler) handleTradesList(c *gin.Context) {
	page, limit := 1, 20
	if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
		page = p
	}
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 {
		limit = l
	}
	acct, _ := h.st.GetActivePolymarketAccount(c)
	accountID := ""
	if acct != nil {
		accountID = acct.ID
	}
	total, rows, err := h.st.ListTrades(c, page, limit, accountID)
	if err != nil {
		c.JSON(500, gin.H{"error": "list_failed"})
		return
	}
	var sportIcons map[string]string
	if sports, err := h.sportsCache.Get(c); err == nil {
		sportIcons = marketsvc.BuildSportIconMap(sports)
	}
	for _, tr := range rows {
		if sport, ok := tr["sport"].(string); ok && sport != "" {
			if icon, ok2 := sportIcons[strings.ToLower(strings.TrimSpace(sport))]; ok2 {
				tr["iconUrl"] = icon
			}
		}
	}
	c.JSON(200, gin.H{"total": total, "page": page, "limit": limit, "trades": rows})
}

func (h *Handler) handleListAccounts(c *gin.Context) {
	if cached, ok := accountsCache.Get("list"); ok {
		c.JSON(200, cached)
		return
	}
	accts, err := h.st.ListPolymarketAccounts(c)
	if err != nil {
		c.JSON(500, gin.H{"error": "list_accounts"})
		return
	}
	out := make([]gin.H, 0, len(accts))
	for _, x := range accts {
		out = append(out, gin.H{
			"id": x.ID, "name": x.Name, "funderAddress": x.FunderAddress,
			"isActive": x.IsActive, "createdAt": x.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	accountsCache.Set("list", out)
	c.JSON(200, out)
}

func (h *Handler) handleCreateAccount(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	var body struct {
		Name       string `json:"name"`
		PrivateKey string `json:"privateKey"`
	}
	if err := c.BindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.PrivateKey) == "" {
		c.JSON(400, gin.H{"error": "invalid_body"})
		return
	}
	if _, err := polyprov.ValidatePrivateKey(body.PrivateKey); err != nil {
		c.JSON(400, gin.H{"error": "invalid_private_key", "message": "无法从 privateKey 解析出 EOA"})
		return
	}
	creds, err := polyprov.FromPrivateKey(c, h.cfg, body.PrivateKey)
	if err != nil {
		c.JSON(502, gin.H{"error": "provision_failed", "message": err.Error()})
		return
	}
	n, _ := h.st.CountPolymarketAccounts(c)
	pk := strings.TrimSpace(body.PrivateKey)
	if !strings.HasPrefix(pk, "0x") {
		pk = "0x" + pk
	}
	ac := &store.PolymarketAccount{
		ID: uuid.NewString(), Name: strings.TrimSpace(body.Name),
		APIKey: creds.APIKey, Secret: creds.Secret, Passphrase: creds.Passphrase,
		PrivateKey: pk, FunderAddress: creds.FunderAddress,
		IsActive: n == 0,
	}
	if err := h.st.InsertPolymarketAccount(c, ac); err != nil {
		c.JSON(500, gin.H{"error": "db"})
		return
	}
	accountsCache.Delete("list")
	polysession.InvalidateEnvCache()
	h.app.InvalidateAndRebuildCache()
	c.JSON(201, gin.H{"id": ac.ID, "name": ac.Name, "funderAddress": ac.FunderAddress, "isActive": ac.IsActive})
}

func (h *Handler) handleActivateAccount(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	id := c.Param("id")
	if err := h.st.ActivateAccount(c, id); err != nil {
		c.JSON(500, gin.H{"error": "activate_failed"})
		return
	}
	accountsCache.Delete("list")
	polysession.InvalidateEnvCache()
	h.app.InvalidateAndRebuildCache()
	c.JSON(200, gin.H{"ok": true, "id": id})
}

func (h *Handler) handleDeleteAccount(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	id := c.Param("id")
	n, err := h.st.DeletePolymarketAccount(c, id)
	if err != nil {
		logrus.WithError(err).WithField("account_id", id).Error("delete polymarket account failed")
		if isSQLiteForeignKeyViolation(err) {
			c.JSON(500, gin.H{
				"error":   "foreign_key_blocked",
				"message": "该账号仍存在关联数据（交易记录、风控持仓等），无法删除。请先清理关联数据后再试。",
				"detail":  "仍有关联表（如 trades、risk_positions、risk_applied_clob_trades）引用此账号，需先清理或迁移后再删除。",
			})
			return
		}
		c.JSON(500, gin.H{
			"error":   "delete_failed",
			"message": "删除账号失败，请稍后重试。",
		})
		return
	}
	accountsCache.Delete("list")
	if n == 0 {
		c.JSON(404, gin.H{"error": "not_found"})
		return
	}
	polysession.InvalidateEnvCache()
	h.app.InvalidateAndRebuildCache()
	c.Status(204)
}

func (h *Handler) handleRiskPositions(c *gin.Context) {
	meta := risksvc.Meta{OutboundProxyConfigured: h.cfg.HTTPPlatformProxy != ""}
	acct, _ := h.st.GetActivePolymarketAccount(c)
	accountID := ""
	if acct != nil {
		accountID = acct.ID
	}
	fetch := func() (memcache.RiskFetchResult, error) {
		return riskPositionsFetchResult(c.Request.Context(), c.GetString("request_id"), h.risk, accountID, meta)
	}
	rows, meta2, fromCache, err := h.riskCache.GetWithRefresh(c, fetch)
	if err != nil {
		c.JSON(500, gin.H{"error": "risk"})
		return
	}
	if !fromCache {
		fields := logx.Pairs("request_id", c.GetString("request_id"), "count", len(rows))
		logx.Position().WithFields(fields).Info("风控持仓：列表已刷新")
	}
	c.JSON(200, gin.H{"positions": rows, "meta": meta2, "cached": fromCache})
}

func (h *Handler) handleRiskRefresh(c *gin.Context) {
	rid := c.GetString("request_id")
	if h.cfg.ReadOnlyMode {
		logrus.WithFields(logx.Pairs("request_id", rid)).Warn("风控刷新：只读模式已阻止")
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	var syncErr *string
	if h.risk != nil {
		if err := h.risk.SyncRiskFromRESTTrades(c); err != nil {
			es := err.Error()
			syncErr = &es
			fields := logx.Pairs("request_id", rid, "err", es)
			logrus.WithFields(fields).Warn("风控刷新：CLOB 成交同步失败")
			logx.Position().WithFields(fields).Warn("风控刷新：CLOB 成交同步失败")
		} else {
			fields := logx.Pairs("request_id", rid)
			logrus.WithFields(fields).Info("风控刷新：CLOB 成交同步成功")
			logx.Position().WithFields(fields).Info("风控刷新：CLOB 成交同步成功")
		}
	}
	if h.app != nil {
		h.app.InvalidateAndRebuildCache()
	}
	body := gin.H{"ok": true}
	if syncErr != nil {
		body["syncError"] = *syncErr
	}
	c.JSON(200, body)
}

// handleRiskTasksClear deletes terminal risk_tasks rows only (succeeded, failed,
// cancelled) so the dashboard log matches “clear completed / history”. Pending
// and running tasks are preserved so in-flight closes are not dropped.
func (h *Handler) handleRiskTasksClear(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	n, err := h.st.DeleteRiskTasksTerminal(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "clear_failed"})
		return
	}
	tasksCache.Delete("list")
	c.JSON(200, gin.H{"ok": true, "deleted": n})
}

func (h *Handler) handleRiskTasks(c *gin.Context) {
	cacheKey := "list"
	if cached, ok := tasksCache.Get(cacheKey); ok {
		go func() {
			tasks, err := h.st.ListRiskTasksRecent(context.Background(), 40)
			if err == nil {
				out := buildTaskRows(tasks)
				tasksCache.Set(cacheKey, gin.H{"tasks": out})
			}
		}()
		c.JSON(200, cached)
		return
	}
	limit := 40
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "40")); err == nil && l > 0 {
		limit = l
	}
	tasks, err := h.st.ListRiskTasksRecent(c, limit)
	if err != nil {
		c.JSON(500, gin.H{"error": "tasks"})
		return
	}
	out := buildTaskRows(tasks)
	tasksCache.Set(cacheKey, gin.H{"tasks": out})
	c.JSON(200, gin.H{"tasks": out})
}

func (h *Handler) handleRiskRuntimeLogs(c *gin.Context) {
	if h.riskRuntime == nil {
		c.JSON(200, gin.H{"logs": []any{}})
		return
	}
	limit := 100
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "100")); err == nil && l > 0 {
		if l > 500 {
			l = 500
		}
		limit = l
	}
	logs := h.riskRuntime.ListChronological(limit)
	c.JSON(200, gin.H{"logs": logs})
}

func (h *Handler) handleStopLossHistory(c *gin.Context) {
	limit := 50
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "50")); err == nil && l > 0 {
		limit = l
	}
	tasks, err := h.st.ListRiskTasksByReason(c, "close_position", "stop_loss", limit)
	if err != nil {
		c.JSON(500, gin.H{"error": "list_failed"})
		return
	}
	out := buildTaskRows(tasks)
	for i := range out {
		pid, _ := out[i]["positionId"].(string)
		if pid == "" {
			continue
		}
		pos, err := h.st.GetRiskPosition(c, pid)
		if err != nil || pos == nil {
			continue
		}
		out[i]["title"] = pos.Title
		if u := h.risk.OfficialURLForRiskPosition(c, pos); u != "" {
			out[i]["officialUrl"] = u
		}
	}
	c.JSON(200, gin.H{"tasks": out})
}

func (h *Handler) handleTradeHistory(c *gin.Context) {
	limit := 50
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "50")); err == nil && l > 0 {
		limit = l
	}
	trades, err := h.risk.ListOfficialTrades(c, limit)
	if err != nil {
		c.JSON(500, gin.H{"error": "list_failed", "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"trades": trades})
}

func buildTaskRows(tasks []store.RiskTask) []gin.H {
	out := make([]gin.H, 0, len(tasks))
	for _, t := range tasks {
		pid := interface{}(nil)
		if t.PositionID.Valid {
			pid = t.PositionID.String
		}
		le := interface{}(nil)
		if t.LastError.Valid {
			le = t.LastError.String
		}
		reason := interface{}(nil)
		if t.Reason.Valid {
			reason = t.Reason.String
		}
		lad := interface{}(nil)
		if t.LastAttemptDetail.Valid {
			lad = t.LastAttemptDetail.String
		}
		out = append(out, gin.H{
			"id": t.ID, "type": t.Type, "positionId": pid, "status": t.Status,
			"attempts": t.Attempts, "lastError": le, "reason": reason,
			"createdAt":         t.CreatedAt.UTC().Format(time.RFC3339Nano),
			"nextRunAt":         t.NextRunAt.UTC().Format(time.RFC3339Nano),
			"updatedAt":         t.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"lastAttemptDetail": lad,
		})
	}
	return out
}

func (h *Handler) handlePatchRiskPosition(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	var body struct {
		StopLossPct    *float64 `json:"stopLossPct"`
		HighWaterCents *float64 `json:"highWaterCents"`
	}
	_ = c.BindJSON(&body)
	if body.StopLossPct == nil && body.HighWaterCents == nil {
		c.JSON(400, gin.H{"error": "no_fields", "message": "stopLossPct or highWaterCents required"})
		return
	}
	id := c.Param("id")
	if err := h.st.UpdateRiskPositionStop(c, id, body.StopLossPct, body.HighWaterCents); err != nil {
		if errors.Is(err, store.ErrRiskPatchNoFields) {
			c.JSON(400, gin.H{"error": "no_fields"})
			return
		}
		c.JSON(400, gin.H{"error": "update_failed"})
		return
	}
	p, err := h.st.GetRiskPosition(c, id)
	if err != nil || p == nil {
		c.JSON(404, gin.H{"error": "not_found"})
		return
	}
	fields := logx.Pairs("request_id", c.GetString("request_id"), "position_id", id)
	if body.StopLossPct != nil {
		fields["stop_loss_pct"] = *body.StopLossPct
	}
	if body.HighWaterCents != nil {
		floored := risksvc.FloorCents1(*body.HighWaterCents)
		body.HighWaterCents = &floored
		fields["high_water_cents"] = floored
	}
	logx.Position().WithFields(fields).Info("风控持仓：PATCH 已更新")
	c.JSON(200, gin.H{"ok": true, "position": h.riskRowFromPosition(c, p)})
}

func (h *Handler) riskRowFromPosition(ctx context.Context, p *store.RiskPosition) gin.H {
	row := riskRowFromPosition(p)
	// Re-derive trail using the configured absolute cent floor when present.
	// The unauthenticated/test variant keeps the legacy formula (no store).
	hw := risksvc.FloorCents1(p.HighWaterCents)
	row["trailingStopCents"] = risksvc.TrailingStopCentsFromHWWithAbs(hw, p.StopLossPct, h.st.GetBotConfigFloat(ctx, "priceStopLossAbsCents", 0))

	if bid, ask, ok := h.risk.BestBidAskCents(ctx, p.TokenID); ok {
		cur := bid
		if cur <= 0 && ask > 0 {
			cur = ask
		}
		row["currentCents"] = cur
		v := cur / 100 * p.SizeShares
		row["valueUsd"] = v
		pnl := v - p.CostUSD
		row["pnlUsd"] = pnl
	}

	return row
}

func riskRowFromPosition(p *store.RiskPosition) gin.H {
	hw := risksvc.FloorCents1(p.HighWaterCents)
	trail := risksvc.TrailingStopCentsFromHW(hw, p.StopLossPct)
	tid := strings.ToLower(strings.TrimSpace(p.TokenID))
	return gin.H{
		"id": p.ID, "title": p.Title, "sideLabel": p.SideLabel, "tokenId": tid,
		"avgEntryCents": p.AvgEntryCents, "currentCents": nil,
		"sizeShares": p.SizeShares, "costUsd": p.CostUSD,
		"highWaterCents": hw, "stopLossPct": p.StopLossPct, "trailingStopCents": trail,
		"valueUsd": nil, "pnlUsd": nil, "maxPayoffUsd": p.SizeShares, "potentialProfitUsd": p.SizeShares - p.CostUSD,
		"status": p.Status, "source": p.Source,
	}
}

// handleRiskGate exposes current trade-gate state (kill switch, halt, WS health).
// Operators inspect this to understand why /api/trade is rejecting.
func (h *Handler) handleRiskGate(c *gin.Context) {
	manualHalted := false
	if v, ok, _ := h.st.GetBotConfig(c, "riskTradingHalted"); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			manualHalted = true
		}
	}
	autoHalted, autoReason := h.risk.AutoHaltStatus()
	wsDown, wsReason := h.risk.WSMarketDown()
	last := h.risk.LastBookTickAt()
	gateErr := h.risk.EnsureTradeAllowed(c, "")
	out := gin.H{
		"manualHalted":           manualHalted,
		"autoHalted":             autoHalted,
		"autoHaltReason":         autoReason,
		"wsMarketDown":           wsDown,
		"wsMarketReason":         wsReason,
		"maxDailyLossUSD":        h.st.GetBotConfigFloat(c, "riskMaxDailyLossUSD", 0),
		"maxOpenPositions":       h.st.GetBotConfigInt(c, "riskMaxOpenPositions", 0),
		"maxAccountExposureUSD":  h.st.GetBotConfigFloat(c, "riskMaxAccountExposureUSD", 0),
		"maxMarketExposureUSD":   h.st.GetBotConfigFloat(c, "riskMaxMarketExposureUSD", 0),
		"bookMaxAgeMs":           h.st.GetBotConfigInt(c, "riskBookMaxAgeMs", 0),
		"maxReconcileGapSec":     h.st.GetBotConfigInt(c, "riskMaxReconcileGapSec", 0),
		"stopLossAbsCents":       h.st.GetBotConfigFloat(c, "priceStopLossAbsCents", 0),
		"openTradeAllowed":       gateErr == nil,
	}
	// Surface live exposure totals for the active account (best-effort;
	// failure is silent so the gate snapshot endpoint still serves config).
	if acct, _ := h.st.GetActivePolymarketAccount(c); acct != nil {
		if v, err := h.st.AccountOpenExposureUSD(c, acct.ID); err == nil {
			out["accountExposureUSD"] = v
		}
	}
	if !last.IsZero() {
		out["lastBookTickAt"] = last.Format(time.RFC3339)
	}
	if gateErr != nil {
		out["openTradeReject"] = gin.H{
			"code":    gateErr.Code,
			"message": gateErr.Message,
			"detail":  gateErr.Detail,
		}
	}
	if snap, err := h.risk.EvaluateKillSwitch(c); err == nil {
		out["killSwitch"] = gin.H{
			"thresholdUsd":     snap.ThresholdUSD,
			"unrealizedUsd":    snap.UnrealizedUSD,
			"realizedUsd":      snap.RealizedUSD,
			"totalPnlUsd":      snap.TotalPnLUSD,
			"windowSec":        snap.WindowSec,
			"openPositions":    snap.OpenPositions,
			"bookCovered":      snap.BookCovered,
			"bookMissing":      snap.BookMissing,
			"worstPositionUsd": snap.WorstPositionUSD,
			"tripped":          snap.Tripped,
			"reason":           snap.Reason,
		}
	}
	c.JSON(200, out)
}

// handleTradeQualityRecent returns the most recent fills (newest first) for
// the active account. Useful for spot-checking slippage.
func (h *Handler) handleTradeQualityRecent(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	accountID := ""
	if acct, _ := h.st.GetActivePolymarketAccount(c); acct != nil {
		accountID = acct.ID
	}
	rows, err := h.st.ListRecentTradeQuality(c, accountID, limit)
	if err != nil {
		c.JSON(500, gin.H{"error": "trade_quality_list", "message": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, t := range rows {
		out = append(out, gin.H{
			"id":             t.ID,
			"createdAt":      t.CreatedAt.UTC().Format(time.RFC3339Nano),
			"side":           t.Side,
			"orderType":      t.OrderType,
			"tokenId":        t.TokenID,
			"expectedOdds":   t.ExpectedOdds,
			"fillOdds":       t.FillOdds,
			"limitOdds":      t.LimitOdds,
			"bestBid":        t.BestBid,
			"bestAsk":        t.BestAsk,
			"slippageBps":    t.SlippageBps,
			"size":           t.Size,
			"submitLatencyMs": t.SubmitLatencyMs,
			"tradeId":        t.TradeID,
			"riskTaskId":     t.RiskTaskID,
			"notes":          t.Notes,
		})
	}
	c.JSON(200, gin.H{"rows": out, "count": len(out)})
}

// handleTradeQualityAggregate returns aggregate slippage stats over a window.
// Defaults to the last 24h. Use ?windowSec=N to override.
func (h *Handler) handleTradeQualityAggregate(c *gin.Context) {
	windowSec, _ := strconv.Atoi(c.DefaultQuery("windowSec", "86400"))
	if windowSec <= 0 {
		windowSec = 86400
	}
	accountID := ""
	if acct, _ := h.st.GetActivePolymarketAccount(c); acct != nil {
		accountID = acct.ID
	}
	since := time.Now().UTC().Add(-time.Duration(windowSec) * time.Second)
	agg, err := h.st.AggregateTradeQuality(c, accountID, since)
	if err != nil {
		c.JSON(500, gin.H{"error": "trade_quality_aggregate", "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"windowSec":  windowSec,
		"since":      since.Format(time.RFC3339),
		"accountId":  accountID,
		"aggregate":  agg,
	})
}

// handleRiskKillSwitchClear clears the auto-halt flag (manual halt via bot
// config is unaffected).
func (h *Handler) handleRiskKillSwitchClear(c *gin.Context) {
	rid := c.GetString("request_id")
	h.risk.ClearAutoHalt(c, rid)
	logrus.WithFields(logx.Pairs("request_id", rid)).Info("风控：API 调用已清除 KILL SWITCH 自动标志")
	logx.Trade().WithFields(logx.Pairs("request_id", rid)).Info("风控：API 调用已清除 KILL SWITCH 自动标志")
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) handleClosePosition(c *gin.Context) {
	rid := c.GetString("request_id")
	pid := c.Param("id")
	if h.cfg.ReadOnlyMode {
		logrus.WithFields(logx.Pairs("request_id", rid, "position_id", pid)).Warn("风控平仓：只读模式已阻止")
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	fields := logx.Pairs("request_id", rid, "position_id", pid)
	logrus.WithFields(fields).Info("风控平仓：API 入队请求")
	logx.StopLoss().WithFields(fields).Info("风控平仓：API 入队请求")
	if err := h.risk.EnqueueClosePosition(c, pid); err != nil {
		logrus.WithFields(logx.Pairs("request_id", rid, "position_id", pid, "err", err.Error())).Error("风控平仓：入队失败")
		c.JSON(500, gin.H{"error": "enqueue_failed"})
		return
	}
	c.JSON(200, gin.H{"ok": true, "positionId": pid})
}

func (h *Handler) handleCloseAll(c *gin.Context) {
	rid := c.GetString("request_id")
	if h.cfg.ReadOnlyMode {
		logrus.WithFields(logx.Pairs("request_id", rid)).Warn("风控一键平仓：只读模式已阻止")
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	t := &store.RiskTask{ID: uuid.NewString(), Type: "close_all", Status: "pending", NextRunAt: time.Now().UTC()}
	if err := h.st.InsertRiskTask(c, t); err != nil {
		logrus.WithFields(logx.Pairs("request_id", rid, "err", err.Error())).Error("风控一键平仓：入队失败")
		c.JSON(500, gin.H{"error": "enqueue_failed"})
		return
	}
	logrus.WithFields(logx.Pairs("request_id", rid, "task_id", t.ID)).Info("风控一键平仓：任务已入队")
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) handleRiskHiddenList(c *gin.Context) {
	acct, err := h.st.GetActivePolymarketAccount(c)
	if err != nil || acct == nil {
		c.JSON(400, gin.H{"error": "no_active_account"})
		return
	}
	rows, err := h.st.ListRiskHiddenPositions(c.Request.Context(), acct.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": "db"})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{"tokenId": r.TokenID, "sideLabel": r.SideLabel, "createdAt": r.CreatedAt})
	}
	c.JSON(200, gin.H{"hidden": out})
}

func (h *Handler) handleRiskHiddenPost(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	acct, err := h.st.GetActivePolymarketAccount(c)
	if err != nil || acct == nil {
		c.JSON(400, gin.H{"error": "no_active_account"})
		return
	}
	var body struct {
		TokenID   string `json:"tokenId"`
		SideLabel string `json:"sideLabel"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "bad_body"})
		return
	}
	if strings.TrimSpace(body.TokenID) == "" {
		c.JSON(400, gin.H{"error": "tokenId_required"})
		return
	}
	if err := h.st.UpsertRiskHiddenPosition(c.Request.Context(), acct.ID, body.TokenID, body.SideLabel); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if h.riskCache != nil {
		h.riskCache.Invalidate(c.Request.Context())
	}
	if h.app != nil {
		h.app.InvalidateAndRebuildCache()
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) handleRiskHiddenDelete(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	acct, err := h.st.GetActivePolymarketAccount(c)
	if err != nil || acct == nil {
		c.JSON(400, gin.H{"error": "no_active_account"})
		return
	}
	var body struct {
		TokenID   string `json:"tokenId"`
		SideLabel string `json:"sideLabel"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "bad_body"})
		return
	}
	if err := h.st.DeleteRiskHiddenPosition(c.Request.Context(), acct.ID, body.TokenID, body.SideLabel); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if h.riskCache != nil {
		h.riskCache.Invalidate(c.Request.Context())
	}
	if h.app != nil {
		h.app.InvalidateAndRebuildCache()
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) handleStatsMarkets(c *gin.Context) {
	c.JSON(200, gin.H{
		"bestOddsMatched24h":    gin.H{"poly": 0, "total": 0},
		"bestOddsAllMatched24h": gin.H{"poly": 0, "total": 0},
		"edgeMatched24h":        nil,
		"edgeAllMatched24h":     nil,
	})
}

func mapRouterErr(e *routersvc.RouterError) int {
	if e == nil {
		return 400
	}
	switch e.Code {
	case "outcome_not_found":
		return 404
	case "size_exceeds_max":
		return 400
	case "slippage_exceeded":
		return 422
	default:
		return 400
	}
}
