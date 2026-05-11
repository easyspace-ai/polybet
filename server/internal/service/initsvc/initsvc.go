package initsvc

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/service/balancesvc"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/store"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

type StepStatus struct {
	Status   string      `json:"status"`
	Error    string      `json:"error,omitempty"`
	Details  interface{} `json:"details,omitempty"`
}

type InitStatus struct {
	ConfigCheck    StepStatus `json:"configCheck"`
	ProxyCheck     StepStatus `json:"proxyCheck"`
	BalanceCache   StepStatus `json:"balanceCache"`
	PositionCache  StepStatus `json:"positionCache"`
	Complete       bool       `json:"complete"`
}

type ProxyDetails struct {
	Blocked  bool   `json:"blocked"`
	IP       string `json:"ip,omitempty"`
	Country  string `json:"country,omitempty"`
	Region   string `json:"region,omitempty"`
}

type CacheDetails struct {
	Count int `json:"count"`
}

type Service struct {
	cfg  *config.Config
	st   *store.Store
	hub  *wsrelay.Hub
	log  *slog.Logger
	risk *risksvc.Service

	mu     sync.RWMutex
	status InitStatus
}

func New(cfg *config.Config, st *store.Store, hub *wsrelay.Hub, risk *risksvc.Service, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		cfg:  cfg,
		st:   st,
		hub:  hub,
		risk: risk,
		log:  log,
		status: InitStatus{
			ConfigCheck:   StepStatus{Status: "pending"},
			ProxyCheck:    StepStatus{Status: "pending"},
			BalanceCache:  StepStatus{Status: "pending"},
			PositionCache: StepStatus{Status: "pending"},
			Complete:      false,
		},
	}
}

func (s *Service) GetStatus() InitStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Service) updateStatus(field string, status StepStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch field {
	case "config":
		s.status.ConfigCheck = status
	case "proxy":
		s.status.ProxyCheck = status
	case "balance":
		s.status.BalanceCache = status
	case "position":
		s.status.PositionCache = status
	}
}

func (s *Service) setComplete(complete bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Complete = complete
}

func (s *Service) Run(ctx context.Context) error {
	s.log.Info("init_service_start")

	if err := s.checkConfig(ctx); err != nil {
		s.log.Error("init_config_check_failed", "err", err)
		return err
	}

	if err := s.checkProxy(ctx); err != nil {
		s.log.Error("init_proxy_check_failed", "err", err)
	}

	if err := s.cacheBalances(ctx); err != nil {
		s.log.Error("init_balance_cache_failed", "err", err)
	}

	if err := s.cachePositions(ctx); err != nil {
		s.log.Error("init_position_cache_failed", "err", err)
	}

	s.setComplete(true)
	s.log.Info("init_service_complete")
	return nil
}

func (s *Service) checkConfig(ctx context.Context) error {
	s.updateStatus("config", StepStatus{Status: "loading"})

	cfg := s.cfg
	hasError := false
	var errMsg string

	if cfg.PolymarketAPIURL == "" {
		hasError = true
		errMsg = "POLYMARKET_API_URL not configured"
	} else if cfg.DatabaseURL == "" {
		hasError = true
		errMsg = "DATABASE_URL not configured"
	}

	if hasError {
		s.updateStatus("config", StepStatus{Status: "error", Error: errMsg})
		return nil
	}

	s.updateStatus("config", StepStatus{Status: "done"})
	return nil
}

func (s *Service) checkProxy(ctx context.Context) error {
	s.updateStatus("proxy", StepStatus{Status: "loading"})

	geoURL := "https://polymarket.com/api/geoblock"
	req, err := http.NewRequestWithContext(ctx, "GET", geoURL, nil)
	if err != nil {
		s.updateStatus("proxy", StepStatus{Status: "error", Error: err.Error()})
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	proxyStr := config.OutboundProxyURL()
	if proxyStr != "" {
		if proxyu, err := url.Parse(proxyStr); err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(proxyu)}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		s.updateStatus("proxy", StepStatus{Status: "error", Error: err.Error()})
		return err
	}
	defer resp.Body.Close()

	var result ProxyDetails
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		s.updateStatus("proxy", StepStatus{Status: "error", Error: err.Error()})
		return err
	}

	s.updateStatus("proxy", StepStatus{
		Status:   "done",
		Details:  result,
	})
	s.log.Info("proxy_check_done", "blocked", result.Blocked, "country", result.Country)
	return nil
}

func (s *Service) cacheBalances(ctx context.Context) error {
	s.updateStatus("balance", StepStatus{Status: "loading"})

	summary, err := balancesvc.Fetch(ctx, s.cfg, s.st)
	if err != nil {
		s.updateStatus("balance", StepStatus{Status: "error", Error: err.Error()})
		return err
	}

	count := 0
	if summary != nil && summary.PolymarketAccounts != nil {
		count = len(summary.PolymarketAccounts)
	}

	s.updateStatus("balance", StepStatus{
		Status:  "done",
		Details: CacheDetails{Count: count},
	})
	s.log.Info("balance_cache_done", "count", count)
	return nil
}

func (s *Service) cachePositions(ctx context.Context) error {
	s.updateStatus("position", StepStatus{Status: "loading"})

	meta := risksvc.Meta{OutboundProxyConfigured: s.cfg.HTTPPlatformProxy != ""}
	rows, _, err := s.risk.ListRiskPositionsEnriched(ctx, meta)
	if err != nil {
		s.updateStatus("position", StepStatus{Status: "error", Error: err.Error()})
		return err
	}

	count := len(rows)
	s.updateStatus("position", StepStatus{
		Status:  "done",
		Details: CacheDetails{Count: count},
	})
	s.log.Info("position_cache_done", "count", count)

	if s.hub != nil {
		s.hub.BroadcastJSON(map[string]any{
			"type": "position_update",
			"data": rows,
		})
	}
	return nil
}
