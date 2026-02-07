package dns

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestIPFilter_IsAllowed(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		blacklist []string
		cidr      string
		want      bool
	}{
		{
			name: "no filters allows everything",
			cidr: "1.2.3.4/32",
			want: true,
		},
		{
			name:      "blacklist blocks exact match",
			blacklist: []string{"169.254.169.254/32"},
			cidr:      "169.254.169.254/32",
			want:      false,
		},
		{
			name:      "blacklist blocks by range",
			blacklist: []string{"127.0.0.0/8"},
			cidr:      "127.0.0.1/32",
			want:      false,
		},
		{
			name:      "blacklist allows non-matching",
			blacklist: []string{"127.0.0.0/8"},
			cidr:      "8.8.8.8/32",
			want:      true,
		},
		{
			name:      "whitelist allows matching",
			whitelist: []string{"10.0.0.0/8"},
			cidr:      "10.1.2.3/32",
			want:      true,
		},
		{
			name:      "whitelist blocks non-matching",
			whitelist: []string{"10.0.0.0/8"},
			cidr:      "8.8.8.8/32",
			want:      false,
		},
		{
			name:      "blacklist overrides whitelist",
			whitelist: []string{"10.0.0.0/8"},
			blacklist: []string{"10.0.1.0/24"},
			cidr:      "10.0.1.5/32",
			want:      false,
		},
		{
			name:      "whitelist+blacklist allows non-blacklisted whitelist IP",
			whitelist: []string{"10.0.0.0/8"},
			blacklist: []string{"10.0.1.0/24"},
			cidr:      "10.0.2.5/32",
			want:      true,
		},
		{
			name:      "IPv6 blacklist blocks",
			blacklist: []string{"::1/128"},
			cidr:      "::1/128",
			want:      false,
		},
		{
			name:      "IPv6 whitelist allows matching",
			whitelist: []string{"2001:db8::/32"},
			cidr:      "2001:db8::1/128",
			want:      true,
		},
		{
			name:      "IPv6 whitelist blocks non-matching",
			whitelist: []string{"2001:db8::/32"},
			cidr:      "2001:db9::1/128",
			want:      false,
		},
		{
			name: "invalid CIDR fails open",
			cidr: "not-a-cidr",
			want: true,
		},
		{
			name:      "invalid CIDR fails open even with blacklist",
			blacklist: []string{"0.0.0.0/0"},
			cidr:      "not-a-cidr",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := NewIPFilter(tt.whitelist, tt.blacklist)
			if err != nil {
				t.Fatalf("NewIPFilter() error: %v", err)
			}
			got := f.IsAllowed(tt.cidr)
			if got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.cidr, got, tt.want)
			}
		})
	}
}

func TestNewIPFilter_InvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		blacklist []string
	}{
		{
			name:      "invalid whitelist",
			whitelist: []string{"not-a-cidr"},
		},
		{
			name:      "invalid blacklist",
			blacklist: []string{"not-a-cidr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewIPFilter(tt.whitelist, tt.blacklist)
			if err == nil {
				t.Fatal("expected error for invalid CIDR input")
			}
		})
	}
}

type stubResolver struct {
	results map[string][]string
	err     error
}

func (s *stubResolver) Resolve(_ context.Context, hostname string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.results[hostname], nil
}

func TestFilteringResolver(t *testing.T) {
	inner := &stubResolver{
		results: map[string][]string{
			"example.com": {"1.2.3.4/32", "127.0.0.1/32", "10.0.0.1/32"},
		},
	}

	f, err := NewIPFilter(nil, []string{"127.0.0.0/8"})
	if err != nil {
		t.Fatalf("NewIPFilter() error: %v", err)
	}

	r := &FilteringResolver{
		Inner:  inner,
		Filter: f,
		Logger: logr.Discard(),
	}

	cidrs, err := r.Resolve(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	// 127.0.0.1/32 should be filtered out
	expected := []string{"1.2.3.4/32", "10.0.0.1/32"}
	if len(cidrs) != len(expected) {
		t.Fatalf("Resolve() returned %d results, want %d: %v", len(cidrs), len(expected), cidrs)
	}
	for i, cidr := range cidrs {
		if cidr != expected[i] {
			t.Errorf("Resolve()[%d] = %q, want %q", i, cidr, expected[i])
		}
	}
}

func TestFilteringResolver_WithWhitelist(t *testing.T) {
	inner := &stubResolver{
		results: map[string][]string{
			"example.com": {"1.2.3.4/32", "10.0.0.1/32", "192.168.1.1/32"},
		},
	}

	f, err := NewIPFilter([]string{"10.0.0.0/8"}, []string{"10.0.1.0/24"})
	if err != nil {
		t.Fatalf("NewIPFilter() error: %v", err)
	}

	r := &FilteringResolver{
		Inner:  inner,
		Filter: f,
		Logger: logr.Discard(),
	}

	cidrs, err := r.Resolve(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	// Only 10.0.0.1/32 should pass (whitelisted, not blacklisted)
	// 1.2.3.4 not in whitelist, 192.168.1.1 not in whitelist
	expected := []string{"10.0.0.1/32"}
	if len(cidrs) != len(expected) {
		t.Fatalf("Resolve() returned %d results, want %d: %v", len(cidrs), len(expected), cidrs)
	}
	if cidrs[0] != expected[0] {
		t.Errorf("Resolve()[0] = %q, want %q", cidrs[0], expected[0])
	}
}

func getCounterValue(c prometheus.Counter) float64 {
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		return 0
	}
	return m.GetCounter().GetValue()
}

func TestFilteringResolver_DNSChangeDetection(t *testing.T) {
	inner := &stubResolver{
		results: map[string][]string{
			"example.com": {"1.2.3.4/32"},
		},
	}

	f, err := NewIPFilter(nil, nil)
	if err != nil {
		t.Fatalf("NewIPFilter() error: %v", err)
	}

	r := &FilteringResolver{
		Inner:  inner,
		Filter: f,
		Logger: logr.Discard(),
	}

	// First resolution — establishes baseline, no change expected
	_, err = r.Resolve(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	before := getCounterValue(dnsResolutionChangesTotal.WithLabelValues("example.com"))

	// Second resolution with same results — no increment
	_, err = r.Resolve(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	after := getCounterValue(dnsResolutionChangesTotal.WithLabelValues("example.com"))
	if after != before {
		t.Errorf("expected no metric increment for same results, got %v → %v", before, after)
	}

	// Third resolution with different results — should increment
	inner.results["example.com"] = []string{"5.6.7.8/32"}
	_, err = r.Resolve(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	afterChange := getCounterValue(dnsResolutionChangesTotal.WithLabelValues("example.com"))
	if afterChange != after+1 {
		t.Errorf("expected metric increment after DNS change, got %v → %v", after, afterChange)
	}
}
