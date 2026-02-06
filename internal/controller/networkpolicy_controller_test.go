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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/prometheus/client_golang/prometheus/testutil"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkingv1alpha1 "github.com/ayoy/augmented-networkpolicy-operator/api/v1alpha1"
	"github.com/ayoy/augmented-networkpolicy-operator/internal/dns"
)

var _ = Describe("NetworkPolicy Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		reconciler *NetworkPolicyReconciler
		ns         *corev1.Namespace
	)

	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ns-",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		reconciler = &NetworkPolicyReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
			Resolver: &dns.MockResolver{
				Results: map[string][]string{
					"example.com":     {"93.184.216.34/32"},
					"api.example.com": {"93.184.216.35/32", "93.184.216.36/32"},
				},
			},
		}
	})

	Context("when creating a NetworkPolicy", func() {
		It("should create a standard NetworkPolicy with resolved IPs", func() {
			creationsBefore := testutil.ToFloat64(networkPolicyCreations)

			tcpProto := corev1.ProtocolTCP
			port443 := intstr.FromInt32(443)
			anp := &networkingv1alpha1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: ns.Name,
				},
				Spec: networkingv1alpha1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "web"},
					},
					Egress: []networkingv1alpha1.EgressRule{
						{
							Ports: []networkingv1alpha1.NetworkPolicyPort{
								{
									Protocol: &tcpProto,
									Port:     &port443,
								},
							},
							To: []networkingv1alpha1.EgressPeer{
								{Hostname: "example.com"},
							},
						},
					},
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				},
			}

			Expect(k8sClient.Create(ctx, anp)).To(Succeed())

			// Reconcile
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      anp.Name,
					Namespace: anp.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(defaultResolutionInterval))

			// Verify the standard NetworkPolicy was created
			var stdNP networkingv1.NetworkPolicy
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      anp.Name,
					Namespace: anp.Namespace,
				}, &stdNP)
			}, timeout, interval).Should(Succeed())

			Expect(stdNP.Spec.PodSelector).To(Equal(anp.Spec.PodSelector))
			Expect(stdNP.Spec.PolicyTypes).To(Equal(anp.Spec.PolicyTypes))
			Expect(stdNP.Spec.Egress).To(HaveLen(1))
			Expect(stdNP.Spec.Egress[0].To).To(HaveLen(1))
			Expect(stdNP.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("93.184.216.34/32"))
			Expect(stdNP.Spec.Egress[0].Ports).To(HaveLen(1))
			Expect(stdNP.Spec.Egress[0].Ports[0].Protocol).To(Equal(&tcpProto))

			// Verify owner reference
			Expect(stdNP.OwnerReferences).To(HaveLen(1))
			Expect(stdNP.OwnerReferences[0].Name).To(Equal(anp.Name))

			// Verify status was updated
			var updatedANP networkingv1alpha1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      anp.Name,
				Namespace: anp.Namespace,
			}, &updatedANP)).To(Succeed())

			Expect(updatedANP.Status.ResolvedAddresses).To(HaveKey("example.com"))
			Expect(updatedANP.Status.ResolvedAddresses["example.com"]).To(ContainElement("93.184.216.34/32"))
			Expect(updatedANP.Status.Conditions).To(HaveLen(1))
			Expect(updatedANP.Status.Conditions[0].Type).To(Equal(conditionTypeReady))
			Expect(updatedANP.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))

			// Verify creation metric incremented
			Expect(testutil.ToFloat64(networkPolicyCreations)).To(Equal(creationsBefore + 1))
		})

		It("should handle multiple peers in a single egress rule", func() {
			anp := &networkingv1alpha1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-peer-policy",
					Namespace: ns.Name,
				},
				Spec: networkingv1alpha1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{},
					Egress: []networkingv1alpha1.EgressRule{
						{
							To: []networkingv1alpha1.EgressPeer{
								{Hostname: "api.example.com"},
							},
						},
					},
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				},
			}

			Expect(k8sClient.Create(ctx, anp)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      anp.Name,
					Namespace: anp.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			var stdNP networkingv1.NetworkPolicy
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      anp.Name,
					Namespace: anp.Namespace,
				}, &stdNP)
			}, timeout, interval).Should(Succeed())

			// api.example.com resolves to 2 IPs, so 2 peers
			Expect(stdNP.Spec.Egress).To(HaveLen(1))
			Expect(stdNP.Spec.Egress[0].To).To(HaveLen(2))
		})
	})

	Context("when updating a NetworkPolicy", func() {
		It("should update the standard NetworkPolicy when spec changes", func() {
			dnsChangesBefore := testutil.ToFloat64(dnsNameChanges)

			tcpProto := corev1.ProtocolTCP
			port443 := intstr.FromInt32(443)
			anp := &networkingv1alpha1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "update-policy",
					Namespace: ns.Name,
				},
				Spec: networkingv1alpha1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "web"},
					},
					Egress: []networkingv1alpha1.EgressRule{
						{
							Ports: []networkingv1alpha1.NetworkPolicyPort{
								{Protocol: &tcpProto, Port: &port443},
							},
							To: []networkingv1alpha1.EgressPeer{
								{Hostname: "example.com"},
							},
						},
					},
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				},
			}

			Expect(k8sClient.Create(ctx, anp)).To(Succeed())

			// First reconcile
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: anp.Name, Namespace: anp.Namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Update the ANP to use a different port
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: anp.Name, Namespace: anp.Namespace}, anp)).To(Succeed())
			port8443 := intstr.FromInt32(8443)
			anp.Spec.Egress[0].Ports[0].Port = &port8443
			Expect(k8sClient.Update(ctx, anp)).To(Succeed())

			// Second reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: anp.Name, Namespace: anp.Namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the standard NetworkPolicy was updated
			var stdNP networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      anp.Name,
				Namespace: anp.Namespace,
			}, &stdNP)).To(Succeed())

			Expect(stdNP.Spec.Egress[0].Ports[0].Port.IntVal).To(Equal(int32(8443)))

			// Verify dns change metric incremented
			Expect(testutil.ToFloat64(dnsNameChanges)).To(Equal(dnsChangesBefore + 1))
		})
	})

	Context("when a NetworkPolicy is deleted", func() {
		It("should return without error for non-existent resources", func() {
			deletionsBefore := testutil.ToFloat64(networkPolicyDeletions)

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "non-existent",
					Namespace: ns.Name,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify deletion metric incremented
			Expect(testutil.ToFloat64(networkPolicyDeletions)).To(Equal(deletionsBefore + 1))
		})
	})

	Context("when custom resolution interval is set", func() {
		It("should requeue with the specified interval", func() {
			customInterval := metav1.Duration{Duration: 10 * time.Minute}
			anp := &networkingv1alpha1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "custom-interval-policy",
					Namespace: ns.Name,
				},
				Spec: networkingv1alpha1.NetworkPolicySpec{
					PodSelector:        metav1.LabelSelector{},
					Egress:             []networkingv1alpha1.EgressRule{},
					PolicyTypes:        []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
					ResolutionInterval: &customInterval,
				},
			}

			Expect(k8sClient.Create(ctx, anp)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      anp.Name,
					Namespace: anp.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(10 * time.Minute))
		})
	})
})
