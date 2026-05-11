package rediska

import (
	"sync"
	"time"
)

type MemCache struct {
	mu      sync.RWMutex
	data    map[string]memCacheItem
	ttl     time.Duration
}

type memCacheItem struct {
	value     any
	expiresAt int64
}

func NewMemCache(ttl time.Duration) *MemCache {
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	return &MemCache{
		data: make(map[string]memCacheItem),
		ttl:  ttl,
	}
}

func (m *MemCache) Get(key string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.data[key]
	if !ok {
		return nil, false
	}
	if time.Now().Unix() > item.expiresAt {
		return nil, false
	}
	return item.value, true
}

func (m *MemCache) Set(key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = memCacheItem{
		value:     value,
		expiresAt: time.Now().Add(m.ttl).Unix(),
	}
}

func (m *MemCache) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

func (m *MemCache) GetOrSet(key string, fetch func() (any, error)) (any, bool, error) {
	if v, ok := m.Get(key); ok {
		go func() {
			if newV, err := fetch(); err == nil {
				m.Set(key, newV)
			}
		}()
		return v, true, nil
	}
	v, err := fetch()
	if err != nil {
		return nil, false, err
	}
	m.Set(key, v)
	return v, false, nil
}