package httpserver

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
)

// tradeGateMiddleware short-circuits any HTTP route that opens new positions
// when the global risk gate is closed (manual halt, kill switch, WS market
// down, open-position cap, account/market exposure caps). Per-token checks
// (book staleness, post-kickoff) require the resolved CLOB token id and
// stay inside tradesvc.ExecutePlan because the router resolves the token
// only after planning.
//
// Returns 409 Conflict so clients can distinguish "gate temporarily closed"
// from genuine 4xx (bad request) and 5xx (server error). The body carries
// the same shape as TradeGateError for telemetry consumers.
func tradeGateMiddleware(risk *risksvc.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if risk == nil {
			c.Next()
			return
		}
		gate := risk.EnsureTradeAllowed(c, "")
		if gate == nil {
			c.Next()
			return
		}
		rid := c.GetString("request_id")
		fields := logx.Pairs(
			"request_id", rid,
			"path", c.Request.URL.Path,
			"gate_code", gate.Code,
			"gate_message", gate.Message,
			"detail", gate.Detail,
		)
		logrus.WithFields(fields).Warn("交易门控：拒绝（middleware 短路）")
		logx.Trade().WithFields(fields).Warn("交易门控：拒绝（middleware 短路）")
		c.AbortWithStatusJSON(409, gin.H{
			"error":   gate.Code,
			"message": gate.Message,
			"detail":  gate.Detail,
		})
	}
}
