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
		{"bearer abc", "abc"}, // case-insensitive prefix
		{"Basic abc123", ""},
		{"", ""},
		{"BearerNoSpace", ""},
		{"Bearer token  ", "token"},   // trailing whitespace trimmed
		{"Bearer  token ", "token"},   // leading+trailing whitespace trimmed
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

