/*
Copyright 2024 ayoy.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-based authentication works
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	networkingv1alpha1 "github.com/ayoy/augmented-networkpolicy-operator/api/v1alpha1"
	"github.com/ayoy/augmented-networkpolicy-operator/internal/controller"
	"github.com/ayoy/augmented-networkpolicy-operator/internal/dns"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(networkingv1alpha1.AddToScheme(scheme))
	utilruntime.Must(networkingv1.AddToScheme(scheme))
}

// stringSliceFlag implements flag.Value for comma-separated string slices.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(val string) error {
	for _, v := range strings.Split(val, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			*s = append(*s, v)
		}
	}
	return nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var ipBlacklist stringSliceFlag
	var ipWhitelist stringSliceFlag
	var autoDetectPodCIDR bool
	var blacklistSet bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable metrics.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics server")
	flag.Var(&ipBlacklist, "ip-blacklist",
		"CIDRs to block from resolved IPs (comma-separated, repeatable). "+
			"Default: 169.254.169.254/32,127.0.0.0/8")
	flag.Var(&ipWhitelist, "ip-whitelist",
		"CIDRs to allow (comma-separated, repeatable). "+
			"When set, only matching IPs pass (unless also blacklisted).")
	flag.BoolVar(&autoDetectPodCIDR, "auto-detect-pod-cidr", false,
		"Auto-detect and blacklist pod network CIDRs from node specs")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Check if --ip-blacklist was explicitly provided
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "ip-blacklist" {
			blacklistSet = true
		}
	})

	// Apply default blacklist if not explicitly provided
	if !blacklistSet {
		ipBlacklist = stringSliceFlag{"169.254.169.254/32", "127.0.0.0/8"}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Conditions Denial of Service attack that was discovered in October 2023.
	// https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	metricsOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	// Set up IP filter
	ipFilter, err := dns.NewIPFilter([]string(ipWhitelist), []string(ipBlacklist))
	if err != nil {
		setupLog.Error(err, "invalid IP filter configuration")
		os.Exit(1)
	}

	var resolver dns.Resolver = &dns.FilteringResolver{
		Inner:  dns.NewNetResolver(),
		Filter: ipFilter,
		Logger: ctrl.Log.WithName("ip-filter"),
	}

	setupLog.Info("IP filter configured",
		"blacklist", fmt.Sprintf("%v", []string(ipBlacklist)),
		"whitelist", fmt.Sprintf("%v", []string(ipWhitelist)),
		"autoDetectPodCIDR", autoDetectPodCIDR,
	)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOptions,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "networkpolicy.ayoy.se",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if autoDetectPodCIDR {
		provider := &dns.PodCIDRProvider{
			Client:   mgr.GetClient(),
			Filter:   ipFilter,
			Logger:   ctrl.Log.WithName("pod-cidr-provider"),
			Interval: 5 * time.Minute,
		}
		if err := mgr.Add(provider); err != nil {
			setupLog.Error(err, "unable to add pod CIDR provider")
			os.Exit(1)
		}
	}

	if err = (&controller.NetworkPolicyReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NetworkPolicy")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
