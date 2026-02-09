//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	anpv1alpha1 "github.com/AyoyAB/augmented-networkpolicy-operator/api/v1alpha1"
)

const testNamespace = "default"

func tcpProtocol() *corev1.Protocol {
	p := corev1.ProtocolTCP
	return &p
}

func portInt(port int) *intstr.IntOrString {
	p := intstr.FromInt32(int32(port))
	return &p
}

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

var _ = Describe("CRD", func() {
	It("should have the NetworkPolicy CRD established", func() {
		ctx := context.Background()
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "networkpolicies.networking.ayoy.se"}, crd)).To(Succeed())

		established := findCondition2(crd.Status.Conditions, "Established")
		Expect(established).NotTo(BeNil())
		Expect(string(established.Status)).To(Equal("True"))
	})
})

// findCondition2 finds a CRD condition (different type than metav1.Condition)
func findCondition2(conditions []apiextensionsv1.CustomResourceDefinitionCondition, condType string) *apiextensionsv1.CustomResourceDefinitionCondition {
	for i := range conditions {
		if string(conditions[i].Type) == condType {
			return &conditions[i]
		}
	}
	return nil
}

var _ = Describe("NetworkPolicy", Ordered, func() {
	ctx := context.Background()

	AfterAll(func() {
		for _, name := range []string{"test-create", "test-update", "test-delete", "test-metrics"} {
			cr := &anpv1alpha1.NetworkPolicy{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, cr)
			if err == nil {
				_ = k8sClient.Delete(ctx, cr)
			}
		}
	})

	It("should create an augmented NetworkPolicy and produce a standard NetworkPolicy", func() {
		cr := &anpv1alpha1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-create",
				Namespace: testNamespace,
			},
			Spec: anpv1alpha1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "web"},
				},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				Egress: []anpv1alpha1.EgressRule{
					{
						Ports: []anpv1alpha1.NetworkPolicyPort{
							{Protocol: tcpProtocol(), Port: portInt(443)},
						},
						To: []anpv1alpha1.EgressPeer{
							{Hostname: "kubernetes.default.svc.cluster.local"},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		// Assert standard NetworkPolicy is created
		Eventually(func(g Gomega) {
			var stdNP networkingv1.NetworkPolicy
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-create", Namespace: testNamespace}, &stdNP)).To(Succeed())
			g.Expect(stdNP.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", "web"))
			g.Expect(stdNP.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))
		}, "60s", "2s").Should(Succeed())

		// Assert augmented NP status is Ready
		Eventually(func(g Gomega) {
			var got anpv1alpha1.NetworkPolicy
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-create", Namespace: testNamespace}, &got)).To(Succeed())
			readyCond := findCondition(got.Status.Conditions, "Ready")
			g.Expect(readyCond).NotTo(BeNil())
			g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		}, "60s", "2s").Should(Succeed())
	})

	It("should update the standard NetworkPolicy when the augmented one changes", func() {
		cr := &anpv1alpha1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-update",
				Namespace: testNamespace,
			},
			Spec: anpv1alpha1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "web"},
				},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				Egress: []anpv1alpha1.EgressRule{
					{
						Ports: []anpv1alpha1.NetworkPolicyPort{
							{Protocol: tcpProtocol(), Port: portInt(443)},
						},
						To: []anpv1alpha1.EgressPeer{
							{Hostname: "kubernetes.default.svc.cluster.local"},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		// Wait for initial std NP with port 443
		Eventually(func(g Gomega) {
			var stdNP networkingv1.NetworkPolicy
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-update", Namespace: testNamespace}, &stdNP)).To(Succeed())
			g.Expect(stdNP.Spec.Egress).NotTo(BeEmpty())
			g.Expect(stdNP.Spec.Egress[0].Ports).NotTo(BeEmpty())
			g.Expect(stdNP.Spec.Egress[0].Ports[0].Port.IntValue()).To(Equal(443))
		}, "60s", "2s").Should(Succeed())

		// Update port to 8443
		var current anpv1alpha1.NetworkPolicy
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-update", Namespace: testNamespace}, &current)).To(Succeed())
		current.Spec.Egress[0].Ports[0].Port = portInt(8443)
		Expect(k8sClient.Update(ctx, &current)).To(Succeed())

		// Assert std NP updated to port 8443
		Eventually(func(g Gomega) {
			var stdNP networkingv1.NetworkPolicy
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-update", Namespace: testNamespace}, &stdNP)).To(Succeed())
			g.Expect(stdNP.Spec.Egress).NotTo(BeEmpty())
			g.Expect(stdNP.Spec.Egress[0].Ports).NotTo(BeEmpty())
			g.Expect(stdNP.Spec.Egress[0].Ports[0].Port.IntValue()).To(Equal(8443))
		}, "60s", "2s").Should(Succeed())
	})

	It("should garbage collect the standard NetworkPolicy when the augmented one is deleted", func() {
		cr := &anpv1alpha1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-delete",
				Namespace: testNamespace,
			},
			Spec: anpv1alpha1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "web"},
				},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				Egress: []anpv1alpha1.EgressRule{
					{
						Ports: []anpv1alpha1.NetworkPolicyPort{
							{Protocol: tcpProtocol(), Port: portInt(443)},
						},
						To: []anpv1alpha1.EgressPeer{
							{Hostname: "kubernetes.default.svc.cluster.local"},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		// Wait for standard NP to exist
		Eventually(func() error {
			var stdNP networkingv1.NetworkPolicy
			return k8sClient.Get(ctx, types.NamespacedName{Name: "test-delete", Namespace: testNamespace}, &stdNP)
		}, "60s", "2s").Should(Succeed())

		// Delete the augmented NP
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())

		// Assert both are gone
		Eventually(func() bool {
			var stdNP networkingv1.NetworkPolicy
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-delete", Namespace: testNamespace}, &stdNP)
			return apierrors.IsNotFound(err)
		}, "60s", "2s").Should(BeTrue())
	})

	It("should expose creation and deletion metrics", func() {
		cr := &anpv1alpha1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-metrics",
				Namespace: testNamespace,
			},
			Spec: anpv1alpha1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "web"},
				},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
				Egress: []anpv1alpha1.EgressRule{
					{
						Ports: []anpv1alpha1.NetworkPolicyPort{
							{Protocol: tcpProtocol(), Port: portInt(443)},
						},
						To: []anpv1alpha1.EgressPeer{
							{Hostname: "kubernetes.default.svc.cluster.local"},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		// Wait for standard NP
		Eventually(func() error {
			var stdNP networkingv1.NetworkPolicy
			return k8sClient.Get(ctx, types.NamespacedName{Name: "test-metrics", Namespace: testNamespace}, &stdNP)
		}, "60s", "2s").Should(Succeed())

		// Check creation metric via curl pod
		metricsURL := "http://augmented-networkpolicy-operator-controller-manager-metrics.augmented-networkpolicy-operator-system.svc:8080/metrics"
		assertMetric(ctx, "curl-metrics-create", metricsURL, "augmented_networkpolicy_creations_total")

		// Delete the augmented NP
		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())

		// Wait for standard NP to be gone
		Eventually(func() bool {
			var stdNP networkingv1.NetworkPolicy
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-metrics", Namespace: testNamespace}, &stdNP)
			return apierrors.IsNotFound(err)
		}, "60s", "2s").Should(BeTrue())

		// Check deletion metric
		assertMetric(ctx, "curl-metrics-delete", metricsURL, "augmented_networkpolicy_deletions_total")
	})
})

