package guard

import (
	"sync"
	"time"
)

type cacheEntry[T any] struct {
	value   T
	expires time.Time
}

type ttlCache[T any] struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]cacheEntry[T]
}

func newTTLCache[T any](ttl time.Duration) *ttlCache[T] {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &ttlCache[T]{
		ttl:   ttl,
		items: make(map[string]cacheEntry[T]),
	}
}

func (c *ttlCache[T]) get(key string) (T, bool) {
	var zero T

	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return zero, false
	}
	if time.Now().After(entry.expires) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return zero, false
	}
	return entry.value, true
}

func (c *ttlCache[T]) set(key string, value T) {
	c.mu.Lock()
	c.items[key] = cacheEntry[T]{
		value:   value,
		expires: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}
