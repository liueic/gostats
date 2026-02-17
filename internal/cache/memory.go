package cache

import (
	"sync"
	"time"
)

type entry struct {
	value     any
	expiresAt time.Time
}

type Memory struct {
	ttl   time.Duration
	mu    sync.RWMutex
	items map[string]entry
}

func NewMemory(ttl time.Duration) *Memory {
	return &Memory{
		ttl:   ttl,
		items: make(map[string]entry),
	}
}

func (m *Memory) Get(key string) (any, bool) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}

	if time.Now().After(item.expiresAt) {
		m.mu.Lock()
		delete(m.items, key)
		m.mu.Unlock()
		return nil, false
	}

	return item.value, true
}

func (m *Memory) Set(key string, value any) {
	m.mu.Lock()
	m.items[key] = entry{
		value:     value,
		expiresAt: time.Now().Add(m.ttl),
	}
	m.mu.Unlock()
}
