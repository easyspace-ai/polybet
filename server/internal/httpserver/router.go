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
	r.Use(cors(d.Cfg.CORSOrigins))

	h := NewHandler(d)

	r.GET("/api/health", h.handleHealth)

	api := r.Group("/api")
	{
		// Markets
		api.GET("/markets", h.handleMarkets)
		api.GET("/sports", h.handleSports)
		api.POST("/markets/refresh", h.handleMarketsRefresh)

		// Trades
		api.GET("/trade/orderbook", h.handleOrderbook)
		api.GET("/trade/preview", h.handleTradePreview)
		api.POST("/trade", h.handleTradeExecute)
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
		api.GET("/risk/stop-loss-history", h.handleStopLossHistory)
		api.GET("/risk/trade-history", h.handleTradeHistory)
		api.PATCH("/risk/positions/:id", h.handlePatchRiskPosition)
		api.POST("/risk/positions/:id/close", h.handleClosePosition)
		api.POST("/risk/close-all", h.handleCloseAll)

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

		// Cache & restart
		api.POST("/cache/refresh", h.handleCacheRefresh)
		api.POST("/restart", h.handleRestart)
	}

	registerWS(r, d)
	webui.Mount(r)
	return r
}
