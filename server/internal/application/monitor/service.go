package monitor

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/easyspace-ai/polybet/internal/application/connectivity"
	"github.com/easyspace-ai/polybet/internal/config"
	"github.com/easyspace-ai/polybet/internal/marketstream"
	"github.com/easyspace-ai/polybet/internal/service/polysession"
	"github.com/easyspace-ai/polybet/internal/service/risksvc"
	"github.com/easyspace-ai/polybet/internal/storage"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

// Service orchestrates monitor use cases (CLOB session, heartbeat, stop-loss trigger).
type Service struct {
	cfg      *config.Config
	st       *storage.Backend
	risk     *risksvc.Service
	conn     *connectivity.Service
	watcher  *TaskWatcher
	hub      *wsrelay.Hub
	riskHub  *wsrelay.Hub
}

func NewService(
	cfg *config.Config,
	st *storage.Backend,
	risk *risksvc.Service,
	conn *connectivity.Service,
	hub, riskHub *wsrelay.Hub,
) *Service {
	w := NewTaskWatcher(st, risk)
	return &Service{
		cfg:     cfg,
		st:      st,
		risk:    risk,
		conn:    conn,
		watcher: w,
		hub:     hub,
		riskHub: riskHub,
	}
}

func (s *Service) TaskWatcher() *TaskWatcher {
	return s.watcher
}

// ClobSession returns L2 credentials for browser-direct CLOB WS (loopback only).
type ClobSession struct {
	APIKey        string `json:"apiKey"`
	APISecret     string `json:"apiSecret"`
	APIPassphrase string `json:"apiPassphrase"`
	MarketWSURL   string `json:"marketWsUrl"`
	UserWSURL     string `json:"userWsUrl"`
	AccountID     string `json:"accountId"`
	ProxyURL      string `json:"proxyUrl,omitempty"`
}

func (s *Service) ClobSession(ctx context.Context) (*ClobSession, error) {
	cl, err := polysession.ResolveAuthedCLOB(ctx, s.cfg, s.st)
	if err != nil {
		return nil, err
	}
	if cl.APIKey == nil {
		return nil, errors.New("missing_api_key")
	}
	marketURL, userURL := marketstream.ResolveCLOBWSEndpoints(s.cfg.PolymarketCLOBWS)
	acct, _ := s.st.GetActivePolymarketAccount(ctx)
	accountID := ""
	if acct != nil {
		accountID = acct.ID
	}
	return &ClobSession{
		APIKey:        cl.APIKey.Key,
		APISecret:     cl.APIKey.Secret,
		APIPassphrase: cl.APIKey.Passphrase,
		MarketWSURL:   marketURL,
		UserWSURL:     userURL,
		AccountID:     accountID,
		ProxyURL:      strings.TrimSpace(s.cfg.HTTPPlatformProxy),
	}, nil
}

type HeartbeatInput struct {
	UserConnected      bool     `json:"userConnected"`
	OrderbookConnected bool     `json:"orderbookConnected"`
	SubscribedTokens   []string `json:"subscribedTokens"`
}

func (s *Service) Heartbeat(in HeartbeatInput) {
	n := len(in.SubscribedTokens)
	s.conn.ReportClientHeartbeat(in.UserConnected, in.OrderbookConnected, n)
}

func (s *Service) SyncPositions(ctx context.Context) error {
	acct, err := s.st.GetActivePolymarketAccount(ctx)
	if err != nil || acct == nil {
		return errors.New("no_active_account")
	}
	return s.risk.SyncPositionsFromDataAPI(ctx, acct.ID)
}

type StopLossTriggerInput struct {
	PositionID   string  `json:"positionId"`
	TokenID      string  `json:"tokenId"`
	TriggerCents float64 `json:"triggerCents"`
	TrailCents   float64 `json:"trailCents"`
}

type StopLossTriggerResult struct {
	OK       bool   `json:"ok"`
	TaskID   string `json:"taskId,omitempty"`
	Position string `json:"positionId"`
}

type ProfitProtectEvaluateInput struct {
	PositionID string  `json:"positionId"`
	BidCents   float64 `json:"bidCents"`
	AskCents   float64 `json:"askCents"`
}

type ProfitProtectEvaluateResult struct {
	OK       bool `json:"ok"`
	Position string `json:"positionId"`
}

func (s *Service) TriggerStopLoss(ctx context.Context, in StopLossTriggerInput) (*StopLossTriggerResult, error) {
	pid := strings.TrimSpace(in.PositionID)
	if pid == "" {
		return nil, errors.New("position_id_required")
	}
	if err := s.risk.EnqueueStopLossClose(ctx, pid); err != nil {
		return nil, err
	}
	s.watcher.Register(pid)
	return &StopLossTriggerResult{OK: true, Position: pid}, nil
}

// EvaluateProfitProtect runs server-side profit-protect arming/trigger using browser book prices (second defense line).
func (s *Service) EvaluateProfitProtect(ctx context.Context, in ProfitProtectEvaluateInput) (*ProfitProtectEvaluateResult, error) {
	pid := strings.TrimSpace(in.PositionID)
	if pid == "" {
		return nil, errors.New("position_id_required")
	}
	bid := in.BidCents
	ask := in.AskCents
	if ask <= 0 && bid > 0 {
		ask = bid
	}
	if bid <= 0 && ask > 0 {
		bid = ask
	}
	if err := s.risk.EvaluateProfitProtectForPosition(ctx, pid, bid, ask); err != nil {
		return nil, err
	}
	s.watcher.Register(pid)
	return &ProfitProtectEvaluateResult{OK: true, Position: pid}, nil
}

// BroadcastAccountChanged notifies dashboard to reconnect CLOB after account switch.
func (s *Service) BroadcastAccountChanged(accountID string) {
	msg := map[string]any{
		"type":      "monitor_account_changed",
		"accountId": accountID,
	}
	if s.hub != nil {
		s.hub.BroadcastJSON(msg)
	}
	if s.riskHub != nil {
		s.riskHub.BroadcastJSON(msg)
	}
}

// AllowClobSessionRequest returns true for loopback dashboard callers.
func AllowClobSessionRequest(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	switch host {
	case "127.0.0.1", "::1", "localhost", "":
		return true
	default:
		return false
	}
}

// SyncRegistryFromRisk copies server upstream state into the connectivity registry.
func (s *Service) SyncRegistryFromRisk() {
	if s.risk == nil || s.conn == nil {
		return
	}
	issue := ""
	s.risk.UserWSLastIssue(&issue)
	s.conn.SyncFromServer(
		s.risk.UserWSConnected(),
		s.risk.UserWSConnecting(),
		s.risk.OrderbookWSConnected(),
		s.risk.OrderbookWSConnecting(),
		issue,
	)
	if s.risk.WSMeta != nil {
		ex := s.risk.WSMeta.Snapshot(issue)
		var obMs, userMs int64
		if ex.OrderbookNextRetryAt != nil {
			obMs = *ex.OrderbookNextRetryAt
		}
		if ex.UserNextRetryAt != nil {
			userMs = *ex.UserNextRetryAt
		}
		s.conn.SetWSMeta(ex.OrderbookReconnectAttempt, ex.UserReconnectAttempt, obMs, userMs)
	}
}
