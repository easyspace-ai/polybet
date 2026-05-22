package httpserver

import (
	"context"
	"time"

	appconn "github.com/easyspace-ai/polybet/internal/application/connectivity"
	appmonitor "github.com/easyspace-ai/polybet/internal/application/monitor"
	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/debounce"
	"github.com/easyspace-ai/polybet/internal/memcache"
	"github.com/easyspace-ai/polybet/internal/riskruntime"
	"github.com/easyspace-ai/polybet/internal/service/initsvc"
	"github.com/easyspace-ai/polybet/internal/service/logsvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/storage"
	mktSync "github.com/easyspace-ai/polybet/internal/sync"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

// Deps bundles HTTP handler dependencies (no import cycle with app).
type Deps struct {
	Cfg          *config.Config
	Store        *storage.Backend
	Cache        *bookcache.Cache
	Hub          *wsrelay.Hub
	RiskHub      *wsrelay.Hub
	Risk         *risksvc.Service
	Debounce     *debounce.Debouncer
	BalanceCache *memcache.BalanceCache
	RiskCache    *memcache.RiskCache
	InitService  *initsvc.Service
	LogService   *logsvc.Service
	SportsCache  *mktSync.SportsCache
	RiskRuntime  *riskruntime.Bus
	Conn         *appconn.Service
	Monitor      *appmonitor.Service
	App          interface {
		ScheduleInvalidateAndRebuildCache()
		ScheduleRiskOfficialRefresh() bool
		ScheduleMarketsFullRefresh() bool
		ScheduleMarketsRefresh(force bool) bool
		RefreshMarketsBlocking(context.Context, bool) error
		ResetMarketsBlocking(context.Context) error
		ResetAllAppDataBlocking(context.Context) (int, error)
		RequestRestart()
		ForceWSReconnect(channel string) bool
		EnsureOrderbookToken(tokenID string)
		PolyBookClientSubscribe(tokenID string)
		PolyBookClientUnsubscribe(tokenID string)
		PublishBookSummaryTick(tokenID string)
		PolyBookSubStatusesFor(tokenIDs []string) []map[string]any
		NotifyRiskPositionsChanged()
		OpenRiskPositionCount(ctx context.Context) int
		CachedOpenRiskPositionCount(maxAge time.Duration) (int, bool)
	}
}
