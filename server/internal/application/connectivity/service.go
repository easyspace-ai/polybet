package connectivity

import (
	"sync"
	"time"

	domain "github.com/easyspace-ai/polybet/internal/domain/connectivity"
	"github.com/easyspace-ai/polybet/internal/wsrelay"
)

const broadcastDebounce = 200 * time.Millisecond

// Broadcaster pushes connectivity_snapshot to dashboard clients.
type Broadcaster func(snap domain.Snapshot)

// Service is the application layer for connectivity read/write.
type Service struct {
	reg *domain.Registry

	mu          sync.Mutex
	hub         *wsrelay.Hub
	riskHub     *wsrelay.Hub
	debounce    *time.Timer
	pending     domain.Snapshot
	broadcastFn Broadcaster
}

func NewService(reg *domain.Registry, hub, riskHub *wsrelay.Hub) *Service {
	s := &Service{reg: reg, hub: hub, riskHub: riskHub}
	reg.SetOnChange(s.scheduleBroadcast)
	return s
}

func (s *Service) Registry() *domain.Registry {
	return s.reg
}

func (s *Service) Snapshot() domain.Snapshot {
	return s.reg.Snapshot()
}

func (s *Service) LegacyWSStatus() map[string]any {
	return s.reg.Snapshot().LegacyWSStatusJSON()
}

func (s *Service) SetRelayClients(n int) {
	s.reg.SetRelay(n)
}

func (s *Service) SetOpenPositionCount(n int) {
	s.reg.SetOpenPositionCount(n)
}

func (s *Service) SetLastBookUpdateMs(ms int64) {
	s.reg.SetLastBookUpdateMs(ms)
}

func (s *Service) ReportClientHeartbeat(userConnected, obConnected bool, subscribedTokens int) {
	s.reg.ClientHeartbeat(userConnected, obConnected, subscribedTokens)
}

func (s *Service) SyncFromServer(userConnected, userConnecting, obConnected, obConnecting bool, issue string) {
	s.reg.SyncServerUpstream(userConnected, userConnecting, obConnected, obConnecting, issue)
}

func (s *Service) SetWSMeta(orderbookAttempt, userAttempt int, obRetryMs, userRetryMs int64) {
	s.reg.SetWSMeta(orderbookAttempt, userAttempt, obRetryMs, userRetryMs)
}

func (s *Service) scheduleBroadcast(snap domain.Snapshot) {
	s.mu.Lock()
	s.pending = snap
	if s.debounce != nil {
		s.mu.Unlock()
		return
	}
	s.debounce = time.AfterFunc(broadcastDebounce, s.flushBroadcast)
	s.mu.Unlock()
}

func (s *Service) flushBroadcast() {
	s.mu.Lock()
	snap := s.pending
	s.debounce = nil
	s.mu.Unlock()
	msg := snap.ConnectivitySnapshotJSON()
	if s.hub != nil {
		s.hub.BroadcastJSONAsync(msg)
	}
	if s.riskHub != nil {
		s.riskHub.BroadcastJSONAsync(msg)
	}
	if s.broadcastFn != nil {
		s.broadcastFn(snap)
	}
}

// BroadcastNow sends connectivity_snapshot immediately (e.g. on WS connect).
func (s *Service) BroadcastNow() {
	snap := s.reg.Snapshot()
	msg := snap.ConnectivitySnapshotJSON()
	if s.hub != nil {
		s.hub.BroadcastJSON(msg)
	}
	if s.riskHub != nil {
		s.riskHub.BroadcastJSON(msg)
	}
}
