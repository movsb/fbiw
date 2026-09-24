package helpers

import (
	"sync"
	"time"
)

type ttlCacheEntry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
}

type ttlCacheLoad[V any] struct {
	done  chan struct{}
	value V
	err   error
	panic any
}

// TTLCache is a fixed-capacity LRU cache whose entries expire after a period
// without access.
type TTLCache[K comparable, V any] struct {
	mu              sync.Mutex
	capacity        int
	ttl             time.Duration
	cleanupInterval time.Duration
	entries         map[K]*Element[ttlCacheEntry[K, V]]
	list            List[ttlCacheEntry[K, V]]
	loads           map[K]*ttlCacheLoad[V]
	cleaner         chan struct{}
	now             func() time.Time
}

// NewTTLCache creates a cache with the given capacity, sliding expiration, and
// expired-entry cleanup interval.
func NewTTLCache[K comparable, V any](capacity int, ttl, cleanup time.Duration) *TTLCache[K, V] {
	if capacity <= 0 {
		panic("helpers: TTLCache capacity must be positive")
	}
	if ttl <= 0 {
		panic("helpers: TTLCache TTL must be positive")
	}
	if cleanup <= 0 {
		panic("helpers: TTLCache cleanup interval must be positive")
	}
	return &TTLCache[K, V]{
		capacity:        capacity,
		ttl:             ttl,
		cleanupInterval: cleanup,
		entries:         make(map[K]*Element[ttlCacheEntry[K, V]], capacity),
		loads:           make(map[K]*ttlCacheLoad[V]),
		now:             time.Now,
	}
}

// Get returns the value associated with key. A successful access renews the
// entry's expiration and moves it to the front of the LRU list.
func (c *TTLCache[K, V]) Get(key K) (value V, ok bool) {
	c.mu.Lock()
	value, ok = c.getLocked(key, c.now())
	c.mu.Unlock()
	return value, ok
}

// GetOrLoad returns the value associated with key, loading and caching it on a
// miss. Concurrent loads for the same key are combined.
func (c *TTLCache[K, V]) GetOrLoad(key K, loader func() (V, error)) (value V, err error) {
	if loader == nil {
		panic("helpers: TTLCache loader must not be nil")
	}

	c.mu.Lock()
	if value, ok := c.getLocked(key, c.now()); ok {
		c.mu.Unlock()
		return value, nil
	}
	if call, ok := c.loads[key]; ok {
		c.mu.Unlock()
		<-call.done
		if call.panic != nil {
			panic(call.panic)
		}
		return call.value, call.err
	}

	call := &ttlCacheLoad[V]{done: make(chan struct{})}
	c.loads[key] = call
	c.mu.Unlock()

	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()
		value, err = loader()
	}()

	c.mu.Lock()
	if panicValue == nil && err == nil {
		c.setLocked(key, value, c.now())
	}
	call.value = value
	call.err = err
	call.panic = panicValue
	delete(c.loads, key)
	close(call.done)
	c.mu.Unlock()

	if panicValue != nil {
		panic(panicValue)
	}
	return value, err
}

func (c *TTLCache[K, V]) getLocked(key K, now time.Time) (value V, ok bool) {
	e, ok := c.entries[key]
	if !ok {
		return value, false
	}
	entry := &e.Value
	if !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
		c.removeLocked(e)
		return value, false
	}
	entry.expiresAt = now.Add(c.ttl)
	c.list.MoveToFront(e)
	return entry.value, true
}

func (c *TTLCache[K, V]) setLocked(key K, value V, now time.Time) {
	entry := ttlCacheEntry[K, V]{key: key, value: value, expiresAt: now.Add(c.ttl)}
	e := c.list.PushFront(entry)
	c.entries[key] = e

	if c.list.Len() > c.capacity {
		c.removeLocked(c.list.Back())
	}
	if c.list.Len() == 1 && c.cleaner == nil {
		c.startCleanerLocked()
	}
}

func (c *TTLCache[K, V]) removeLocked(e *Element[ttlCacheEntry[K, V]]) {
	delete(c.entries, e.Value.key)
	c.list.Remove(e)
	if c.list.Len() == 0 && c.cleaner != nil {
		close(c.cleaner)
		c.cleaner = nil
	}
}

func (c *TTLCache[K, V]) startCleanerLocked() {
	cleaner := make(chan struct{})
	c.cleaner = cleaner
	go c.runCleaner(cleaner)
}

func (c *TTLCache[K, V]) runCleaner(cleaner chan struct{}) {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			if !c.cleanup(now, cleaner) {
				return
			}
		case <-cleaner:
			return
		}
	}
}

func (c *TTLCache[K, V]) cleanup(now time.Time, cleaner chan struct{}) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cleaner != cleaner {
		return false
	}
	for e := c.list.Front(); e != nil; {
		next := e.Next()
		if expiresAt := e.Value.expiresAt; !expiresAt.IsZero() && !now.Before(expiresAt) {
			c.removeLocked(e)
		}
		e = next
	}
	return c.cleaner == cleaner
}
