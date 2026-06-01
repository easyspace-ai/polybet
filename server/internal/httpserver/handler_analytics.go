package httpserver

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/easyspace-ai/polybet/internal/storage/badgerdb"
)

func (h *Handler) handleAnalyticsDaily(c *gin.Context) {
	accountID := ""
	if acct, _ := h.st.GetActivePolymarketAccount(c); acct != nil {
		accountID = acct.ID
	}
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))
	rows, err := h.st.ListClosedPositionsForAnalytics(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(500, gin.H{"error": "analytics_daily", "message": err.Error()})
		return
	}
	daily := badgerdb.AggregateAnalyticsDaily(rows, from, to)
	out := make([]gin.H, 0, len(daily))
	for _, r := range daily {
		out = append(out, gin.H{
			"date":             r.Date,
			"totalInvestedUsd": r.TotalInvestedUSD,
			"tradeCount":       r.TradeCount,
			"winCount":         r.WinCount,
			"winRate":          r.WinRate,
			"profitUsd":        r.ProfitUSD,
			"profitAmountRate": r.ProfitAmountRate,
		})
	}
	c.JSON(200, gin.H{
		"accountId": accountID,
		"from":      from,
		"to":        to,
		"rows":      out,
	})
}

func (h *Handler) handleAnalyticsTrades(c *gin.Context) {
	accountID := ""
	if acct, _ := h.st.GetActivePolymarketAccount(c); acct != nil {
		accountID = acct.ID
	}
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))
	result := strings.TrimSpace(c.Query("result"))
	rows, err := h.st.ListClosedPositionsForAnalytics(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(500, gin.H{"error": "analytics_trades", "message": err.Error()})
		return
	}
	filtered, totals := badgerdb.FilterAnalyticsTrades(rows, result, from, to)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}
	total := len(filtered)
	page := badgerdb.PaginateAnalyticsTrades(filtered, offset, limit)
	out := make([]gin.H, 0, len(page))
	for _, r := range page {
		out = append(out, gin.H{
			"positionId":       r.PositionID,
			"title":            r.Title,
			"polyEventSlug":    r.PolyEventSlug,
			"investedUsd":      r.InvestedUSD,
			"profitUsd":        r.RealizedPnLUSD,
			"returnPct":        r.ReturnPct,
			"settlementAt":     formatAnalyticsTime(r.SettlementAt),
			"settlementSource": r.SettlementSource,
			"settlementDate":   r.SettlementDate,
			"pendingOfficial":  r.PendingOfficial,
			"closedAt":         formatAnalyticsTime(r.ClosedAt),
		})
	}
	c.JSON(200, gin.H{
		"accountId": accountID,
		"from":      from,
		"to":        to,
		"result":    result,
		"offset":    offset,
		"limit":     limit,
		"total":     total,
		"rows":      out,
		"totals": gin.H{
			"totalInvestedUsd": totals.TotalInvestedUSD,
			"totalProfitUsd":   totals.TotalProfitUSD,
			"returnPct":        totals.ReturnPct,
			"tradeCount":       totals.TradeCount,
		},
	})
}

func formatAnalyticsTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func (h *Handler) handleAnalyticsFullSync(c *gin.Context) {
	if h.cfg.ReadOnlyMode {
		c.JSON(403, gin.H{"error": "read_only"})
		return
	}
	if h.risk == nil {
		c.JSON(503, gin.H{"error": "analytics_sync_unavailable"})
		return
	}
	stats, err := h.risk.SyncClosedPositionsFullFromDataAPI(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "analytics_full_sync", "message": err.Error()})
		return
	}
	if h.app != nil {
		h.app.ScheduleInvalidateAndRebuildCache()
	}
	if acct, _ := h.st.GetActivePolymarketAccount(c.Request.Context()); acct != nil {
		h.st.InvalidateAnalyticsCache(acct.ID)
	} else {
		h.st.InvalidateAnalyticsCache("")
	}
	c.JSON(200, gin.H{"ok": true, "stats": stats})
}
