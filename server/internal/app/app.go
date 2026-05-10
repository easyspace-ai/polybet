package app

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/debounce"
	"github.com/easyspace-ai/polybet/internal/homesettings"
	"github.com/easyspace-ai/polybet/internal/httpserver"
	"github.com/easyspace-ai/polybet/internal/service/marketsvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/store"
	marketsync "github.com/easyspace-ai/polybet/internal/sync"
	"github.com/easyspace-ai/polybet/internal/tg"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

type App struct {
	Cfg        *config.Config
	DB         *sql.DB
	Store      *store.Store
	Cache      *bookcache.Cache
	Hub        *wsrelay.Hub
	Risk       *risksvc.Service
	SyncEngine *marketsync.Engine
	Debounce   *debounce.Debouncer
	Log        *slog.Logger
	httpSrv    *http.Server
	publicSrv  *http.Server
	wg         sync.WaitGroup
}

func New(cfg *config.Config, db *sql.DB, log *slog.Logger) *App {
	if log == nil {
		log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: SlogLevel(cfg.LogLevel)}))
	}
	st := store.New(db)
	topN := st.GetBotConfigInt(context.Background(), "orderBookLevels", 10)
	cache := bookcache.New(topN)
	hub := wsrelay.NewHub()
	risk := risksvc.New(cfg, st, cache, log)
	syncEng := marketsync.NewEngine(cfg, st, cache, log)
	return &App{
		Cfg: cfg, DB: db, Store: st, Cache: cache, Hub: hub, Risk: risk,
		SyncEngine: syncEng, Debounce: debounce.New(120 * time.Millisecond), Log: log,
	}
}

// SlogLevel maps LOG_LEVEL env to slog levels (used by cmd/server).
func SlogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (a *App) Run(ctx context.Context) error {
	if err := a.Store.SeedDefaultConfig(ctx); err != nil {
		return err
	}
	a.Log.Info("boot_seed_config_ok")
	if p, err := homesettings.FilePath(); err == nil {
		if err := homesettings.ApplyFromFile(ctx, a.Store); err != nil {
			a.Log.Warn("home_bot_settings_apply_failed", "path", p, "err", err.Error())
		} else {
			a.Log.Info("home_bot_settings_applied_if_present", "path", p)
		}
		if err := homesettings.SnapshotToFile(ctx, a.Store); err != nil {
			a.Log.Warn("home_bot_settings_snapshot_failed", "path", p, "err", err.Error())
		} else {
			a.Log.Info("home_bot_settings_snapshot_ok", "path", p)
		}
	} else {
		a.Log.Warn("home_bot_settings_path_failed", "err", err.Error())
	}

	deps := httpserver.Deps{
		Cfg: a.Cfg, DB: a.DB, Store: a.Store, Cache: a.Cache, Hub: a.Hub, Risk: a.Risk, Debounce: a.Debounce,
	}
	engine := httpserver.NewRouter(deps)
	a.httpSrv = &http.Server{Addr: a.Cfg.Host + ":" + a.Cfg.Port, Handler: engine, ReadHeaderTimeout: 10 * time.Second}
	if a.Cfg.PublicPort != "" {
		a.publicSrv = &http.Server{Addr: a.Cfg.Host + ":" + a.Cfg.PublicPort, Handler: httpserver.NewPublicRouter(deps), ReadHeaderTimeout: 10 * time.Second}
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.Log.Info("http listen", "addr", a.httpSrv.Addr)
		if err := a.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Log.Error("http", "err", err)
		}
	}()
	if a.publicSrv != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.Log.Info("public http listen", "addr", a.publicSrv.Addr)
			if err := a.publicSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				a.Log.Error("public http", "err", err)
			}
		}()
	}

	// Initial Gamma/market sync can take minutes on slow networks. HTTP must
	// listen first so Electron (and ops) can pass /api/health while sync runs.
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.SyncEngine.Once(ctx); err != nil {
			a.Log.Warn("boot_market_sync", "err", err)
		} else {
			a.Log.Info("boot_market_sync_ok")
		}
		if markets, err := marketsvc.BuildMarketsPayload(ctx, a.Store, a.Cache); err != nil {
			a.Log.Warn("boot_markets_payload", "err", err)
		} else {
			a.Log.Info("boot_markets_snapshot", "count", len(markets))
			a.Hub.BroadcastJSON(map[string]any{"type": "marketsSnapshot", "data": markets})
		}
	}()

	a.wg.Add(1)
	go a.riskTicker(ctx)
	a.wg.Add(1)
	go a.syncTicker(ctx)
	a.wg.Add(1)
	go a.restTradesTicker(ctx)
	a.wg.Add(1)
	go a.polyWSLoop(ctx)
	a.wg.Add(1)
	go a.polyUserWSLoop(ctx)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		tg.Run(ctx, a.Cfg, a.Store, a.Log)
	}()
	<-ctx.Done()
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = a.httpSrv.Shutdown(shCtx)
	if a.publicSrv != nil {
		_ = a.publicSrv.Shutdown(shCtx)
	}
	a.wg.Wait()
	return nil
}

func (a *App) riskTicker(ctx context.Context) {
	defer a.wg.Done()
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = a.Risk.ProcessRiskTasksOnce(context.Background())
		}
	}
}

func (a *App) syncTicker(ctx context.Context) {
	defer a.wg.Done()
	for {
		iv := a.Store.GetBotConfigInt(context.Background(), "pollingInterval", 30)
		if iv < 5 {
			iv = 30
		}
		d := time.Duration(iv) * time.Second
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
			a.Log.Info("market_sync_ticker_fire", "interval_sec", iv)
			if err := a.SyncEngine.Once(context.Background()); err != nil {
				a.Log.Warn("market_sync_ticker_err", "err", err)
			}
			if markets, err := marketsvc.BuildMarketsPayload(context.Background(), a.Store, a.Cache); err != nil {
				a.Log.Warn("market_snapshot_build_err", "err", err)
			} else {
				a.Log.Info("market_snapshot_broadcast", "markets", len(markets))
				a.Hub.BroadcastJSON(map[string]any{"type": "marketsSnapshot", "data": markets})
			}
		}
	}
}

func (a *App) restTradesTicker(ctx context.Context) {
	defer a.wg.Done()
	t := time.NewTicker(45 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = a.Risk.SyncRiskFromRESTTrades(context.Background())
		}
	}
}
