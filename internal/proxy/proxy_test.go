package proxy

import "testing"

func TestConnectionCount(t *testing.T) {
	p := New()

	if got := p.ConnectionCount(); got != 0 {
		t.Errorf("initial ConnectionCount() = %d, want 0", got)
	}

	p.IncrementConnections("100.64.0.1")
	p.IncrementConnections("100.64.0.1")
	p.IncrementConnections("100.64.0.2")

	if got := p.ConnectionCount(); got != 3 {
		t.Errorf("ConnectionCount() = %d, want 3", got)
	}

	if got := p.ConnectionCountForIP("100.64.0.1"); got != 2 {
		t.Errorf("ConnectionCountForIP(100.64.0.1) = %d, want 2", got)
	}

	if got := p.ConnectionCountForIP("100.64.0.2"); got != 1 {
		t.Errorf("ConnectionCountForIP(100.64.0.2) = %d, want 1", got)
	}

	if got := p.ConnectionCountForIP("100.64.0.3"); got != 0 {
		t.Errorf("ConnectionCountForIP(unknown) = %d, want 0", got)
	}

	p.DecrementConnections("100.64.0.1")
	if got := p.ConnectionCount(); got != 2 {
		t.Errorf("ConnectionCount() after decrement = %d, want 2", got)
	}
	if got := p.ConnectionCountForIP("100.64.0.1"); got != 1 {
		t.Errorf("ConnectionCountForIP(100.64.0.1) after decrement = %d, want 1", got)
	}

	// Decrement to zero should clean up map entry
	p.DecrementConnections("100.64.0.2")
	if got := p.ConnectionCountForIP("100.64.0.2"); got != 0 {
		t.Errorf("ConnectionCountForIP(100.64.0.2) after full decrement = %d, want 0", got)
	}
}

func TestTotalCounters(t *testing.T) {
	p := New()

	p.IncrementConnections("100.64.0.1")
	p.IncrementConnections("100.64.0.1")
	p.DecrementConnections("100.64.0.1")

	if got := p.TotalConnections(); got != 2 {
		t.Errorf("TotalConnections() = %d, want 2 (should count all, not just active)", got)
	}

	p.IncrementMessages()
	p.IncrementMessages()
	p.IncrementMessages()

	if got := p.TotalMessages(); got != 3 {
		t.Errorf("TotalMessages() = %d, want 3", got)
	}
}

func TestHttpToWS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://localhost:18800", "ws://localhost:18800"},
		{"https://gateway.example.com:443", "wss://gateway.example.com:443"},
		{"http://10.0.0.1:18800/path", "ws://10.0.0.1:18800/path"},
		{"ws://already-ws:8080", "ws://already-ws:8080"},
		{"wss://already-wss:8080", "wss://already-wss:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := httpToWS(tt.input)
			if got != tt.want {
				t.Errorf("httpToWS(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTryIncrementConnections(t *testing.T) {
	p := New()

	// Should succeed within limits
	if reason := p.TryIncrementConnections("10.0.0.1", 3, 2); reason != "" {
		t.Errorf("TryIncrementConnections() = %q, want empty", reason)
	}
	if got := p.ConnectionCount(); got != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", got)
	}

	// Second from same IP — still within per-IP limit of 2
	if reason := p.TryIncrementConnections("10.0.0.1", 3, 2); reason != "" {
		t.Errorf("TryIncrementConnections() = %q, want empty", reason)
	}

	// Third from same IP — should hit per-IP limit
	if reason := p.TryIncrementConnections("10.0.0.1", 3, 2); reason != "max_connections_per_ip" {
		t.Errorf("TryIncrementConnections() = %q, want %q", reason, "max_connections_per_ip")
	}
	// Count should NOT have incremented
	if got := p.ConnectionCount(); got != 2 {
		t.Errorf("ConnectionCount() = %d, want 2 (should not increment on rejection)", got)
	}

	// Different IP — should succeed (global count is 2, limit is 3)
	if reason := p.TryIncrementConnections("10.0.0.2", 3, 2); reason != "" {
		t.Errorf("TryIncrementConnections() = %q, want empty", reason)
	}

	// Now at global limit of 3 — another should hit global limit
	if reason := p.TryIncrementConnections("10.0.0.3", 3, 2); reason != "max_connections" {
		t.Errorf("TryIncrementConnections() = %q, want %q", reason, "max_connections")
	}
}
