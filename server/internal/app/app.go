package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/easyspace-ai/polysdk/pkg/data"
	"github.com/easyspace-ai/polysdk/pkg/transport"
	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/debounce"
	"github.com/easyspace-ai/polybet/internal/homesettings"
	"github.com/easyspace-ai/polybet/internal/httpserver"
	"github.com/easyspace-ai/polybet/internal/logx"
	"github.com/easyspace-ai/polybet/internal/memcache"
	"github.com/easyspace-ai/polybet/internal/riskruntime"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/service/initsvc"
	"github.com/easyspace-ai/polybet/internal/service/logsvc"
	"github.com/easyspace-ai/polybet/internal/service/marketsvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/stoplossengine"
	"github.com/easyspace-ai/polybet/internal/store"
	marketsync "github.com/easyspace-ai/polybet/internal/sync"
	"github.com/easyspace-ai/polybet/internal/tg"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

// shutdownHTTPTimeout is how long http.Server.Shutdown waits for idle connections.
// shutdownWorkerDrain caps WaitGroup after HTTP stops; in-flight Gamma sync and
// HTTP clients should abort when the root ctx is cancelled (usually a few seconds).
// A stuck SQLite lock or blocking syscall can use the full drain window.
const (
	shutdownHTTPTimeout = 10 * time.Second
	shutdownWorkerDrain = 25 * time.Second
)

type App struct {
	Cfg          *config.Config
	DB           *sql.DB
	Store        *store.Store
	Cache        *bookcache.Cache
	Hub          *wsrelay.Hub
	RiskHub      *wsrelay.Hub
	RiskRuntime  *riskruntime.Bus
	Risk         *risksvc.Service
	SyncEngine   *marketsync.Engine
	SportsCache  *marketsync.SportsCache
	Debounce     *debounce.Debouncer
	Log          *logrus.Logger
	BalanceCache *memcache.BalanceCache
	RiskCache    *memcache.RiskCache
	InitService  *initsvc.Service
	LogService   *logsvc.Service
	StopLoss     *stoplossengine.Engine
	httpSrv      *http.Server
	publicSrv    *http.Server
	wg           sync.WaitGroup
	restartCh    chan struct{}
}

func (a *App) RequestRestart() {
	select {
	case a.restartCh <- struct{}{}:
	default:
	}
}

func New(cfg *config.Config, db *sql.DB, log *logrus.Logger) *App {
	if log == nil {
		log = logrus.StandardLogger()
	}
	st := store.New(db)
	topN := st.GetBotConfigInt(context.Background(), "orderBookLevels", 10)
	cache := bookcache.New(topN)
	hub := wsrelay.NewHub()
	riskHub := wsrelay.NewHub()
	var httpDoer *http.Client
	if cfg.HTTPPlatformProxy != "" {
		if proxyURL, err := url.Parse(cfg.HTTPPlatformProxy); err == nil {
			httpDoer = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
		}
	}
	dataClient := data.NewClient(transport.NewClient(httpDoer, data.BaseURL))
	riskRuntime := riskruntime.NewBus(riskHub, riskruntime.DefaultRingCap)
	risk := risksvc.New(cfg, st, cache, dataClient, log, riskRuntime)
	sportsCache := marketsync.NewSportsCache(cfg.HTTPPlatformProxy, time.Hour)
	syncEng := marketsync.NewEngine(cfg, st, cache, sportsCache, log)

	balanceCache := memcache.NewBalanceCache(st, cfg, log)
	riskCache := memcache.NewRiskCache(log)
	log.Info("进程内缓存(memcache)：余额与风控快照已初始化")
	initSvc := initsvc.New(cfg, st, hub, risk, log)
	logSvc := logsvc.New()

	a := &App{
		Cfg: cfg, DB: db, Store: st, Cache: cache, Hub: hub, RiskHub: riskHub, RiskRuntime: riskRuntime, Risk: risk,
		SyncEngine: syncEng, SportsCache: sportsCache, Debounce: debounce.New(120 * time.Millisecond), Log: log,
		BalanceCache: balanceCache, RiskCache: riskCache, InitService: initSvc,
		LogService: logSvc,
		restartCh:  make(chan struct{}, 1),
	}
	a.StopLoss = stoplossengine.New(cfg, st, cache, risk, a.Debounce, hub, riskHub, riskRuntime, log, func() { a.rebuildAndBroadcastCache() })
	return a
}

