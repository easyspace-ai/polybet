package httpserver

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/easyspace-ai/polybet/internal/autoorder"
)

func (h *Handler) handleAutoOrderConfigGet(c *gin.Context) {
	resp, err := autoorder.LoadConfigResponse(c.Request.Context(), h.st, h.cfg.ReadOnlyMode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "auto_order_config", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) handleAutoOrderConfigPut(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(http.StatusForbidden, gin.H{"error": "read_only"})
		return
	}
	var req autoorder.SaveConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}
	if err := autoorder.ApplySaveRequest(c.Request.Context(), h.st, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation_failed", "message": err.Error()})
		return
	}
	resp, err := autoorder.LoadConfigResponse(c.Request.Context(), h.st, h.cfg.ReadOnlyMode)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) handleAutoOrderRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	runs, err := autoorder.ListRecentRuns(c.Request.Context(), h.st, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "auto_order_runs", "message": err.Error()})
		return
	}
	if runs == nil {
		runs = []autoorder.RunRecord{}
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

func (h *Handler) handleTeams(c *gin.Context) {
	league := strings.ToLower(strings.TrimSpace(c.Query("league")))
	if league == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "league_required"})
		return
	}
	if h.teamsCache == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "teams_cache_unconfigured"})
		return
	}
	teams, err := h.teamsCache.Get(c.Request.Context(), league)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "teams_fetch_failed", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, teams)
}
