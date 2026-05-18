package httpserver

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/polyexec"
	"github.com/easyspace-ai/polybet/internal/store"
)

const (
	riskBookRESTMinInterval = 3 * time.Second
)

type riskBookPullGate struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (g *riskBookPullGate) allow(tokenID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == nil {
		g.last = make(map[string]time.Time)
	}
	now := time.Now()
	if prev, ok := g.last[tokenID]; ok && now.Sub(prev) < riskBookRESTMinInterval {
		return false
	}
	g.last[tokenID] = now
	return true
}

var dashboardRiskBookPullGate riskBookPullGate

func bookLevelRows(levels []bookcache.Level) []gin.H {
	out := make([]gin.H, 0, len(levels))
	for _, L := range levels {
		out = append(out, gin.H{
			"odds":     L.Odds,
			"size":     L.Size,
			"platform": L.Platform,
		})
	}
	return out
}

func buildRiskBookJSON(cache *bookcache.Cache, tokenID string, source string) gin.H {
	bids, asks := cache.GetBidsAsks(tokenID, 5)
	bestBid, bestAsk, _ := cache.TopOfBook(tokenID)
	bidCents := 0.0
	askCents := 0.0
	if bestBid > 0 {
		bidCents = polyexec.CentsFromPrice01(bestBid)
	}
	if bestAsk > 0 {
		askCents = polyexec.CentsFromPrice01(bestAsk)
	}
	updatedAtMs := cache.BookUpdatedAtMs(tokenID)
	bookAgeMs := int64(-1)
	if age, ok := cache.BookAge(tokenID); ok {
		bookAgeMs = age.Milliseconds()
	}
	return gin.H{
		"tokenId":     tokenID,
		"bids":        bookLevelRows(bids),
		"asks":        bookLevelRows(asks),
		"bestBid":     bidCents,
		"bestAsk":     askCents,
		"source":      source,
		"updatedAtMs": updatedAtMs,
		"bookAgeMs":   bookAgeMs,
	}
}

func (h *Handler) publishRiskBookRESTLog(tokenID, source, reason string, bidCents, askCents float64) {
	if h == nil || h.riskRuntime == nil || source != "rest" {
		return
	}
	detail := map[string]any{
		"source":        source,
		"reason":        reason,
		"bestBid":       polyexec.FloorCents1(bidCents),
		"bestAsk":       polyexec.FloorCents1(askCents),
		"schemaVersion": 1,
	}
	h.riskRuntime.Publish("market_data", "info", "market.book.rest_refresh", "", "", tokenID, "", detail)
}

// handleRiskBook returns cached or REST-refreshed order book for one CLOB token.
// GET /api/risk/book?tokenId=...&refresh=1
func (h *Handler) handleRiskBook(c *gin.Context) {
	raw := c.Query("tokenId")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tokenId required"})
		return
	}
	tid := store.NormalizeRiskCLOBTokenID(raw)
	if tid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tokenId"})
		return
	}

	force := c.Query("refresh") == "1" || c.Query("force") == "1"
	reason := c.DefaultQuery("reason", "dashboard")
	source := "cache"
	needsREST := force || bookCacheNeedsRESTWarm(h.cache, tid)

	if needsREST {
		if !dashboardRiskBookPullGate.allow(tid) {
			source = "cache"
		} else {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			src, ageMs, bb, ba := warmBookCacheFromREST(ctx, h.cfg, h.cache, tid)
			cancel()
			source = src
			if src == "rest" {
				fields := logx.Pairs("token_id", tid, "reason", reason, "book_age_ms", ageMs, "best_bid", bb, "best_ask", ba)
				logrus.WithFields(fields).Info("风控盘口：REST 兜底刷新")
				h.publishRiskBookRESTLog(tid, "rest", reason, polyexec.CentsFromPrice01(bb), polyexec.CentsFromPrice01(ba))
			}
		}
	}

	out := buildRiskBookJSON(h.cache, tid, source)
	if h.app != nil {
		subs := h.app.PolyBookSubStatusesFor([]string{tid})
		if len(subs) > 0 {
			out["subscription"] = subs[0]
		}
	}
	c.JSON(http.StatusOK, out)
}
