package httpserver

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/memcache"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
)

const (
	wsStatusCacheTTL         = 5 * time.Second
	wsStatusRefreshInterval  = 5 * time.Second
	wsStatusOpenCountTimeout = 200 * time.Millisecond
	wsStatusBuildTimeout     = 500 * time.Millisecond
	wsStatusOpenCountStale   = 30 * time.Second
)

type wsStatusSnap struct {
	mu   sync.RWMutex
	data map[string]any
	at   time.Time
	sf   singleflight.Group
}

func (s *wsStatusSnap) get() (map[string]any, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data == nil || time.Since(s.at) > wsStatusCacheTTL {
		return nil, false
	}
	out := make(map[string]any, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, true
}

func (s *wsStatusSnap) getStale() (map[string]any, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data == nil || time.Since(s.at) > wsStatusCacheTTL*4 {
		return nil, false
	}
	out := make(map[string]any, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, true
}

func (s *wsStatusSnap) invalidate() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.data = nil
	s.at = time.Time{}
	s.mu.Unlock()
}

func (s *wsStatusSnap) set(data map[string]any) {
	if s == nil || data == nil {
		return
	}
	cp := make(map[string]any, len(data))
	for k, v := range data {
		cp[k] = v
	}
	s.mu.Lock()
	s.data = cp
	s.at = time.Now()
	s.mu.Unlock()
}

func buildWSStatusJSON(risk *risksvc.Service, cache *bookcache.Cache, dashClients int, openPositions int) map[string]any {
	out := map[string]any{
		"dashConnected":           dashClients > 0,
		"dashClients":             dashClients,
		"polyOrderbookConnected":  false,
		"polyOrderbookConnecting": false,
		"polyUserConnected":       false,
		"polyUserConnecting":      false,
		"openPositionsCount":      openPositions,
	}
	if risk == nil {
		return out
	}
	out["polyOrderbookConnected"] = risk.OrderbookWSConnected()
	out["polyOrderbookConnecting"] = risk.OrderbookWSConnecting()
	out["polyUserConnected"] = risk.UserWSConnected()
	out["polyUserConnecting"] = risk.UserWSConnecting()
	issue := ""
	risk.UserWSLastIssue(&issue)
	if risk.WSMeta != nil {
		ex := risk.WSMeta.Snapshot(issue)
		out["orderbookReconnectAttempt"] = ex.OrderbookReconnectAttempt
		out["userReconnectAttempt"] = ex.UserReconnectAttempt
		if ex.OrderbookNextRetryAt != nil {
			out["orderbookNextRetryAt"] = *ex.OrderbookNextRetryAt
		}
		if ex.UserNextRetryAt != nil {
			out["userNextRetryAt"] = *ex.UserNextRetryAt
		}
		if ex.UserWsLastIssue != "" {
			out["userWsLastIssue"] = ex.UserWsLastIssue
		}
		if len(ex.WSEvents) > 0 {
			out["wsEvents"] = ex.WSEvents
		}
	}
	if cache != nil {
		if ms := cache.LastBookUpdateMs(); ms > 0 {
			out["lastBookUpdateMs"] = ms
		}
	}
	return out
}

func openRiskPositionCountFromCache(rc *memcache.RiskCache) (int, bool) {
	if rc == nil {
		return 0, false
	}
	positions, meta, found := rc.Snapshot()
	if !found {
		return 0, false
	}
	minShares := meta.MinOpenRiskShares
	if minShares <= 0 {
		minShares = 1
	}
	n := 0
	for _, row := range positions {
		status, _ := row["status"].(string)
		if status != "open" {
			continue
		}
		if riskRowFloat(row, "sizeShares") >= minShares {
			n++
		}
	}
	return n, true
}

func riskRowFloat(row map[string]any, key string) float64 {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case jsonNumber:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}

func (h *Handler) openRiskPositionCount(ctx context.Context) int {
	if n, ok := openRiskPositionCountFromCache(h.riskCache); ok {
		return n
	}
	if h.app == nil {
		return 0
	}
	if n, ok := h.app.CachedOpenRiskPositionCount(wsStatusOpenCountStale); ok {
		return n
	}
	countCtx, cancel := context.WithTimeout(ctx, wsStatusOpenCountTimeout)
	defer cancel()
	n := h.app.OpenRiskPositionCount(countCtx)
	if n > 0 {
		return n
	}
	if stale, ok := h.app.CachedOpenRiskPositionCount(wsStatusOpenCountStale); ok {
		return stale
	}
	return 0
}

func (h *Handler) buildWSStatus(ctx context.Context) map[string]any {
	start := time.Now()
	hubSize, openN := 0, 0

	if h.hub != nil {
		hubSize = h.hub.ClientCount()
	}
	openN = h.openRiskPositionCount(ctx)

	out := buildWSStatusJSON(h.risk, h.cache, hubSize, openN)
	logrus.WithFields(logx.Pairs(
		"duration_ms", time.Since(start).Milliseconds(),
		"open_positions", openN,
		"dash_clients", hubSize,
	)).Debug("ws status snapshot built")
	return out
}

// refreshWSStatusSnap builds a new snapshot and stores it.  It is safe to call
// concurrently; singleflight deduplicates overlapping refreshes.
func (h *Handler) refreshWSStatusSnap() {
	if h.wsStatusSnap == nil {
		return
	}
	_, _, _ = h.wsStatusSnap.sf.Do("ws_status_refresh", func() (any, error) {
		buildCtx, cancel := context.WithTimeout(context.Background(), wsStatusBuildTimeout)
		defer cancel()
		out := h.buildWSStatus(buildCtx)
		h.wsStatusSnap.set(out)
		return nil, nil
	})
}

// wsStatusResponse always returns immediately from the cached snapshot.
// The cache is kept warm by a background goroutine (startWSStatusRefresher).
// If the cache is cold we fall back to stale data or a zero-value skeleton.
func (h *Handler) wsStatusResponse(_ context.Context) map[string]any {
	if h.wsStatusSnap == nil {
		return buildWSStatusJSON(h.risk, h.cache, 0, 0)
	}
	if snap, ok := h.wsStatusSnap.get(); ok {
		logrus.WithFields(logx.Pairs("cache", "hit")).Debug("ws status snapshot")
		return snap
	}
	if snap, ok := h.wsStatusSnap.getStale(); ok {
		logrus.WithFields(logx.Pairs("cache", "stale-fallback")).Debug("ws status snapshot")
		return snap
	}
	return buildWSStatusJSON(h.risk, h.cache, 0, 0)
}

// startWSStatusRefresher launches a background goroutine that rebuilds the
// ws/status snapshot every interval.  This keeps the API handler path O(1).
// Call the returned stop function on shutdown (optional).
func startWSStatusRefresher(h *Handler, interval time.Duration) func() {
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				h.refreshWSStatusSnap()
			}
		}
	}()
	return func() { close(stopCh) }
}
