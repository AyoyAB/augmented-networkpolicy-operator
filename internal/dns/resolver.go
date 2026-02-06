package dns

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
)

// Resolver resolves hostnames to IP addresses with CIDR notation.
type Resolver interface {
	// Resolve resolves a hostname to a list of IP addresses in CIDR notation
	// (e.g., "1.2.3.4/32" for IPv4 or "::1/128" for IPv6).
	Resolve(ctx context.Context, hostname string) ([]string, error)
}

// NetResolver uses net.DefaultResolver to resolve hostnames.
type NetResolver struct{}

// NewNetResolver returns a new NetResolver.
func NewNetResolver() *NetResolver {
	return &NetResolver{}
}

// Resolve resolves a hostname to a sorted list of IP addresses in CIDR notation.
func (r *NetResolver) Resolve(ctx context.Context, hostname string) ([]string, error) {
	addrs, err := net.DefaultResolver.LookupHost(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve hostname %q: %w", hostname, err)
	}

	cidrs := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		cidr := toCIDR(addr)
		if cidr != "" {
			cidrs = append(cidrs, cidr)
		}
	}

	sort.Strings(cidrs)
	return cidrs, nil
}

// toCIDR converts an IP address string to CIDR notation.
// Returns /32 for IPv4 and /128 for IPv6.
func toCIDR(addr string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return ""
	}
	if strings.Contains(addr, ":") {
		return addr + "/128"
	}
	return addr + "/32"
}
