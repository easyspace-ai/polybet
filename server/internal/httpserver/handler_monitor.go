package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"

	appmonitor "github.com/easyspace-ai/polybet/internal/application/monitor"
)

func (h *Handler) handleMonitorClobSession(c *gin.Context) {
	if h.monitor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "monitor_unavailable"})
		return
	}
	if !appmonitor.AllowClobSessionRequest(c.ClientIP()) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}
	sess, err := h.monitor.ClobSession(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sess)
}

func (h *Handler) handleMonitorHeartbeat(c *gin.Context) {
	if h.monitor == nil || h.conn == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "monitor_unavailable"})
		return
	}
	var body appmonitor.HeartbeatInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	h.monitor.Heartbeat(body)
	if h.hub != nil {
		h.conn.SetRelayClients(h.hub.ClientCount())
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) handleMonitorStopLossTrigger(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(http.StatusForbidden, gin.H{"error": "read_only"})
		return
	}
	if h.monitor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "monitor_unavailable"})
		return
	}
	var body appmonitor.StopLossTriggerInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	out, err := h.monitor.TriggerStopLoss(c.Request.Context(), body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) handleMonitorProfitProtectEvaluate(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(http.StatusForbidden, gin.H{"error": "read_only"})
		return
	}
	if h.monitor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "monitor_unavailable"})
		return
	}
	var body appmonitor.ProfitProtectEvaluateInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	out, err := h.monitor.EvaluateProfitProtect(c.Request.Context(), body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) handleMonitorPositionsSync(c *gin.Context) {
	if h.monitor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "monitor_unavailable"})
		return
	}
	if err := h.monitor.SyncPositions(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if h.app != nil {
		h.app.ScheduleInvalidateAndRebuildCache()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
