package security

import "testing"

func TestIsTailscaleIP(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		// Valid Tailscale IPv4 (100.64.0.0/10 = 100.64.0.0 – 100.127.255.255)
		{"100.64.0.1:8080", true},
		{"100.100.100.100:8080", true},
		{"100.101.102.103:8080", true},
		{"100.127.255.255:8080", true},

		// Edge of Tailscale IPv4 range
		{"100.64.0.0:8080", true},

		// Outside Tailscale IPv4 range
		{"100.63.255.255:8080", false},
		{"100.128.0.0:8080", false},
		{"192.168.1.1:8080", false},
		{"10.0.0.1:8080", false},
		{"172.16.0.1:8080", false},
		{"8.8.8.8:8080", false},
		{"127.0.0.1:8080", false},

		// Valid Tailscale IPv6 (fd7a:115c:a1e0::/48)
		{"[fd7a:115c:a1e0::1]:8080", true},
		{"[fd7a:115c:a1e0:ab12::1]:8080", true},
		{"[fd7a:115c:a1e0:ffff:ffff:ffff:ffff:ffff]:8080", true},

		// Outside Tailscale IPv6 range
		{"[fd7a:115c:a1e1::1]:8080", false},
		{"[fd7a:115d::1]:8080", false},
		{"[::1]:8080", false},
		{"[2001:db8::1]:8080", false},

		// Invalid addresses
		{"not-an-address", false},
		{"", false},
		{"100.64.0.1", false}, // no port
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := IsTailscaleIP(tt.addr)
			if got != tt.want {
				t.Errorf("IsTailscaleIP(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}
