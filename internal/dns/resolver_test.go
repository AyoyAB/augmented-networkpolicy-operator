package dns

import (
	"context"
	"testing"
)

func TestToCIDR(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "IPv4", addr: "1.2.3.4", want: "1.2.3.4/32"},
		{name: "IPv6", addr: "::1", want: "::1/128"},
		{name: "IPv6 full", addr: "2001:db8::1", want: "2001:db8::1/128"},
		{name: "invalid", addr: "not-an-ip", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toCIDR(tt.addr)
			if got != tt.want {
				t.Errorf("toCIDR(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestNetResolver_Resolve(t *testing.T) {
	r := NewNetResolver()
	ctx := context.Background()

	// Resolve localhost - should always work
	cidrs, err := r.Resolve(ctx, "localhost")
	if err != nil {
		t.Fatalf("Resolve(localhost) error: %v", err)
	}
	if len(cidrs) == 0 {
		t.Fatal("Resolve(localhost) returned no results")
	}

	// Check that results are sorted
	for i := 1; i < len(cidrs); i++ {
		if cidrs[i] < cidrs[i-1] {
			t.Errorf("results not sorted: %v", cidrs)
			break
		}
	}

	// Check CIDR format
	for _, cidr := range cidrs {
		if cidr != "127.0.0.1/32" && cidr != "::1/128" {
			t.Errorf("unexpected CIDR for localhost: %q", cidr)
		}
	}
}

func TestNetResolver_ResolveError(t *testing.T) {
	r := NewNetResolver()
	ctx := context.Background()

	_, err := r.Resolve(ctx, "this-hostname-definitely-does-not-exist.invalid")
	if err == nil {
		t.Fatal("expected error for non-existent hostname")
	}
}
