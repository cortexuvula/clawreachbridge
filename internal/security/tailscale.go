package security

import "net"

// Package-level vars — parsed once at init, not per-request
var (
	tailscaleIPv4 = mustParseCIDR("100.64.0.0/10")       // Tailscale CGNAT range
	tailscaleIPv6 = mustParseCIDR("fd7a:115c:a1e0::/48") // Tailscale ULA range
)

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// IsTailscaleIP checks whether the given address (host:port) belongs to the
// Tailscale network range (IPv4: 100.64.0.0/10, IPv6: fd7a:115c:a1e0::/48).
func IsTailscaleIP(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return tailscaleIPv4.Contains(ip) || tailscaleIPv6.Contains(ip)
}