func (a *App) Run(ctx context.Context) error {
	if err := a.Store.SeedDefaultConfig(ctx); err != nil {
		return err
	}
	a.Log.Info("启动：默认配置种子数据已就绪")
	if p, err := homesettings.FilePath(); err == nil {
		if err := homesettings.ApplyFromFile(ctx, a.Store); err != nil {
			a.Log.WithFields(logx.Pairs("path", p, "err", err.Error())).Warn("本地机器人设置应用失败")
		} else {
			a.Log.WithFields(logx.Pairs("path", p)).Info("本地机器人设置已按需应用")
		}
	} else {
		a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("本地机器人设置路径解析失败")
	}

	deps := httpserver.Deps{
		Cfg: a.Cfg, DB: a.DB, Store: a.Store, Cache: a.Cache, Hub: a.Hub, RiskHub: a.RiskHub, Risk: a.Risk, Debounce: a.Debounce,
		BalanceCache: a.BalanceCache, RiskCache: a.RiskCache, InitService: a.InitService, LogService: a.LogService,
		SportsCache: a.SportsCache, RiskRuntime: a.RiskRuntime,
		App:         a,
	}
	engine := httpserver.NewRouter(deps)
	a.httpSrv = &http.Server{Addr: a.Cfg.Host + ":" + a.Cfg.Port, Handler: engine, ReadHeaderTimeout: 10 * time.Second}
	if a.Cfg.PublicPort != "" {
		a.publicSrv = &http.Server{Addr: a.Cfg.Host + ":" + a.Cfg.PublicPort, Handler: httpserver.NewPublicRouter(deps), ReadHeaderTimeout: 10 * time.Second}
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.Log.WithFields(logx.Pairs("addr", a.httpSrv.Addr)).Info("HTTP 主端口监听中")
		if err := a.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Log.WithFields(logx.Pairs("err", err)).Error("HTTP 主服务异常退出")
		}
	}()
	if a.publicSrv != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.Log.WithFields(logx.Pairs("addr", a.publicSrv.Addr)).Info("HTTP 公开端口监听中")
			if err := a.publicSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				a.Log.WithFields(logx.Pairs("err", err)).Error("HTTP 公开服务异常退出")
			}
		}()
	}

	// Start init service in background
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.InitService.Run(ctx); err != nil {
			a.Log.WithFields(logx.Pairs("err", err)).Error("初始化服务失败")
		}
		a.Log.Info("初始化服务流程已结束")
	}()

	// Initial Gamma/market sync can take minutes on slow networks. HTTP must
	// listen first so Electron (and ops) can pass /api/health while sync runs.
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		syncCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if err := a.SyncEngine.Once(syncCtx, false); err != nil {
			a.Log.WithFields(logx.Pairs("err", err)).Warn("启动阶段市场同步未完成")
		} else {
			a.Log.Info("启动阶段市场同步成功")
		}
		var sportIcons map[string]string
		if sports, err := a.SportsCache.Get(ctx); err == nil {
			sportIcons = marketsvc.BuildSportIconMap(sports)
		}
		if markets, err := marketsvc.BuildMarketsPayload(ctx, a.Store, a.Cache, sportIcons); err != nil {
			a.Log.WithFields(logx.Pairs("err", err)).Warn("启动阶段市场快照构建失败")
		} else {
			a.Log.WithFields(logx.Pairs("count", len(markets))).Info("启动阶段已向 Dashboard 推送市场快照")
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
	go a.positionsReconcileTicker(ctx)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.StopLoss.Run(ctx)
	}()
	a.wg.Add(1)
	go a.polyUserWSLoop(ctx)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		tg.Run(ctx, a.Cfg, a.Store, a.Log)
	}()
	select {
	case <-ctx.Done():
	case <-a.restartCh:
		a.Log.Info("收到 API 触发的优雅重启请求")
	}
	shCtx, cancel := context.WithTimeout(context.Background(), shutdownHTTPTimeout)
	if err := a.httpSrv.Shutdown(shCtx); err != nil {
		a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("HTTP 主服务 Shutdown 未在超时内完成")
	}
	cancel()
	if a.publicSrv != nil {
		pubCtx, pubCancel := context.WithTimeout(context.Background(), shutdownHTTPTimeout)
		if err := a.publicSrv.Shutdown(pubCtx); err != nil {
			a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("HTTP 公开服务 Shutdown 未在超时内完成")
		}
		pubCancel()
	}

	waitDone := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
		a.Log.Info("优雅关闭：后台任务已全部退出")
	case <-time.After(shutdownWorkerDrain):
		a.Log.WithFields(logx.Pairs(
			"wait", shutdownWorkerDrain.String(),
			"hint", "常见原因：SQLite 写锁、阻塞的网络或忽略 ctx 的第三方调用；main 返回后 runtime 将终止残留协程",
		)).Error("优雅关闭：等待后台任务超时，继续退出（数据库 defer 仍会执行）")
	}
	return nil
}

