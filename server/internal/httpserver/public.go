package httpserver

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/easyspace-ai/polybet/internal/service/marketsvc"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/service/routersvc"
)

// NewPublicRouter exposes read-only endpoints on PUBLIC_PORT (no trade POST, no mutations).
func NewPublicRouter(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestID())
	r.Use(cors(d.Cfg.CORSOrigins))

	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "db": "public"})
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
				slog.Warn("trade_preview_failed_public",
					"request_id", rid, "outcome_id", oid, "side", side, "size", size,
					"router_code", res.Error.Code, "router_message", res.Error.Message, "detail", res.Error.Detail, "http_status", st)
				c.JSON(st, gin.H{"error": res.Error.Code, "message": res.Error.Message, "detail": res.Error.Detail})
				return
			}
			c.JSON(200, res.Plan)
		})
		api.GET("/config", func(c *gin.Context) {
			rows, err := d.Store.ListBotConfig(c)
			if err != nil {
				c.JSON(500, gin.H{"error": "config"})
				return
			}
			out := make([]gin.H, 0, len(rows))
			for _, row := range rows {
				out = append(out, gin.H{"key": row.Key, "value": row.Value})
			}
			c.JSON(200, out)
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
		api.GET("/setup/status", func(c *gin.Context) {
			v, ok, _ := d.Store.GetBotConfig(c, "onboardingComplete")
			onboardingDone := ok && strings.TrimSpace(v) == "true"
			_, polyErr := polysession.ResolveAuthedCLOB(c, d.Cfg, d.Store)
			needs := !onboardingDone && polyErr != nil
			c.JSON(200, gin.H{
				"needsOnboarding":      needs,
				"proxyConfigured":      d.Cfg.HTTPPlatformProxy != "",
				"polymarketConfigured": polyErr == nil,
			})
		})
	}
	return r
}
