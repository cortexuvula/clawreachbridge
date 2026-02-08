package security

import (
	"fmt"
	"testing"

	"golang.org/x/time/rate"
)

func TestRateLimiterAllow(t *testing.T) {
	// 1 request per second, burst of 2
	rl := NewRateLimiter(rate.Limit(1), 2)
	defer rl.Stop()

	ip := "100.64.0.1"

	// First two should succeed (burst)
	if !rl.Allow(ip) {
		t.Error("first request should be allowed")
	}
	if !rl.Allow(ip) {
		t.Error("second request (burst) should be allowed")
	}

	// Third should be denied (burst exhausted, no time to replenish)
	if rl.Allow(ip) {
		t.Error("third request should be denied (burst exhausted)")
	}
}

func TestRateLimiterPerIP(t *testing.T) {
	// Very low rate to test per-IP isolation
	rl := NewRateLimiter(rate.Limit(1), 1)
	defer rl.Stop()

	// IP A uses its burst
	if !rl.Allow("100.64.0.1") {
		t.Error("IP A first request should be allowed")
	}
	if rl.Allow("100.64.0.1") {
		t.Error("IP A second request should be denied")
	}

	// IP B should still have its own burst
	if !rl.Allow("100.64.0.2") {
		t.Error("IP B first request should be allowed")
	}
}

func TestRateLimiterUpdateRate(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(1), 1)
	defer rl.Stop()

	ip := "100.64.0.1"

	// Use up burst
	rl.Allow(ip)

	// Update to higher burst
	rl.UpdateRate(rate.Limit(1), 5)

	// Should have new burst available
	if !rl.Allow(ip) {
		t.Error("should be allowed after rate update")
	}
}

func TestRateLimiterMaxEntries(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(1), 10)
	defer rl.Stop()

	// Override maxEntries to a small value for testing
	rl.mu.Lock()
	rl.maxEntries = 3
	rl.mu.Unlock()

	// First 3 IPs should be allowed
	for i := 0; i < 3; i++ {
		ip := fmt.Sprintf("100.64.0.%d", i+1)
		if !rl.Allow(ip) {
			t.Errorf("IP %s should be allowed (map not full)", ip)
		}
	}

	// 4th IP should be rejected (map cap reached)
	if rl.Allow("100.64.0.100") {
		t.Error("should reject new IP when map is at capacity")
	}

	// Existing IP should still be allowed
	if !rl.Allow("100.64.0.1") {
		t.Error("existing IP should still be allowed")
	}
}

func TestRateLimiterStop(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(1), 1)
	rl.Stop() // Should not panic or deadlock
}
