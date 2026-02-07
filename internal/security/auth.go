package security

import (
	"crypto/subtle"
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

// TokenMatch uses constant-time comparison to prevent timing attacks.
func TokenMatch(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
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
