package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	networkPolicyCreations = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "augmented_networkpolicy_creations_total",
		Help: "Total number of standard NetworkPolicies created",
	})

	networkPolicyDeletions = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "augmented_networkpolicy_deletions_total",
		Help: "Total number of augmented NetworkPolicies detected as deleted",
	})

	dnsNameChanges = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "augmented_networkpolicy_dns_changes_total",
		Help: "Total number of standard NetworkPolicy spec updates due to DNS changes",
	})
)

func init() {
	metrics.Registry.MustRegister(
		networkPolicyCreations,
		networkPolicyDeletions,
		dnsNameChanges,
	)
}