// assertMetric creates a curl pod to fetch metrics and asserts the given metric has a value >= 1.
func assertMetric(ctx context.Context, podName, metricsURL, metricName string) {
	// Clean up any previous pod with this name
	_ = k8sClient.Delete(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: testNamespace},
	})
	// Wait for old pod to be gone
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: testNamespace}, &corev1.Pod{})
		return apierrors.IsNotFound(err)
	}, "30s", "2s").Should(BeTrue())

	// Create the curl pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: testNamespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "curl",
					Image:   "curlimages/curl",
					Command: []string{"curl", "-sf", metricsURL},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, pod)).To(Succeed())

	// Wait for pod to succeed
	Eventually(func(g Gomega) {
		var p corev1.Pod
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: testNamespace}, &p)).To(Succeed())
		g.Expect(p.Status.Phase).To(Equal(corev1.PodSucceeded))
	}, "120s", "2s").Should(Succeed())

	// Read the pod logs via kubernetes clientset
	cfg, err := ctrl.GetConfig()
	Expect(err).NotTo(HaveOccurred())

	clientset, err := kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	req := clientset.CoreV1().Pods(testNamespace).GetLogs(podName, &corev1.PodLogOptions{})
	stream, err := req.Stream(ctx)
	Expect(err).NotTo(HaveOccurred())
	defer stream.Close()

	buf := new(strings.Builder)
	_, err = io.Copy(buf, io.LimitReader(stream, 1<<20)) // 1 MB limit
	Expect(err).NotTo(HaveOccurred())

	logs := buf.String()

	// Check that the metric exists with value >= 1
	found := false
	for _, line := range strings.Split(logs, "\n") {
		if strings.HasPrefix(line, metricName+" ") {
			found = true
			break
		}
	}
	Expect(found).To(BeTrue(), fmt.Sprintf("metric %s not found in metrics output", metricName))

	// Clean up the curl pod
	_ = k8sClient.Delete(ctx, pod)
}
