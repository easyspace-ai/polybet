package httpserver

import (
	"context"
	"database/sql"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/debounce"
	"github.com/easyspace-ai/polybet/internal/rediska"
	"github.com/easyspace-ai/polybet/internal/service/initsvc"
	"github.com/easyspace-ai/polybet/internal/service/logsvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

// Deps bundles HTTP handler dependencies (no import cycle with app).
type Deps struct {
	Cfg           *config.Config
	DB            *sql.DB
	Store         *store.Store
	Cache         *bookcache.Cache
	Hub           *wsrelay.Hub
	Risk          *risksvc.Service
	Debounce      *debounce.Debouncer
	BalanceCache  *rediska.BalanceCache
	RiskCache     *rediska.RiskCache
	InitService   *initsvc.Service
	LogService    *logsvc.Service
	App           interface {
		InvalidateAndRebuildCache()
		SyncAndBroadcastMarkets(ctx context.Context) error
	}
}
