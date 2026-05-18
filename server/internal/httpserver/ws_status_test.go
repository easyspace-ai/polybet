package httpserver

import (
	"context"
	"testing"
	"time"

	"github.com/easyspace-ai/polybet/internal/memcache"
)

func TestWSStatusSnap_getSetTTL(t *testing.T) {
	snap := &wsStatusSnap{}
	snap.set(map[string]any{"dashClients": 2})

	got, ok := snap.get()
	if !ok || got["dashClients"] != 2 {
		t.Fatalf("expected cached dashClients=2, got=%v ok=%v", got, ok)
	}

	snap.mu.Lock()
	snap.at = time.Now().Add(-wsStatusCacheTTL - time.Millisecond)
	snap.mu.Unlock()

	if _, ok := snap.get(); ok {
		t.Fatal("expected cache miss after TTL")
	}
}

func TestWSStatusSnap_missWhileConnecting(t *testing.T) {
	snap := &wsStatusSnap{}
	snap.set(map[string]any{
		"polyUserConnected":       false,
		"polyUserConnecting":      true,
		"polyOrderbookConnected":  false,
		"polyOrderbookConnecting": false,
	})
	if _, ok := snap.get(); ok {
		t.Fatal("expected cache miss while user upstream is connecting")
	}
}

func TestWSStatusSnap_invalidate(t *testing.T) {
	snap := &wsStatusSnap{}
	snap.set(map[string]any{"dashClients": 1})
	snap.invalidate()
	if _, ok := snap.get(); ok {
		t.Fatal("expected cache miss after invalidate")
	}
}

func TestOpenRiskPositionCountFromCache(t *testing.T) {
	rc := memcache.NewRiskCache(nil)
	_ = rc.Set(t.Context(), memcache.RiskFetchResult{
		Positions: []map[string]any{
			{"status": "open", "sizeShares": 5.0},
			{"status": "open", "sizeShares": 0.5},
			{"status": "closed", "sizeShares": 10.0},
		},
		Meta: memcache.RiskMeta{MinOpenRiskShares: 1},
	})

	n, ok := openRiskPositionCountFromCache(rc)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if n != 1 {
		t.Fatalf("open count=%d want 1", n)
	}
}

func TestOpenRiskPositionCountFromCache_minShares(t *testing.T) {
	rc := memcache.NewRiskCache(nil)
	_ = rc.Set(t.Context(), memcache.RiskFetchResult{
		Positions: []map[string]any{
			{"status": "open", "sizeShares": 5.0},
			{"status": "open", "sizeShares": 0.5},
		},
		Meta: memcache.RiskMeta{MinOpenRiskShares: 2},
	})

	n, ok := openRiskPositionCountFromCache(rc)
	if !ok || n != 1 {
		t.Fatalf("open count=%d ok=%v want 1 true", n, ok)
	}
}

type cachedOpenPosApp struct{}

func (cachedOpenPosApp) ScheduleInvalidateAndRebuildCache()             {}
func (cachedOpenPosApp) ScheduleRiskOfficialRefresh() bool                { return false }
func (cachedOpenPosApp) ScheduleMarketsFullRefresh() bool                 { return false }
func (cachedOpenPosApp) ScheduleMarketsRefresh(force bool) bool           { return false }
func (cachedOpenPosApp) RequestRestart()                                  {}
func (cachedOpenPosApp) ForceWSReconnect(channel string)                  {}
func (cachedOpenPosApp) EnsureOrderbookToken(tokenID string)              {}
func (cachedOpenPosApp) PolyBookClientSubscribe(tokenID string)           {}
func (cachedOpenPosApp) PolyBookClientUnsubscribe(tokenID string)         {}
func (cachedOpenPosApp) PublishBookSummaryTick(tokenID string)            {}
func (cachedOpenPosApp) PolyBookSubStatusesFor(tokenIDs []string) []map[string]any {
	return nil
}
func (cachedOpenPosApp) NotifyRiskPositionsChanged()                      {}
func (cachedOpenPosApp) OpenRiskPositionCount(context.Context) int        { return 0 }
func (cachedOpenPosApp) CachedOpenRiskPositionCount(time.Duration) (int, bool) {
	return 3, true
}

func TestOpenRiskPositionCount_prefersAppCache(t *testing.T) {
	h := &Handler{app: cachedOpenPosApp{}}
	if got := h.openRiskPositionCount(context.Background()); got != 3 {
		t.Fatalf("open count=%d want 3", got)
	}
}

func TestBuildWSStatusJSON_nilRisk(t *testing.T) {
	out := buildWSStatusJSON(nil, nil, 3, 4)
	if out["dashClients"] != 3 || out["openPositionsCount"] != 4 {
		t.Fatalf("unexpected payload: %v", out)
	}
	if out["dashConnected"] != true {
		t.Fatal("expected dashConnected true")
	}
}
