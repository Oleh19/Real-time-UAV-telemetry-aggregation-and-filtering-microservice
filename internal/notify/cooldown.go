package notify

import (
	"sync"
	"time"
)

type cooldownGate struct {
	mu       sync.Mutex
	window   time.Duration
	lastSeen map[string]time.Time
}

func newCooldownGate(window time.Duration) *cooldownGate {
	return &cooldownGate{window: window, lastSeen: make(map[string]time.Time)}
}

func (g *cooldownGate) allow(key string, now time.Time) bool {
	if g.window <= 0 {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pruneLocked(now)
	if last, ok := g.lastSeen[key]; ok && now.Sub(last) < g.window {
		return false
	}
	g.lastSeen[key] = now
	return true
}

func (g *cooldownGate) pruneLocked(now time.Time) {
	for key, seen := range g.lastSeen {
		if now.Sub(seen) >= g.window {
			delete(g.lastSeen, key)
		}
	}
}
