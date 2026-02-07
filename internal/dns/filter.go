package dns

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var ipFilteredTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "augmented_networkpolicy_ip_filtered_total",
	Help: "Total number of resolved IP addresses filtered out by IP filter",
}, []string{"hostname", "cidr"})

func init() {
	metrics.Registry.MustRegister(ipFilteredTotal)
}

// IPFilter filters IP addresses against whitelist and blacklist CIDRs.
// Blacklist always takes precedence over whitelist.
type IPFilter struct {
	whitelist       []*net.IPNet
	staticBlacklist []*net.IPNet

	mu               sync.RWMutex
	dynamicBlacklist []*net.IPNet
}

// NewIPFilter creates an IPFilter from whitelist and blacklist CIDR strings.
func NewIPFilter(whitelist, blacklist []string) (*IPFilter, error) {
	wl, err := parseCIDRs(whitelist)
	if err != nil {
		return nil, fmt.Errorf("invalid whitelist CIDR: %w", err)
	}
	bl, err := parseCIDRs(blacklist)
	if err != nil {
		return nil, fmt.Errorf("invalid blacklist CIDR: %w", err)
	}
	return &IPFilter{
		whitelist:       wl,
		staticBlacklist: bl,
	}, nil
}

// SetDynamicBlacklist replaces the dynamic blacklist with the given CIDRs.
func (f *IPFilter) SetDynamicBlacklist(cidrs []string) error {
	nets, err := parseCIDRs(cidrs)
	if err != nil {
		return fmt.Errorf("invalid dynamic blacklist CIDR: %w", err)
	}
	f.mu.Lock()
	f.dynamicBlacklist = nets
	f.mu.Unlock()
	return nil
}

// IsAllowed returns whether a CIDR string is allowed by the filter.
// Blacklist (static + dynamic) takes precedence over whitelist.
// If whitelist is non-empty, only whitelisted IPs are allowed (unless blacklisted).
// Unparseable CIDRs are allowed (fail-open).
func (f *IPFilter) IsAllowed(cidr string) bool {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		// Fail-open for unparseable CIDRs
		return true
	}

	// Check static blacklist
	for _, n := range f.staticBlacklist {
		if n.Contains(ip) {
			return false
		}
	}

	// Check dynamic blacklist
	f.mu.RLock()
	dynBL := f.dynamicBlacklist
	f.mu.RUnlock()
	for _, n := range dynBL {
		if n.Contains(ip) {
			return false
		}
	}

	// If whitelist is configured, IP must match at least one entry
	if len(f.whitelist) > 0 {
		for _, n := range f.whitelist {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}

	return true
}

// FilteringResolver wraps a Resolver and filters results through an IPFilter.
type FilteringResolver struct {
	Inner  Resolver
	Filter *IPFilter
	Logger logr.Logger
}

// Resolve resolves a hostname and filters results through the IPFilter.
func (r *FilteringResolver) Resolve(ctx context.Context, hostname string) ([]string, error) {
	cidrs, err := r.Inner.Resolve(ctx, hostname)
	if err != nil {
		return nil, err
	}

	allowed := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		if r.Filter.IsAllowed(cidr) {
			allowed = append(allowed, cidr)
		} else {
			r.Logger.Info("filtered resolved IP", "hostname", hostname, "cidr", cidr)
			ipFilteredTotal.WithLabelValues(hostname, cidr).Inc()
		}
	}
	return allowed, nil
}

func parseCIDRs(cidrs []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("parsing %q: %w", cidr, err)
		}
		nets = append(nets, n)
	}
	return nets, nil
}
