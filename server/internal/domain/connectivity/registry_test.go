package connectivity

import (
	"testing"
	"time"
)

func TestRegistry_ClientHeartbeatOwner(t *testing.T) {
	r := NewRegistry()
	r.ClientHeartbeat(true, true, 3)
	s := r.Snapshot()
	if s.Owner != OwnerClient {
		t.Fatalf("owner=%s", s.Owner)
	}
	if !s.User.Connected || !s.Orderbook.Connected {
		t.Fatalf("user=%+v ob=%+v", s.User, s.Orderbook)
	}
	if s.SubscribedTokenCount != 3 {
		t.Fatalf("tokens=%d", s.SubscribedTokenCount)
	}
}

func TestRegistry_ServerFallbackWhenHeartbeatStale(t *testing.T) {
	r := NewRegistry()
	r.ClientHeartbeat(true, true, 1)
	r.mu.Lock()
	r.snap.LastClientHeartbeat = time.Now().Add(-ClientHeartbeatStale - time.Second)
	r.mu.Unlock()
	r.SyncServerUpstream(false, true, false, false, "disconnected")
	s := r.Snapshot()
	if s.Owner != OwnerServer {
		t.Fatalf("owner=%s", s.Owner)
	}
	if s.User.Connecting != true {
		t.Fatalf("expected user connecting, got %+v", s.User)
	}
}

func TestRegistry_OrderbookStandbyWithoutPositions(t *testing.T) {
	r := NewRegistry()
	r.SetOpenPositionCount(0)
	r.SyncServerUpstream(true, false, false, false, "")
	s := r.Snapshot()
	if s.Orderbook.Display != DisplayStandby {
		t.Fatalf("ob display=%s", s.Orderbook.Display)
	}
}
