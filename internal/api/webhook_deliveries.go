package api

import (
	"sync"
	"time"
)

type deliveryKey struct {
	body    [32]byte
	service string
}
type deliveryEntry struct {
	expires time.Time
	done    bool
}

// deliveryCache tracks authenticated payloads, rather than the unsigned delivery
// header, with a fixed bound. In-flight work is retained until finish is called.
type deliveryCache struct {
	mu      sync.Mutex
	entries map[deliveryKey]deliveryEntry
}

func (c *deliveryCache) begin(key deliveryKey, now time.Time) (duplicate, busy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[deliveryKey]deliveryEntry)
	}
	for k, e := range c.entries {
		if e.done && !e.expires.After(now) {
			delete(c.entries, k)
		}
	}
	if e, ok := c.entries[key]; ok {
		return e.done, !e.done
	}
	if len(c.entries) >= 1024 {
		var oldest deliveryKey
		var expiry time.Time
		for k, e := range c.entries {
			if e.done && (expiry.IsZero() || e.expires.Before(expiry)) {
				oldest = k
				expiry = e.expires
			}
		}
		if expiry.IsZero() {
			return false, true
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = deliveryEntry{expires: now.Add(24 * time.Hour)}
	return false, false
}
func (c *deliveryCache) finish(key deliveryKey, success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !success {
		delete(c.entries, key)
		return
	}
	e := c.entries[key]
	e.done = true
	c.entries[key] = e
}
