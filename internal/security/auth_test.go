package security

import "testing"

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer my-secret-token", "my-secret-token"},
		{"Bearer abc123", "abc123"},
		{"Bearer ", ""},    // empty token after prefix
		{"bearer abc", ""}, // wrong case
		{"Basic abc123", ""},
		{"", ""},
		{"BearerNoSpace", ""},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := ExtractBearerToken(tt.header)
			if got != tt.want {
				t.Errorf("ExtractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestTokenMatch(t *testing.T) {
	tests := []struct {
		name     string
		provided string
		expected string
		want     bool
	}{
		{"matching tokens", "my-token", "my-token", true},
		{"different tokens", "wrong", "right", false},
		{"empty provided", "", "token", false},
		{"empty expected", "token", "", false},
		{"both empty", "", "", false},
		{"different lengths", "short", "much-longer-token", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TokenMatch(tt.provided, tt.expected)
			if got != tt.want {
				t.Errorf("TokenMatch(%q, %q) = %v, want %v", tt.provided, tt.expected, got, tt.want)
			}
		})
	}
}

func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"100.64.0.1:8080", "100.64.0.1"},
		{"192.168.1.1:443", "192.168.1.1"},
		{"[::1]:8080", "::1"},
		{"[fd7a:115c:a1e0::1]:8080", "fd7a:115c:a1e0::1"},
		{"127.0.0.1:0", "127.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := ExtractClientIP(tt.addr)
			if got != tt.want {
				t.Errorf("ExtractClientIP(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}
