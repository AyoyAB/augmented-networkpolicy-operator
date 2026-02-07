package dns

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodCIDRProvider periodically fetches pod CIDRs from node specs and updates
// the dynamic blacklist of an IPFilter.
type PodCIDRProvider struct {
	Client   client.Reader
	Filter   *IPFilter
	Logger   logr.Logger
	Interval time.Duration
}

// Start implements manager.Runnable. It performs an initial fetch of pod CIDRs,
// then refreshes at the configured interval until the context is cancelled.
func (p *PodCIDRProvider) Start(ctx context.Context) error {
	p.refresh(ctx)

	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.refresh(ctx)
		}
	}
}

func (p *PodCIDRProvider) refresh(ctx context.Context) {
	var nodeList corev1.NodeList
	if err := p.Client.List(ctx, &nodeList); err != nil {
		p.Logger.Error(err, "failed to list nodes for pod CIDR detection")
		return
	}

	seen := make(map[string]struct{})
	var cidrs []string
	for _, node := range nodeList.Items {
		// Prefer PodCIDRs (dual-stack), fall back to PodCIDR
		podCIDRs := node.Spec.PodCIDRs
		if len(podCIDRs) == 0 && node.Spec.PodCIDR != "" {
			podCIDRs = []string{node.Spec.PodCIDR}
		}
		for _, cidr := range podCIDRs {
			if _, ok := seen[cidr]; !ok {
				seen[cidr] = struct{}{}
				cidrs = append(cidrs, cidr)
			}
		}
	}

	if err := p.Filter.SetDynamicBlacklist(cidrs); err != nil {
		p.Logger.Error(err, "failed to set dynamic blacklist from pod CIDRs")
		return
	}

	p.Logger.Info("updated pod CIDR blacklist", "cidrs", cidrs)
}
