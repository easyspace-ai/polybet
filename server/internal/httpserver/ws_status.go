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
	wsStatusCacheTTL         = 1500 * time.Millisecond
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
	// Never serve cached snapshots while upstream is mid-connect — REST clients
	// would otherwise stick on "reconnecting" after the live WS already connected.
	if connecting, ok := s.data["polyOrderbookConnecting"].(bool); ok && connecting {
		return nil, false
	}
	if connecting, ok := s.data["polyUserConnecting"].(bool); ok && connecting {
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
	countCtx, cancel := context.WithTimeout(context.Background(), wsStatusOpenCountTimeout)
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
	var hubSize, openN int

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if h.hub != nil {
				hubSize = h.hub.ClientCount()
			}
		}()
		go func() {
			defer wg.Done()
			openN = h.openRiskPositionCount(ctx)
		}()
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	case <-time.After(wsStatusBuildTimeout):
	}

	out := buildWSStatusJSON(h.risk, h.cache, hubSize, openN)
	logrus.WithFields(logx.Pairs(
		"duration_ms", time.Since(start).Milliseconds(),
		"open_positions", openN,
		"dash_clients", hubSize,
	)).Debug("ws status snapshot built")
	return out
}

func (h *Handler) wsStatusResponse(ctx context.Context) map[string]any {
	if h.wsStatusSnap == nil {
		h.wsStatusSnap = &wsStatusSnap{}
	}
	if snap, ok := h.wsStatusSnap.get(); ok {
		logrus.WithFields(logx.Pairs("cache", "hit")).Debug("ws status snapshot")
		return snap
	}
	v, _, _ := h.wsStatusSnap.sf.Do("ws_status", func() (any, error) {
		if snap, ok := h.wsStatusSnap.get(); ok {
			return snap, nil
		}
		buildCtx, cancel := context.WithTimeout(context.Background(), wsStatusBuildTimeout)
		defer cancel()
		out := h.buildWSStatus(buildCtx)
		h.wsStatusSnap.set(out)
		return out, nil
	})
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return h.buildWSStatus(ctx)
}
