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

package controller

import (
	"context"
	"fmt"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/ayoy/augmented-networkpolicy-operator/api/v1alpha1"
	"github.com/ayoy/augmented-networkpolicy-operator/internal/dns"
)

const (
	defaultResolutionInterval = 5 * time.Minute
	minResolutionInterval     = 30 * time.Second
	conditionTypeReady        = "Ready"
)

// NetworkPolicyReconciler reconciles a NetworkPolicy object.
type NetworkPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Resolver dns.Resolver
}

// +kubebuilder:rbac:groups=networking.ayoy.se,resources=networkpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.ayoy.se,resources=networkpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.ayoy.se,resources=networkpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile handles reconciliation of NetworkPolicy custom resources.
func (r *NetworkPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the custom NetworkPolicy
	var anp networkingv1alpha1.NetworkPolicy
	if err := r.Get(ctx, req.NamespacedName, &anp); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("NetworkPolicy resource not found, likely deleted")
			networkPolicyDeletions.Inc()
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get NetworkPolicy: %w", err)
	}

	// Resolve hostnames and build the standard NetworkPolicy
	resolvedAddresses := make(map[string][]string)
	var egressRules []networkingv1.NetworkPolicyEgressRule
	var resolutionErrors []string

	for _, rule := range anp.Spec.Egress {
		var peers []networkingv1.NetworkPolicyPeer
		for _, to := range rule.To {
			cidrs, err := r.Resolver.Resolve(ctx, to.Hostname)
			if err != nil {
				logger.Error(err, "failed to resolve hostname", "hostname", to.Hostname)
				resolutionErrors = append(resolutionErrors, fmt.Sprintf("failed to resolve %q: %v", to.Hostname, err))
				continue
			}
			resolvedAddresses[to.Hostname] = cidrs
			for _, cidr := range cidrs {
				peers = append(peers, networkingv1.NetworkPolicyPeer{
					IPBlock: &networkingv1.IPBlock{
						CIDR: cidr,
					},
				})
			}
		}

		// Convert ports
		var ports []networkingv1.NetworkPolicyPort
		for _, p := range rule.Ports {
			port := networkingv1.NetworkPolicyPort{
				Protocol: p.Protocol,
				Port:     p.Port,
				EndPort:  p.EndPort,
			}
			ports = append(ports, port)
		}

		egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{
			Ports: ports,
			To:    peers,
		})
	}

	// Build the desired standard NetworkPolicy
	desired := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      anp.Name,
			Namespace: anp.Namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: anp.Spec.PodSelector,
			Egress:      egressRules,
			PolicyTypes: anp.Spec.PolicyTypes,
		},
	}

	// Set owner reference for automatic garbage collection
	if err := controllerutil.SetControllerReference(&anp, desired, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Create or update the standard NetworkPolicy
	existing := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("creating standard NetworkPolicy", "name", desired.Name)
			if err := r.Create(ctx, desired); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create NetworkPolicy: %w", err)
			}
			networkPolicyCreations.Inc()
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to get existing NetworkPolicy: %w", err)
		}
	} else {
		// Update if spec changed
		if !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
			existing.Spec = desired.Spec
			logger.Info("updating standard NetworkPolicy", "name", desired.Name)
			if err := r.Update(ctx, existing); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update NetworkPolicy: %w", err)
			}
			dnsNameChanges.Inc()
		}
	}

	// Update status
	condition := metav1.Condition{
		Type:               conditionTypeReady,
		ObservedGeneration: anp.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if len(resolutionErrors) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ResolutionFailed"
		condition.Message = fmt.Sprintf("failed to resolve some hostnames: %v", resolutionErrors)
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "Reconciled"
		condition.Message = "All hostnames resolved successfully"
	}

	anp.Status.ResolvedAddresses = resolvedAddresses
	setCondition(&anp.Status.Conditions, condition)

	if err := r.Status().Update(ctx, &anp); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	// Requeue for DNS re-resolution
	interval := defaultResolutionInterval
	if anp.Spec.ResolutionInterval != nil {
		interval = anp.Spec.ResolutionInterval.Duration
	}
	if interval < minResolutionInterval {
		logger.Info("resolutionInterval too low, using minimum", "requested", interval, "minimum", minResolutionInterval)
		interval = minResolutionInterval
	}

	return ctrl.Result{RequeueAfter: interval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.NetworkPolicy{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Complete(r)
}

// setCondition sets a condition on the list, replacing any existing condition of the same type.
func setCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	for i, existing := range *conditions {
		if existing.Type == condition.Type {
			if existing.Status != condition.Status {
				(*conditions)[i] = condition
			} else {
				// Keep the existing LastTransitionTime if status hasn't changed
				condition.LastTransitionTime = existing.LastTransitionTime
				(*conditions)[i] = condition
			}
			return
		}
	}
	*conditions = append(*conditions, condition)
}
