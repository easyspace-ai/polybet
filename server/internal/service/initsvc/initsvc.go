package initsvc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/logx"
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
	log  *logrus.Logger
	risk *risksvc.Service

	mu     sync.RWMutex
	status InitStatus
}

func New(cfg *config.Config, st *store.Store, hub *wsrelay.Hub, risk *risksvc.Service, log *logrus.Logger) *Service {
	if log == nil {
		log = logrus.StandardLogger()
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
	s.log.Info("初始化服务：开始")

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := s.checkConfig(ctx); err != nil {
		s.log.WithFields(logx.Pairs("err", err)).Error("初始化：配置检查失败")
		return err
	}

	if err := s.checkProxy(ctx); err != nil {
		s.log.WithFields(logx.Pairs("err", err)).Error("初始化：代理检查失败")
	}

	if err := s.cacheBalances(ctx); err != nil {
		s.log.WithFields(logx.Pairs("err", err)).Error("初始化：余额缓存失败")
	}

	if err := s.cachePositions(ctx); err != nil {
		s.log.WithFields(logx.Pairs("err", err)).Error("初始化：持仓缓存失败")
	}

	s.setComplete(true)
	s.log.Info("初始化服务：全部步骤已完成")
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		s.updateStatus("proxy", StepStatus{Status: "error", Error: err.Error()})
		return err
	}
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("geoblock http %d", resp.StatusCode)
		s.updateStatus("proxy", StepStatus{Status: "error", Error: errMsg})
		return fmt.Errorf("%s", errMsg)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "json") && len(body) > 0 && body[0] == '<' {
		errMsg := "geoblock returned HTML (proxy misconfigured or blocked)"
		s.updateStatus("proxy", StepStatus{Status: "error", Error: errMsg})
		return fmt.Errorf("%s", errMsg)
	}

	var result ProxyDetails
	if err := json.Unmarshal(body, &result); err != nil {
		s.updateStatus("proxy", StepStatus{Status: "error", Error: err.Error()})
		return err
	}

	s.updateStatus("proxy", StepStatus{
		Status:   "done",
		Details:  result,
	})
	s.log.WithFields(logx.Pairs("blocked", result.Blocked, "country", result.Country)).Info("初始化：代理检测完成")
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
	s.log.WithFields(logx.Pairs("count", count)).Info("初始化：余额缓存完成")
	return nil
}

func (s *Service) cachePositions(ctx context.Context) error {
	s.updateStatus("position", StepStatus{Status: "loading"})

	meta := risksvc.Meta{OutboundProxyConfigured: s.cfg.HTTPPlatformProxy != ""}
	acct, _ := s.st.GetActivePolymarketAccount(ctx)
	accountID := ""
	if acct != nil {
		accountID = acct.ID
	}
	rows, _, err := s.risk.ListRiskPositionsEnriched(ctx, meta, accountID)
	if err != nil {
		s.updateStatus("position", StepStatus{Status: "error", Error: err.Error()})
		return err
	}

	count := len(rows)
	s.updateStatus("position", StepStatus{
		Status:  "done",
		Details: CacheDetails{Count: count},
	})
	s.log.WithFields(logx.Pairs("count", count)).Info("初始化：持仓缓存完成")

	if s.hub != nil {
		s.hub.BroadcastJSON(map[string]any{
			"type": "position_update",
			"data": rows,
		})
	}
	return nil
}
