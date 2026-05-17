package httpserver

import (
	"context"
	"database/sql"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/debounce"
	"github.com/easyspace-ai/polybet/internal/memcache"
	"github.com/easyspace-ai/polybet/internal/riskruntime"
	"github.com/easyspace-ai/polybet/internal/service/initsvc"
	"github.com/easyspace-ai/polybet/internal/service/logsvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/store"
	mktSync "github.com/easyspace-ai/polybet/internal/sync"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

// Deps bundles HTTP handler dependencies (no import cycle with app).
type Deps struct {
	Cfg           *config.Config
	DB            *sql.DB
	Store         *store.Store
	Cache         *bookcache.Cache
	Hub           *wsrelay.Hub
	RiskHub       *wsrelay.Hub
	Risk          *risksvc.Service
	Debounce      *debounce.Debouncer
	BalanceCache  *memcache.BalanceCache
	RiskCache     *memcache.RiskCache
	InitService   *initsvc.Service
	LogService    *logsvc.Service
	SportsCache   *mktSync.SportsCache
	RiskRuntime   *riskruntime.Bus
	App           interface {
		ScheduleInvalidateAndRebuildCache()
		ScheduleRiskOfficialRefresh() bool
		ScheduleMarketsRefresh(force bool) bool
		RequestRestart()
		ForceWSReconnect(channel string)
		EnsureOrderbookToken(tokenID string)
		OpenRiskPositionCount(ctx context.Context) int
	}
}
