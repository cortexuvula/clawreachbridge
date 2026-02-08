package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"strings"
)

// ExtractBearerToken parses "Bearer <token>" from the Authorization header.
func ExtractBearerToken(authHeader string) string {
	const prefix = "Bearer "
	if len(authHeader) > len(prefix) && authHeader[:len(prefix)] == prefix {
		return authHeader[len(prefix):]
	}
	return ""
}

// TokenMatch uses HMAC comparison to prevent timing attacks including length oracle.
func TokenMatch(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	// HMAC with a fixed key normalizes both values to the same length,
	// preventing the length leak in subtle.ConstantTimeCompare.
	key := []byte("clawreach-token-compare")
	h1 := hmac.New(sha256.New, key)
	h1.Write([]byte(provided))
	h2 := hmac.New(sha256.New, key)
	h2.Write([]byte(expected))
	return hmac.Equal(h1.Sum(nil), h2.Sum(nil))
}

// ExtractClientIP strips the port from RemoteAddr ("ip:port" → "ip").
func ExtractClientIP(remoteAddr string) string {
	// Handle IPv6 addresses like "[::1]:8080"
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		host := remoteAddr[:idx]
		// Remove brackets from IPv6
		host = strings.TrimPrefix(host, "[")
		host = strings.TrimSuffix(host, "]")
		return host
	}
	return remoteAddr
}