// sleepCtx waits up to d or returns immediately when ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
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
			_ = a.Risk.ProcessRiskTasksOnce(ctx)
		}
	}
}

func (a *App) SyncAndBroadcastMarkets(ctx context.Context, force bool) error {
	if err := a.SyncEngine.Once(ctx, force); err != nil {
		a.Log.WithFields(logx.Pairs("err", err)).Warn("市场同步失败")
		if a.LogService != nil {
			a.LogService.Error("市场同步", "同步失败: "+err.Error())
		}
		return err
	}
	var sportIcons map[string]string
	if sports, err := a.SportsCache.Get(ctx); err == nil {
		sportIcons = marketsvc.BuildSportIconMap(sports)
	}
	markets, err := marketsvc.BuildMarketsPayload(ctx, a.Store, a.Cache, sportIcons)
	if err != nil {
		a.Log.WithFields(logx.Pairs("err", err)).Warn("市场快照构建失败")
		return err
	}
	a.Log.WithFields(logx.Pairs("markets", len(markets))).Info("市场快照已广播")
	a.Hub.BroadcastJSON(map[string]any{"type": "marketsSnapshot", "data": markets})
	if a.LogService != nil {
		a.LogService.Info("市场同步", fmt.Sprintf("同步完成, 共 %d 个市场", len(markets)))
	}
	return nil
}

func (a *App) syncTicker(ctx context.Context) {
	defer a.wg.Done()
	for {
		// pollingInterval is stored in bot_config as **minutes** (not seconds).
		iv := a.Store.GetBotConfigInt(context.Background(), "pollingInterval", 60)
		if iv < 1 {
			iv = 1
		}
		d := time.Duration(iv) * time.Minute
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
			a.Log.WithFields(logx.Pairs("interval_min", iv)).Info("定时市场同步触发")
			_ = a.SyncAndBroadcastMarkets(ctx, false)
		}
	}
}

func (a *App) scheduleBalanceBroadcast() {
	if a == nil || a.Debounce == nil {
		return
	}
	a.Debounce.Trigger("balance_rebuild", func() {
		summary, err := balancesvc.Fetch(context.Background(), a.Cfg, a.Store)
		if err != nil {
			a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("余额广播：拉取失败")
			return
		}
		_ = a.BalanceCache.Set(context.Background(), summary)
		a.broadcastBalanceUpdateIfChanged(context.Background(), summary)
	})
}

func (a *App) broadcastBalanceUpdateIfChanged(ctx context.Context, summary *balancesvc.Summary) {
	if a == nil || summary == nil || a.BalanceCache == nil {
		return
	}
	acct, _ := a.Store.GetActivePolymarketAccount(ctx)
	aid := ""
	if acct != nil {
		aid = acct.ID
	}
	if !a.BalanceCache.MarkBalanceBroadcastIfChanged(aid, summary) {
		a.Log.Debug("余额广播：摘要未变化，跳过 WS")
		return
	}
	a.Hub.BroadcastJSON(map[string]any{"type": "balance_update", "data": summary})
	a.RiskHub.BroadcastJSON(map[string]any{"type": "balance_update", "data": summary})
}

func (a *App) positionsReconcileNextInterval() time.Duration {
	ctx := context.Background()
	acct, _ := a.Store.GetActivePolymarketAccount(ctx)
	if acct == nil {
		return 60 * time.Second
	}
	minShares := a.Store.GetBotConfigFloat(ctx, "minOpenRiskShares", 1)
	n, err := a.Store.CountOpenRiskPositionsMinShares(ctx, minShares, acct.ID)
	if err != nil || n == 0 {
		return 60 * time.Second
	}
	return 20 * time.Second
}

func (a *App) positionsReconcileTicker(ctx context.Context) {
	defer a.wg.Done()
	var flight sync.Mutex
	for {
		d := a.positionsReconcileNextInterval()
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			if !t.Stop() {
				<-t.C
			}
			return
		case <-t.C:
		}
		if !flight.TryLock() {
			a.Log.Debug("定时持仓对账：上一轮仍在执行，跳过")
			continue
		}
		func() {
			defer flight.Unlock()
			acct, _ := a.Store.GetActivePolymarketAccount(context.Background())
			if acct == nil {
				return
			}
			if err := a.Risk.SyncPositionsFromDataAPI(ctx, acct.ID); err != nil {
				a.Log.WithFields(logx.Pairs("err", err.Error())).Warn("定时持仓对账：Data API 同步失败")
				return
			}
			a.rebuildAndBroadcastCache()
		}()
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
			_ = a.Risk.SyncRiskFromRESTTrades(ctx)
		}
	}
}
