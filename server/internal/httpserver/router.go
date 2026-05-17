package httpserver

import (
	"github.com/gin-gonic/gin"

	"github.com/easyspace-ai/polybet/internal/webui"
)

func NewRouter(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestID())
	r.Use(requestStartLog())
	r.Use(accessLog())
	r.Use(cors(d.Cfg.CORSOrigins))

	h := NewHandler(d)

	if d.Cfg != nil && d.Cfg.EnablePprof {
		registerPprof(r)
	}

	r.GET("/api/health", h.handleHealth)

	api := r.Group("/api")
	{
		// Markets
		api.GET("/markets", h.handleMarkets)
		api.GET("/sports", h.handleSports)
		api.POST("/markets/refresh", h.handleMarketsRefresh)
		api.POST("/markets/refresh-full", h.handleMarketsRefreshFull)

		// Trades
		api.GET("/trade/orderbook", h.handleOrderbook)
		api.GET("/trade/preview", h.handleTradePreview)
		// POST /api/trade goes through the trade-gate middleware so manual
		// halt / kill switch / WS-down / exposure caps short-circuit before
		// the handler does any work. Per-token checks (book staleness,
		// post-kickoff) still run inside tradesvc.ExecutePlan once the
		// allocation plan resolves the per-leg tokenID.
		api.POST("/trade", tradeGateMiddleware(h.risk), h.handleTradeExecute)
		api.GET("/trades", h.handleTradesList)

		// Config
		api.GET("/config", h.handleConfig)
		api.PUT("/config/:key", h.handleUpdateConfig)

		// Telegram
		api.POST("/telegram/test", h.handleTelegramTest)

		// Balances
		api.GET("/balances", h.handleBalances)

		// Polymarket accounts
		api.GET("/polymarket/accounts", h.handleListAccounts)
		api.POST("/polymarket/accounts", h.handleCreateAccount)
		api.PATCH("/polymarket/accounts/:id/activate", h.handleActivateAccount)
		api.DELETE("/polymarket/accounts/:id", h.handleDeleteAccount)

		// Risk
		api.GET("/risk/positions", h.handleRiskPositions)
		api.POST("/risk/refresh", h.handleRiskRefresh)
		api.GET("/risk/tasks", h.handleRiskTasks)
		api.POST("/risk/tasks/clear", h.handleRiskTasksClear)
		api.GET("/risk/runtime-logs", h.handleRiskRuntimeLogs)
		api.GET("/risk/stop-loss-history", h.handleStopLossHistory)
		api.GET("/risk/trade-history", h.handleTradeHistory)
		api.PATCH("/risk/positions/:id", h.handlePatchRiskPosition)
		api.POST("/risk/positions/:id/close", h.handleClosePosition)
		api.POST("/risk/close-all", h.handleCloseAll)
		api.GET("/risk/hidden-positions", h.handleRiskHiddenList)
		api.POST("/risk/hidden-positions", h.handleRiskHiddenPost)
		api.DELETE("/risk/hidden-positions", h.handleRiskHiddenDelete)

		// Trade gate / kill switch
		api.GET("/risk/gate", h.handleRiskGate)
		api.POST("/risk/kill-switch/clear", h.handleRiskKillSwitchClear)

		// Execution-quality / slippage telemetry
		api.GET("/trade-quality/recent", h.handleTradeQualityRecent)
		api.GET("/trade-quality/aggregate", h.handleTradeQualityAggregate)
		api.GET("/risk/realized-pnl-by-event", h.handleRealizedPnLByEvent)

		// Stats
		api.GET("/stats/markets", h.handleStatsMarkets)

		// Setup
		api.GET("/setup/status", h.handleSetupStatus)
		api.GET("/setup/init-status", h.handleInitStatus)
		api.POST("/setup/complete", h.handleSetupComplete)

		// Logs
		api.GET("/logs", h.handleLogs)
		api.GET("/logs/errors", h.handleLogErrors)
		api.POST("/logs/clear", h.handleLogClear)

		// Status
		api.GET("/status", h.handleStatus)
		api.GET("/ws/status", h.handleWSStatus)
		api.POST("/ws/reconnect", h.handleWSReconnect)

		// Cache & restart
		api.POST("/cache/refresh", h.handleCacheRefresh)
		api.POST("/restart", h.handleRestart)
	}

	registerWS(r, d)
	r.GET("/ws/risk", h.handleWSRisk)
	webui.Mount(r)
	return r
}
