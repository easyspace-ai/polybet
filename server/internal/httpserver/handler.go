package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/debounce"
	"github.com/easyspace-ai/polybet/internal/polyprov"
	"github.com/easyspace-ai/polybet/internal/rediska"
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
	risk         *risksvc.Service
	debounce     *debounce.Debouncer
	balanceCache *rediska.BalanceCache
	riskCache    *rediska.RiskCache
	initService  *initsvc.Service
	logService   *logsvc.Service
	sportsCache  *mktSync.SportsCache
	app          interface {
		InvalidateAndRebuildCache()
		SyncAndBroadcastMarkets(ctx context.Context) error
		RequestRestart()
	}
}

func NewHandler(d Deps) *Handler {
	return &Handler{
		cfg:          d.Cfg,
		db:           d.DB,
		st:           d.Store,
		cache:        d.Cache,
		hub:          d.Hub,
		risk:         d.Risk,
		debounce:     d.Debounce,
		balanceCache: d.BalanceCache,
		riskCache:    d.RiskCache,
		initService:  d.InitService,
		logService:   d.LogService,
		sportsCache:  d.SportsCache,
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
	slog.Info("restart_request_via_api", "request_id", c.GetString("request_id"))
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
	if err := h.st.UpsertBotConfig(c, key, body.Value); err != nil {
		c.JSON(500, gin.H{"error": "update_failed"})
		return
	}
	c.JSON(200, gin.H{"key": key, "value": body.Value})
}

func (h *Handler) handleTelegramTest(c *gin.Context) {
	token, chat := tg.ResolveTelegramCreds(c, h.cfg, h.st)
	rid := c.GetString("request_id")
	slog.Info("telegram_test_request", "request_id", rid, "token_set", token != "", "chat_set", chat != "", "proxy", h.cfg.HTTPPlatformProxy)
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
		slog.Warn("telegram_test_request_create_failed", "request_id", rid, "err", err.Error())
		c.JSON(500, gin.H{"error": "request_failed", "message": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var hc *http.Client
	if h.cfg.HTTPPlatformProxy != "" {
		if proxyURL, err := url.Parse(h.cfg.HTTPPlatformProxy); err == nil {
			slog.Info("telegram_test_using_proxy", "request_id", rid, "proxy", h.cfg.HTTPPlatformProxy)
			hc = &http.Client{
				Timeout: 14 * time.Second,
				Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
			}
		}
	}
	if hc == nil {
		slog.Info("telegram_test_no_proxy", "request_id", rid)
		hc = &http.Client{Timeout: 14 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		slog.Warn("telegram_test_send_failed", "request_id", rid, "err", err.Error())
		c.JSON(502, gin.H{"error": "send_failed", "message": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	slog.Info("telegram_test_response", "request_id", rid, "status", resp.StatusCode, "body", string(body))
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
		"wsClients": hubSize,
		"serverTime": time.Now().Format("2006-01-02 15:04:05"),
	})
}

func (h *Handler) handleWSStatus(c *gin.Context) {
	hubSize := 0
	if h.hub != nil {
		hubSize = h.hub.ClientCount()
	}
	c.JSON(200, gin.H{
		"dashConnected":         hubSize > 0,
		"dashClients":           hubSize,
		"polyOrderbookConnected":  h.risk.OrderbookWSConnected(),
		"polyOrderbookConnecting": h.risk.OrderbookWSConnecting(),
		"polyUserConnected":       h.risk.UserWSConnected(),
		"polyUserConnecting":      h.risk.UserWSConnecting(),
	})
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
		slog.Warn("markets_refresh_blocked_read_only", "request_id", rid)
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	slog.Info("markets_refresh_request", "request_id", rid)
	if h.logService != nil {
		h.logService.Info("市场同步", "用户触发强制刷新")
	}
	if err := h.app.SyncAndBroadcastMarkets(c); err != nil {
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
	type lvl struct {
		Odds     float64 `json:"odds"`
		Size     float64 `json:"size"`
		Platform string  `json:"platform"`
	}
	levels := make([]lvl, 0)
	var polyTok string
	for _, o := range rows {
		if o.ExternalID.Valid && polyTok == "" {
			polyTok = o.ExternalID.String
		}
		tok := ""
		if o.ExternalID.Valid {
			tok = o.ExternalID.String
		}
		for _, L := range h.cache.GetLevels(tok) {
			levels = append(levels, lvl{Odds: L.Odds, Size: L.Size, Platform: "polymarket"})
		}
	}
	c.JSON(200, gin.H{"levels": levels, "polyTokenId": polyTok})
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
		slog.Warn("trade_preview_failed",
			"request_id", rid, "outcome_id", oid, "side", side, "size", size,
			"router_code", res.Error.Code, "router_message", res.Error.Message, "detail", res.Error.Detail, "http_status", st)
		c.JSON(st, gin.H{"error": res.Error.Code, "message": res.Error.Message, "detail": res.Error.Detail})
		return
	}
	slog.Info("trade_preview_ok", "request_id", rid, "outcome_id", oid, "side", side, "size", size, "allocations", len(res.Plan.Allocations))
	c.JSON(200, res.Plan)
}

func (h *Handler) handleTradeExecute(c *gin.Context) {
	rid := c.GetString("request_id")
	if h.cfg.ReadOnlyMode {
		slog.Warn("trade_blocked_read_only", "request_id", rid)
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
			slog.Warn("trade_bad_request", "request_id", rid, "bind_err", bindErr.Error())
		} else {
			slog.Warn("trade_bad_request", "request_id", rid, "reason", "missing_fields")
		}
		c.JSON(400, gin.H{"error": "outcomeId, side, and size are required"})
		return
	}
	if strings.ToLower(body.Side) != "buy" {
		slog.Warn("trade_side_not_supported", "request_id", rid, "side", body.Side)
		c.JSON(400, gin.H{"error": "only_buy_supported"})
		return
	}
	slog.Info("trade_request", "request_id", rid, "outcome_id", body.OutcomeID, "side", body.Side, "size", body.Size)
	if h.logService != nil {
		h.logService.Info("交易", fmt.Sprintf("用户下单: %s $%.2f", body.OutcomeID, body.Size))
	}
	res := routersvc.BuildAllocationPlan(c, h.st, h.cache, body.OutcomeID, body.Side, body.Size)
	if !res.OK {
		st := mapRouterErr(res.Error)
		slog.Warn("trade_plan_failed",
			"request_id", rid, "outcome_id", body.OutcomeID, "size", body.Size,
			"router_code", res.Error.Code, "router_message", res.Error.Message, "detail", res.Error.Detail, "http_status", st)
		c.JSON(st, gin.H{"error": res.Error.Code, "message": res.Error.Message, "detail": res.Error.Detail})
		return
	}
	if _, err := polysession.ResolveAuthedCLOB(c, h.cfg, h.st); err != nil {
		slog.Warn("trade_polymarket_session_missing", "request_id", rid, "err", err.Error())
		c.JSON(503, gin.H{"error": "polymarket_not_configured", "message": err.Error()})
		return
	}
	resp, code, err := tradesvc.ExecutePlan(c, h.cfg, h.st, h.cache, h.risk, res.Plan, body.Side)
	if err != nil {
		slog.Error("trade_execute_error", "request_id", rid, "outcome_id", body.OutcomeID, "err", err.Error())
		if h.logService != nil {
			h.logService.Error("交易", fmt.Sprintf("执行失败: %s", err.Error()))
		}
		c.JSON(500, gin.H{"error": "trade_failed", "message": err.Error()})
		return
	}
	if code == 422 {
		slog.Warn("trade_execute_no_fill", "request_id", rid, "outcome_id", body.OutcomeID, "response_status", resp.Status,
			"allocations", len(resp.Trades), "trades", tradeResultsLog(resp.Trades))
	} else {
		slog.Info("trade_execute_done", "request_id", rid, "outcome_id", body.OutcomeID, "http_status", code,
			"response_status", resp.Status, "trades", tradeResultsLog(resp.Trades))
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
		go func() {
			accts, err := h.st.ListPolymarketAccounts(context.Background())
			if err == nil {
				out := make([]gin.H, 0, len(accts))
				for _, x := range accts {
					out = append(out, gin.H{
						"id": x.ID, "name": x.Name, "funderAddress": x.FunderAddress,
						"isActive": x.IsActive, "createdAt": x.CreatedAt.UTC().Format(time.RFC3339Nano),
					})
				}
				accountsCache.Set("list", out)
			}
		}()
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
	_ = h.st.DeletePolymarketAccount(c, c.Param("id"))
	accountsCache.Delete("list")
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
	fetch := func() (rediska.RiskFetchResult, error) {
		rows, m, err := h.risk.ListRiskPositionsEnriched(c, meta, accountID)
		if err != nil {
			return rediska.RiskFetchResult{}, err
		}
		return rediska.RiskFetchResult{
			Positions: rows,
			Meta: rediska.RiskMeta{
				UserWsConnected:         m.UserWsConnected,
				UserWsConnecting:        m.UserWsConnecting,
				OutboundProxyConfigured: m.OutboundProxyConfigured,
				MinOpenRiskShares:       m.MinOpenRiskShares,
			},
		}, nil
	}
	rows, meta2, fromCache, err := h.riskCache.GetWithRefresh(c, fetch)
	if err != nil {
		c.JSON(500, gin.H{"error": "risk"})
		return
	}
	c.JSON(200, gin.H{"positions": rows, "meta": meta2, "cached": fromCache})
}

func (h *Handler) handleRiskRefresh(c *gin.Context) {
	rid := c.GetString("request_id")
	if h.cfg.ReadOnlyMode {
		slog.Warn("risk_refresh_blocked_read_only", "request_id", rid)
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	var syncErr *string
	if h.risk != nil {
		if err := h.risk.SyncRiskFromRESTTrades(c); err != nil {
			es := err.Error()
			syncErr = &es
			slog.Warn("risk_refresh_clob_sync", "request_id", rid, "err", es)
		} else {
			slog.Info("risk_refresh_clob_sync_ok", "request_id", rid)
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
		out = append(out, gin.H{
			"id": t.ID, "type": t.Type, "positionId": pid, "status": t.Status,
			"attempts": t.Attempts, "lastError": le, "reason": reason,
			"createdAt": t.CreatedAt.UTC().Format(time.RFC3339Nano),
			"nextRunAt": t.NextRunAt.UTC().Format(time.RFC3339Nano),
			"updatedAt": t.UpdatedAt.UTC().Format(time.RFC3339Nano),
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
	c.JSON(200, gin.H{"ok": true, "position": riskRowFromPosition(p)})
}

func (h *Handler) handleClosePosition(c *gin.Context) {
	rid := c.GetString("request_id")
	pid := c.Param("id")
	if h.cfg.ReadOnlyMode {
		slog.Warn("risk_close_blocked_read_only", "request_id", rid, "position_id", pid)
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	slog.Info("risk_close_api", "request_id", rid, "position_id", pid)
	if err := h.risk.EnqueueClosePosition(c, pid); err != nil {
		slog.Error("risk_close_enqueue_failed", "request_id", rid, "position_id", pid, "err", err.Error())
		c.JSON(500, gin.H{"error": "enqueue_failed"})
		return
	}
	c.JSON(200, gin.H{"ok": true, "positionId": pid})
}

func (h *Handler) handleCloseAll(c *gin.Context) {
	rid := c.GetString("request_id")
	if h.cfg.ReadOnlyMode {
		slog.Warn("risk_close_all_blocked_read_only", "request_id", rid)
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	t := &store.RiskTask{ID: uuid.NewString(), Type: "close_all", Status: "pending", NextRunAt: time.Now().UTC()}
	if err := h.st.InsertRiskTask(c, t); err != nil {
		slog.Error("risk_close_all_enqueue_failed", "request_id", rid, "err", err.Error())
		c.JSON(500, gin.H{"error": "enqueue_failed"})
		return
	}
	slog.Info("risk_close_all_enqueued", "request_id", rid, "task_id", t.ID)
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

func riskRowFromPosition(p *store.RiskPosition) gin.H {
	hw := p.HighWaterCents
	trail := hw * (1 - p.StopLossPct/100)
	return gin.H{
		"id": p.ID, "title": p.Title, "sideLabel": p.SideLabel, "tokenId": p.TokenID,
		"avgEntryCents": p.AvgEntryCents, "currentCents": nil,
		"sizeShares": p.SizeShares, "costUsd": p.CostUSD,
		"highWaterCents": hw, "stopLossPct": p.StopLossPct, "trailingStopCents": trail,
		"valueUsd": nil, "pnlUsd": nil, "maxPayoffUsd": p.SizeShares, "potentialProfitUsd": p.SizeShares - p.CostUSD,
		"status": p.Status, "source": p.Source,
	}
}
