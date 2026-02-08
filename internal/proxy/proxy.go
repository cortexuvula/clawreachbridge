package proxy

import (
	"sync"
	"sync/atomic"
)

// Proxy tracks active connections and provides connection counting.
type Proxy struct {
	activeConnections atomic.Int64
	totalConnections  atomic.Int64
	totalMessages     atomic.Int64

	// Per-IP connection tracking
	ipConnections map[string]int
	ipMu          sync.Mutex
}

// New creates a new Proxy instance.
func New() *Proxy {
	return &Proxy{
		ipConnections: make(map[string]int),
	}
}

// ConnectionCount returns the current number of active connections.
func (p *Proxy) ConnectionCount() int {
	return int(p.activeConnections.Load())
}

// ConnectionCountForIP returns the active connection count for a specific IP.
func (p *Proxy) ConnectionCountForIP(ip string) int {
	p.ipMu.Lock()
	defer p.ipMu.Unlock()
	return p.ipConnections[ip]
}

// TryIncrementConnections atomically checks limits and increments counters.
// Returns "" on success, or a reason string if the limit was hit.
func (p *Proxy) TryIncrementConnections(ip string, maxGlobal, maxPerIP int) string {
	p.ipMu.Lock()
	defer p.ipMu.Unlock()

	// Check global limit (read atomic under the lock to prevent TOCTOU)
	current := int(p.activeConnections.Load())
	if current >= maxGlobal {
		return "max_connections"
	}

	// Check per-IP limit
	if p.ipConnections[ip] >= maxPerIP {
		return "max_connections_per_ip"
	}

	// Both checks passed — increment atomically
	p.activeConnections.Add(1)
	p.totalConnections.Add(1)
	p.ipConnections[ip]++
	return ""
}

// IncrementConnections increments both global and per-IP connection counters.
func (p *Proxy) IncrementConnections(ip string) {
	p.activeConnections.Add(1)
	p.totalConnections.Add(1)
	p.ipMu.Lock()
	p.ipConnections[ip]++
	p.ipMu.Unlock()
}

// DecrementConnections decrements both global and per-IP connection counters.
func (p *Proxy) DecrementConnections(ip string) {
	p.activeConnections.Add(-1)
	p.ipMu.Lock()
	p.ipConnections[ip]--
	if p.ipConnections[ip] <= 0 {
		delete(p.ipConnections, ip)
	}
	p.ipMu.Unlock()
}

// IncrementMessages increments the total messages counter.
func (p *Proxy) IncrementMessages() {
	p.totalMessages.Add(1)
}

// TotalConnections returns the total number of connections handled since start.
func (p *Proxy) TotalConnections() int64 {
	return p.totalConnections.Load()
}

// TotalMessages returns the total number of messages proxied since start.
func (p *Proxy) TotalMessages() int64 {
	return p.totalMessages.Load()
}
