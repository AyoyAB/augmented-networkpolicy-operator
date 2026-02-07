package dns

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	ipFilteredTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "augmented_networkpolicy_ip_filtered_total",
		Help: "Total number of resolved IP addresses filtered out by IP filter",
	}, []string{"hostname"})

	dnsResolutionChangesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "augmented_networkpolicy_dns_resolution_changes_total",
		Help: "Total number of times DNS resolution results changed for a hostname",
	}, []string{"hostname"})
)

func init() {
	metrics.Registry.MustRegister(ipFilteredTotal, dnsResolutionChangesTotal)
}

// IPFilter filters IP addresses against whitelist and blacklist CIDRs.
// Blacklist always takes precedence over whitelist.
type IPFilter struct {
	whitelist []*net.IPNet
	blacklist []*net.IPNet
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
		whitelist: wl,
		blacklist: bl,
	}, nil
}

// IsAllowed returns whether a CIDR string is allowed by the filter.
// Blacklist takes precedence over whitelist.
// If whitelist is non-empty, only whitelisted IPs are allowed (unless blacklisted).
// Unparseable CIDRs are allowed (fail-open).
func (f *IPFilter) IsAllowed(cidr string) bool {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		// Fail-closed for unparseable CIDRs
		return false
	}

	// Check blacklist
	for _, n := range f.blacklist {
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
// It also tracks DNS resolution changes per hostname for rebinding detection.
type FilteringResolver struct {
	Inner  Resolver
	Filter *IPFilter
	Logger logr.Logger

	mu       sync.Mutex
	lastSeen map[string][]string // hostname → previous filtered CIDRs
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
			ipFilteredTotal.WithLabelValues(hostname).Inc()
		}
	}

	// DNS change detection
	r.mu.Lock()
	if r.lastSeen == nil {
		r.lastSeen = make(map[string][]string)
	}
	prev, seen := r.lastSeen[hostname]
	if seen && !stringSlicesEqual(prev, allowed) {
		r.Logger.Info("DNS resolution change detected",
			"hostname", hostname,
			"previous", prev,
			"current", allowed,
		)
		dnsResolutionChangesTotal.WithLabelValues(hostname).Inc()
	}
	r.lastSeen[hostname] = copyAndSort(allowed)
	r.mu.Unlock()

	return allowed, nil
}

func copyAndSort(s []string) []string {
	c := make([]string, len(s))
	copy(c, s)
	sort.Strings(c)
	return c
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	bs := copyAndSort(b)
	for i, v := range a {
		if v != bs[i] {
			return false
		}
	}
	return true
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
