package allowlist

import (
	"net"
	"strings"
)

// Allowlist defines allowed IP ranges.
type Allowlist struct {
	allowAll bool
	entries  []*net.IPNet
}

// Parse builds an allowlist from a comma-separated list of IPs or CIDRs.
func Parse(value string) (Allowlist, error) {
	if strings.TrimSpace(value) == "" {
		return Allowlist{allowAll: true}, nil
	}
	parts := strings.Split(value, ",")
	entries := make([]*net.IPNet, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "/") {
			_, ipnet, err := net.ParseCIDR(trimmed)
			if err != nil {
				return Allowlist{}, err
			}
			entries = append(entries, ipnet)
			continue
		}
		ip := net.ParseIP(trimmed)
		if ip == nil {
			return Allowlist{}, &net.ParseError{Type: "IP address", Text: trimmed}
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		mask := net.CIDRMask(bits, bits)
		entries = append(entries, &net.IPNet{IP: ip, Mask: mask})
	}
	return Allowlist{entries: entries}, nil
}

// Allows returns true if the IP is within the allowlist.
func (a Allowlist) Allows(ip net.IP) bool {
	if a.allowAll {
		return true
	}
	if ip == nil {
		return false
	}
	for _, entry := range a.entries {
		if entry.Contains(ip) {
			return true
		}
	}
	return false
}
