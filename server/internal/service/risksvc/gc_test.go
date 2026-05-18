package risksvc

import (
	"sync"
	"testing"
	"time"
)

func TestGcCloseLocks(t *testing.T) {
	t.Parallel()
	s := &Service{closeLocks: sync.Map{}}
	now := time.Now().UnixMilli()
	// One stale, one fresh. Stale = older than closeLockMaxIdle.
	s.closeLocks.Store("stale", &closeLockMeta{lastMs: now - (closeLockMaxIdle + time.Minute).Milliseconds()})
	s.closeLocks.Store("fresh", &closeLockMeta{lastMs: now})

	if got := s.gcCloseLocks(); got != 1 {
		t.Fatalf("expected 1 stale eviction, got %d", got)
	}
	if _, ok := s.closeLocks.Load("stale"); ok {
		t.Fatalf("stale entry should be removed")
	}
	if _, ok := s.closeLocks.Load("fresh"); !ok {
		t.Fatalf("fresh entry must survive")
	}
}

func TestGcCloseLocksDropsCorruptValues(t *testing.T) {
	t.Parallel()
	s := &Service{closeLocks: sync.Map{}}
	// Simulate a legacy entry storing the wrong type. GC should drop it.
	s.closeLocks.Store("legacy", &sync.Mutex{})
	if got := s.gcCloseLocks(); got != 1 {
		t.Fatalf("expected legacy entry to be dropped, got removed=%d", got)
	}
}

func TestGcStopLossCooldown(t *testing.T) {
	t.Parallel()
	s := &Service{slMktEndedCool: map[string]time.Time{}}
	now := time.Now().UTC()
	s.slMktEndedCool["expired"] = now.Add(-time.Minute)
	s.slMktEndedCool["live"] = now.Add(10 * time.Minute)
	if got := s.gcStopLossCooldown(); got != 1 {
		t.Fatalf("expected 1 expired entry removed, got %d", got)
	}
	if _, ok := s.slMktEndedCool["expired"]; ok {
		t.Fatalf("expired entry should be deleted")
	}
	if _, ok := s.slMktEndedCool["live"]; !ok {
		t.Fatalf("live entry must survive")
	}
}

func TestLoadOrStoreCloseLockUpgradesLegacy(t *testing.T) {
	t.Parallel()
	s := &Service{closeLocks: sync.Map{}}
	// Insert a legacy *sync.Mutex shape; loadOrStoreCloseLock should
	// transparently replace it with a closeLockMeta so callers always
	// get something that gcCloseLocks understands.
	s.closeLocks.Store("legacy", &sync.Mutex{})
	got := s.loadOrStoreCloseLock("legacy")
	if got == nil || got.lastMs == 0 {
		t.Fatalf("expected upgraded meta with lastMs set, got %+v", got)
	}
	v, _ := s.closeLocks.Load("legacy")
	if _, ok := v.(*closeLockMeta); !ok {
		t.Fatalf("legacy entry was not upgraded in the map")
	}
}

func TestCloseEnqueueRecently(t *testing.T) {
	t.Parallel()
	s := &Service{closeEnqueueRecent: sync.Map{}}
	pid := "pos-1"
	if s.closeEnqueueRecently(pid) {
		t.Fatal("expected no recent enqueue before touch")
	}
	s.touchCloseEnqueue(pid)
	if !s.closeEnqueueRecently(pid) {
		t.Fatal("expected recent enqueue immediately after touch")
	}
}

func TestGcCloseEnqueueRecent(t *testing.T) {
	t.Parallel()
	s := &Service{closeEnqueueRecent: sync.Map{}}
	now := time.Now().UnixMilli()
	s.closeEnqueueRecent.Store("stale", &closeEnqueueMeta{
		lastMs: now - closeEnqueueDedupeWindow.Milliseconds() - 1,
	})
	s.closeEnqueueRecent.Store("fresh", &closeEnqueueMeta{lastMs: now})
	if got := s.gcCloseEnqueueRecent(); got != 1 {
		t.Fatalf("expected 1 stale eviction, got %d", got)
	}
	if _, ok := s.closeEnqueueRecent.Load("stale"); ok {
		t.Fatal("stale entry should be removed")
	}
	if _, ok := s.closeEnqueueRecent.Load("fresh"); !ok {
		t.Fatal("fresh entry must survive")
	}
}
