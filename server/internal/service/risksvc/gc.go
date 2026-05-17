package risksvc

import (
	"context"
	"sync"
	"time"

	"github.com/easyspace-ai/polybet/internal/bookcache"
	"github.com/easyspace-ai/polybet/internal/logx"
)

// gcInterval is how often the background sweep runs. The maps it cleans
// (closeLocks, slMktEndedCool) are bounded by the number of distinct
// position IDs ever seen — small per-process — but the bot has no upper
// limit so without GC long-running deployments grow unbounded.
const gcInterval = 5 * time.Minute

// closeLockMaxIdle is how long a per-position close-task mutex stays in
// closeLocks before being eligible for removal. Long enough that an
// in-flight close finishes and any retries within the standard backoff
// window (max ~60s) keep the same lock.
const closeLockMaxIdle = 30 * time.Minute

// bookCachePruneAge mirrors closeLockMaxIdle — token caches that have not
// been touched in this long are evicted on every sweep.
const bookCachePruneAge = 4 * time.Hour

// closeLockMeta extends the per-position mutex with a last-touched timestamp.
// closeLocks legacy storage was a sync.Map[string]*sync.Mutex with no
// activity tracking; GC needs the timestamp to know what is safe to drop.
type closeLockMeta struct {
	mu     sync.Mutex
	lastMs int64
}

// touchCloseLock records that a close path used the lock (called inside
// ensureCloseTask while the mutex is held). Keeps per-position lock
// metadata fresh so GC does not evict actively-used entries.
func (s *Service) touchCloseLock(positionID string) {
	if s == nil || positionID == "" {
		return
	}
	now := time.Now().UnixMilli()
	if v, ok := s.closeLocks.Load(positionID); ok {
		if m, ok2 := v.(*closeLockMeta); ok2 {
			m.lastMs = now
		}
	}
}

// loadOrStoreCloseLock fetches an existing meta or creates a new one. New
// entries are stamped with the current time so the next GC sweep gives
// them a full closeLockMaxIdle window.
func (s *Service) loadOrStoreCloseLock(positionID string) *closeLockMeta {
	now := time.Now().UnixMilli()
	v, _ := s.closeLocks.LoadOrStore(positionID, &closeLockMeta{lastMs: now})
	m, ok := v.(*closeLockMeta)
	if !ok {
		// Defensive: legacy code stored a *sync.Mutex directly. Replace.
		fresh := &closeLockMeta{lastMs: now}
		s.closeLocks.Store(positionID, fresh)
		return fresh
	}
	if m.lastMs == 0 {
		m.lastMs = now
	}
	return m
}

// gcCloseLocks removes per-position locks that have not been touched in
// closeLockMaxIdle. Safe to call concurrently with active close paths:
// touch happens under the lock, so any "currently held" mutex either has
// a fresh timestamp or is held in a goroutine that holds a *Mutex
// reference and will finish without re-reading from the map.
func (s *Service) gcCloseLocks() int {
	if s == nil {
		return 0
	}
	cutoff := time.Now().UnixMilli() - closeLockMaxIdle.Milliseconds()
	removed := 0
	s.closeLocks.Range(func(k, v any) bool {
		m, ok := v.(*closeLockMeta)
		if !ok || m == nil {
			s.closeLocks.Delete(k)
			removed++
			return true
		}
		if m.lastMs > 0 && m.lastMs < cutoff {
			s.closeLocks.Delete(k)
			removed++
		}
		return true
	})
	return removed
}

// gcStopLossCooldown drops cooldown entries whose deadline has already
// passed. The legacy code only cleaned them lazily (on read); a position
// that closes and never re-evaluates leaves the entry in the map forever.
func (s *Service) gcStopLossCooldown() int {
	if s == nil {
		return 0
	}
	now := time.Now().UTC()
	s.slMktEndedCoolMu.Lock()
	defer s.slMktEndedCoolMu.Unlock()
	removed := 0
	for pid, until := range s.slMktEndedCool {
		if now.After(until) {
			delete(s.slMktEndedCool, pid)
			removed++
		}
	}
	return removed
}

// RunGC starts the periodic background sweep. Returns when ctx is
// cancelled. The sweep also prunes idle entries from the supplied
// bookcache so the three caches age in lockstep.
func (s *Service) RunGC(ctx context.Context, cache *bookcache.Cache) {
	if s == nil {
		return
	}
	t := time.NewTicker(gcInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			locksRemoved := s.gcCloseLocks()
			cooldownRemoved := s.gcStopLossCooldown()
			cacheRemoved := 0
			if cache != nil {
				cacheRemoved = cache.PruneIdle(bookCachePruneAge)
			}
			if s.log != nil && (locksRemoved > 0 || cooldownRemoved > 0 || cacheRemoved > 0) {
				s.log.WithFields(logx.Pairs(
					"close_locks_removed", locksRemoved,
					"sl_cooldown_removed", cooldownRemoved,
					"book_cache_removed", cacheRemoved,
				)).Info("风控：周期 GC 完成")
			}
		}
	}
}
