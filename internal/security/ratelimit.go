package security

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter implements per-IP token bucket rate limiting with automatic
// cleanup of stale entries to prevent memory leaks.
type RateLimiter struct {
	limiters   map[string]*ipLimiter
	mu         sync.Mutex
	r          rate.Limit
	burst      int
	ttl        time.Duration // evict entries not seen within this window
	maxEntries int           // cap on number of tracked IPs
	cancel     context.CancelFunc
}

// NewRateLimiter creates a new per-IP rate limiter.
// r is the rate (events per second), burst is the maximum burst size.
func NewRateLimiter(r rate.Limit, burst int) *RateLimiter {
	ctx, cancel := context.WithCancel(context.Background())
	rl := &RateLimiter{
		limiters:   make(map[string]*ipLimiter),
		r:          r,
		burst:      burst,
		ttl:        10 * time.Minute,
		maxEntries: 10000,
		cancel:     cancel,
	}
	go rl.cleanup(ctx) // background goroutine to evict stale entries
	return rl
}

// Allow checks whether the given IP is allowed to proceed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	entry, exists := rl.limiters[ip]
	if !exists {
		if len(rl.limiters) >= rl.maxEntries {
			rl.mu.Unlock()
			return false // reject to prevent unbounded map growth
		}
		entry = &ipLimiter{limiter: rate.NewLimiter(rl.r, rl.burst)}
		rl.limiters[ip] = entry
	}
	entry.lastSeen = time.Now()
	rl.mu.Unlock()

	return entry.limiter.Allow()
}

// Stop shuts down the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	rl.cancel()
}

// UpdateRate changes the rate limit parameters. Existing per-IP limiters
// are cleared so they pick up the new rate on next access.
func (rl *RateLimiter) UpdateRate(r rate.Limit, burst int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.r = r
	rl.burst = burst
	// Clear existing limiters so they get recreated with new rate
	rl.limiters = make(map[string]*ipLimiter)
}

func (rl *RateLimiter) cleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.mu.Lock()
			for ip, entry := range rl.limiters {
				if time.Since(entry.lastSeen) > rl.ttl {
					delete(rl.limiters, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}
