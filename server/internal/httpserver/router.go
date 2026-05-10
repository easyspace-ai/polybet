package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/easyspace-ai/polybet/internal/homesettings"
	"github.com/easyspace-ai/polybet/internal/polyprov"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/service/marketsvc"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/service/routersvc"
	"github.com/easyspace-ai/polybet/internal/service/tradesvc"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/webui"
)

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

func NewRouter(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestID())
	r.Use(cors(d.Cfg.CORSOrigins))

	r.GET("/api/health", func(c *gin.Context) {
		if err := d.DB.PingContext(c); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "db": "unreachable", "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "db": "connected"})
	})

	api := r.Group("/api")
	{
		api.GET("/markets", func(c *gin.Context) {
			data, err := marketsvc.BuildMarketsPayload(c, d.Store, d.Cache)
			if err != nil {
				c.JSON(500, gin.H{"error": "markets_failed"})
				return
			}
			c.JSON(200, data)
		})

		api.GET("/trade/orderbook", func(c *gin.Context) {
			oid := c.Query("outcomeId")
			if oid == "" {
				c.JSON(400, gin.H{"error": "outcomeId required"})
				return
			}
			rows, err := d.Store.ListRouterPolySiblings(c, oid)
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
				for _, L := range d.Cache.GetLevels(tok) {
					levels = append(levels, lvl{Odds: L.Odds, Size: L.Size, Platform: "polymarket"})
				}
			}
			c.JSON(200, gin.H{"levels": levels, "polyTokenId": polyTok})
		})

		api.GET("/trade/preview", func(c *gin.Context) {
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
			res := routersvc.BuildAllocationPlan(c, d.Store, d.Cache, oid, side, size)
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
		})

		api.POST("/trade", func(c *gin.Context) {
			rid := c.GetString("request_id")
			if d.Cfg.ReadOnlyMode {
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
			res := routersvc.BuildAllocationPlan(c, d.Store, d.Cache, body.OutcomeID, body.Side, body.Size)
			if !res.OK {
				st := mapRouterErr(res.Error)
				slog.Warn("trade_plan_failed",
					"request_id", rid, "outcome_id", body.OutcomeID, "size", body.Size,
					"router_code", res.Error.Code, "router_message", res.Error.Message, "detail", res.Error.Detail, "http_status", st)
				c.JSON(st, gin.H{"error": res.Error.Code, "message": res.Error.Message, "detail": res.Error.Detail})
				return
			}
			if _, err := polysession.ResolveAuthedCLOB(c, d.Cfg, d.Store); err != nil {
				slog.Warn("trade_polymarket_session_missing", "request_id", rid, "err", err.Error())
				c.JSON(503, gin.H{"error": "polymarket_not_configured", "message": err.Error()})
				return
			}
			resp, code, err := tradesvc.ExecutePlan(c, d.Cfg, d.Store, d.Cache, d.Risk, res.Plan, body.Side)
			if err != nil {
				slog.Error("trade_execute_error", "request_id", rid, "outcome_id", body.OutcomeID, "err", err.Error())
				c.JSON(500, gin.H{"error": "trade_failed", "message": err.Error()})
				return
			}
			// 422 = plan built but no leg filled (FOK rejected / no liquidity); still log as warning for ops.
			if code == 422 {
				slog.Warn("trade_execute_no_fill", "request_id", rid, "outcome_id", body.OutcomeID, "response_status", resp.Status,
					"allocations", len(resp.Trades), "trades", tradeResultsLog(resp.Trades))
			} else {
				slog.Info("trade_execute_done", "request_id", rid, "outcome_id", body.OutcomeID, "http_status", code,
					"response_status", resp.Status, "trades", tradeResultsLog(resp.Trades))
			}
			c.JSON(code, resp)
		})

		api.GET("/trades", func(c *gin.Context) {
			page, limit := 1, 20
			if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
				page = p
			}
			if l, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && l > 0 {
				limit = l
			}
			total, rows, err := d.Store.ListTrades(c, page, limit)
			if err != nil {
				c.JSON(500, gin.H{"error": "list_failed"})
				return
			}
			c.JSON(200, gin.H{"total": total, "page": page, "limit": limit, "trades": rows})
		})

		api.GET("/config", func(c *gin.Context) {
			rows, err := d.Store.ListBotConfig(c)
			if err != nil {
				c.JSON(500, gin.H{"error": "config"})
				return
			}
			out := make([]gin.H, 0, len(rows))
			for _, r := range rows {
				out = append(out, gin.H{"key": r.Key, "value": r.Value})
			}
			c.JSON(200, out)
		})

		api.PUT("/config/:key", func(c *gin.Context) {
			if d.Cfg.ReadOnlyMode {
				c.JSON(403, gin.H{"error": "read_only"})
				return
			}
			var body struct {
				Value string `json:"value"`
			}
			_ = c.BindJSON(&body)
			if err := d.Store.UpsertBotConfig(c, c.Param("key"), body.Value); err != nil {
				c.JSON(500, gin.H{"error": "update_failed"})
				return
			}
			snapshotHomeBotSettings(c.Request.Context(), d.Store)
			c.JSON(200, gin.H{"key": c.Param("key"), "value": body.Value})
		})

		api.GET("/balances", func(c *gin.Context) {
			sum, err := balancesvc.Fetch(c, d.Cfg, d.Store)
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
			c.JSON(200, gin.H{"polymarket": sum.Polymarket, "polymarketAccounts": accts})
		})

		api.GET("/polymarket/accounts", func(c *gin.Context) {
			accts, err := d.Store.ListPolymarketAccounts(c)
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
			c.JSON(200, out)
		})

		api.POST("/polymarket/accounts", func(c *gin.Context) {
			if d.Cfg.ReadOnlyMode {
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
			creds, err := polyprov.FromPrivateKey(c, d.Cfg, body.PrivateKey)
			if err != nil {
				c.JSON(502, gin.H{"error": "provision_failed", "message": err.Error()})
				return
			}
			n, _ := d.Store.CountPolymarketAccounts(c)
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
			if err := d.Store.InsertPolymarketAccount(c, ac); err != nil {
				c.JSON(500, gin.H{"error": "db"})
				return
			}
			polysession.InvalidateEnvCache()
			c.JSON(201, gin.H{"id": ac.ID, "name": ac.Name, "funderAddress": ac.FunderAddress, "isActive": ac.IsActive})
		})

		api.PATCH("/polymarket/accounts/:id/activate", func(c *gin.Context) {
			if d.Cfg.ReadOnlyMode {
				c.JSON(403, gin.H{"error": "read_only"})
				return
			}
			if err := d.Store.ActivateAccount(c, c.Param("id")); err != nil {
				c.JSON(500, gin.H{"error": "activate_failed"})
				return
			}
			polysession.InvalidateEnvCache()
			c.JSON(200, gin.H{"ok": true, "id": c.Param("id")})
		})

		api.DELETE("/polymarket/accounts/:id", func(c *gin.Context) {
			if d.Cfg.ReadOnlyMode {
				c.JSON(403, gin.H{"error": "read_only"})
				return
			}
			_ = d.Store.DeletePolymarketAccount(c, c.Param("id"))
			polysession.InvalidateEnvCache()
			c.Status(204)
		})

		api.GET("/risk/positions", func(c *gin.Context) {
			meta := risksvc.Meta{OutboundProxyConfigured: d.Cfg.HTTPPlatformProxy != ""}
			rows, meta2, err := d.Risk.ListRiskPositionsEnriched(c, meta)
			if err != nil {
				c.JSON(500, gin.H{"error": "risk"})
				return
			}
			c.JSON(200, gin.H{"positions": rows, "meta": meta2})
		})

		api.GET("/risk/tasks", func(c *gin.Context) {
			limit := 40
			if l, err := strconv.Atoi(c.DefaultQuery("limit", "40")); err == nil && l > 0 {
				limit = l
			}
			tasks, err := d.Store.ListRiskTasksRecent(c, limit)
			if err != nil {
				c.JSON(500, gin.H{"error": "tasks"})
				return
			}
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
				out = append(out, gin.H{
					"id": t.ID, "type": t.Type, "positionId": pid, "status": t.Status,
					"attempts": t.Attempts, "lastError": le,
					"nextRunAt": t.NextRunAt.UTC().Format(time.RFC3339Nano),
					"updatedAt": t.UpdatedAt.UTC().Format(time.RFC3339Nano),
				})
			}
			c.JSON(200, gin.H{"tasks": out})
		})

		api.PATCH("/risk/positions/:id", func(c *gin.Context) {
			if d.Cfg.ReadOnlyMode {
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
			if err := d.Store.UpdateRiskPositionStop(c, c.Param("id"), body.StopLossPct, body.HighWaterCents); err != nil {
				if errors.Is(err, store.ErrRiskPatchNoFields) {
					c.JSON(400, gin.H{"error": "no_fields"})
					return
				}
				c.JSON(400, gin.H{"error": "update_failed"})
				return
			}
			p, err := d.Store.GetRiskPosition(c, c.Param("id"))
			if err != nil || p == nil {
				c.JSON(404, gin.H{"error": "not_found"})
				return
			}
			c.JSON(200, gin.H{"ok": true, "position": riskRowFromPosition(p)})
		})

		api.POST("/risk/positions/:id/close", func(c *gin.Context) {
			rid := c.GetString("request_id")
			pid := c.Param("id")
			if d.Cfg.ReadOnlyMode {
				slog.Warn("risk_close_blocked_read_only", "request_id", rid, "position_id", pid)
				c.JSON(403, gin.H{"error": "read_only"})
				return
			}
			slog.Info("risk_close_api", "request_id", rid, "position_id", pid)
			if err := d.Risk.EnqueueClosePosition(c, pid); err != nil {
				slog.Error("risk_close_enqueue_failed", "request_id", rid, "position_id", pid, "err", err.Error())
				c.JSON(500, gin.H{"error": "enqueue_failed"})
				return
			}
			c.JSON(200, gin.H{"ok": true, "positionId": pid})
		})

		api.POST("/risk/close-all", func(c *gin.Context) {
			rid := c.GetString("request_id")
			if d.Cfg.ReadOnlyMode {
				slog.Warn("risk_close_all_blocked_read_only", "request_id", rid)
				c.JSON(403, gin.H{"error": "read_only"})
				return
			}
			t := &store.RiskTask{ID: uuid.NewString(), Type: "close_all", Status: "pending", NextRunAt: time.Now().UTC()}
			if err := d.Store.InsertRiskTask(c, t); err != nil {
				slog.Error("risk_close_all_enqueue_failed", "request_id", rid, "err", err.Error())
				c.JSON(500, gin.H{"error": "enqueue_failed"})
				return
			}
			slog.Info("risk_close_all_enqueued", "request_id", rid, "task_id", t.ID)
			c.JSON(200, gin.H{"ok": true})
		})

		api.GET("/stats/markets", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"bestOddsMatched24h":    gin.H{"poly": 0, "total": 0},
				"bestOddsAllMatched24h": gin.H{"poly": 0, "total": 0},
				"edgeMatched24h":        nil,
				"edgeAllMatched24h":     nil,
			})
		})

		api.GET("/setup/status", func(c *gin.Context) {
			v, ok, _ := d.Store.GetBotConfig(c, "onboardingComplete")
			onboardingDone := ok && strings.TrimSpace(v) == "true"
			_, polyErr := polysession.ResolveAuthedCLOB(c, d.Cfg, d.Store)
			// First boot only: skip wizard if install was finished, or Polymarket is already usable (e.g. env + DB creds).
			needs := !onboardingDone && polyErr != nil
			c.JSON(200, gin.H{
				"needsOnboarding":      needs,
				"proxyConfigured":      d.Cfg.HTTPPlatformProxy != "",
				"polymarketConfigured": polyErr == nil,
			})
		})

		api.POST("/setup/complete", func(c *gin.Context) {
			_ = d.Store.UpsertBotConfig(c, "onboardingComplete", "true")
			snapshotHomeBotSettings(c.Request.Context(), d.Store)
			c.JSON(200, gin.H{"ok": true})
		})
	}

	registerWS(r, d)
	webui.Mount(r)
	return r
}

func snapshotHomeBotSettings(ctx context.Context, st *store.Store) {
	if err := homesettings.SnapshotToFile(ctx, st); err != nil {
		slog.Warn("home_bot_settings_snapshot_failed", "err", err.Error())
	}
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
		"id": p.ID, "title": p.Title, "sideLabel": p.SideLabel,
		"avgEntryCents": p.AvgEntryCents, "currentCents": nil,
		"sizeShares": p.SizeShares, "costUsd": p.CostUSD,
		"highWaterCents": hw, "stopLossPct": p.StopLossPct, "trailingStopCents": trail,
		"valueUsd": nil, "pnlUsd": nil, "maxPayoffUsd": p.SizeShares, "potentialProfitUsd": p.SizeShares - p.CostUSD,
		"status": p.Status, "source": p.Source,
	}
}
