package dns

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPodCIDRProvider_Refresh(t *testing.T) {
	tests := []struct {
		name          string
		nodes         []corev1.Node
		wantBlocked   []string
		wantAllowed   []string
	}{
		{
			name: "single node with PodCIDR",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Spec:       corev1.NodeSpec{PodCIDR: "10.244.0.0/24"},
				},
			},
			wantBlocked: []string{"10.244.0.5/32"},
			wantAllowed: []string{"8.8.8.8/32"},
		},
		{
			name: "node with PodCIDRs (dual-stack)",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Spec: corev1.NodeSpec{
						PodCIDRs: []string{"10.244.0.0/24", "fd00::/64"},
					},
				},
			},
			wantBlocked: []string{"10.244.0.5/32", "fd00::1/128"},
			wantAllowed: []string{"8.8.8.8/32"},
		},
		{
			name: "multiple nodes with deduplication",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Spec:       corev1.NodeSpec{PodCIDR: "10.244.0.0/24"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
					Spec:       corev1.NodeSpec{PodCIDR: "10.244.0.0/24"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-3"},
					Spec:       corev1.NodeSpec{PodCIDR: "10.244.1.0/24"},
				},
			},
			wantBlocked: []string{"10.244.0.5/32", "10.244.1.5/32"},
			wantAllowed: []string{"8.8.8.8/32"},
		},
		{
			name:        "empty node list clears blacklist",
			nodes:       []corev1.Node{},
			wantBlocked: nil,
			wantAllowed: []string{"10.244.0.5/32", "8.8.8.8/32"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme() error: %v", err)
			}

			objs := make([]runtime.Object, len(tt.nodes))
			for i := range tt.nodes {
				objs[i] = &tt.nodes[i]
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			f, err := NewIPFilter(nil, nil)
			if err != nil {
				t.Fatalf("NewIPFilter() error: %v", err)
			}

			p := &PodCIDRProvider{
				Client:   cl,
				Filter:   f,
				Logger:   logr.Discard(),
				Interval: time.Minute,
			}

			p.refresh(context.Background())

			for _, cidr := range tt.wantBlocked {
				if f.IsAllowed(cidr) {
					t.Errorf("expected %q to be blocked after refresh", cidr)
				}
			}
			for _, cidr := range tt.wantAllowed {
				if !f.IsAllowed(cidr) {
					t.Errorf("expected %q to be allowed after refresh", cidr)
				}
			}
		})
	}
}

func TestPodCIDRProvider_EmptyNodeListClearsPrevious(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error: %v", err)
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{PodCIDR: "10.244.0.0/24"},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(node).
		Build()

	f, err := NewIPFilter(nil, nil)
	if err != nil {
		t.Fatalf("NewIPFilter() error: %v", err)
	}

	p := &PodCIDRProvider{
		Client:   cl,
		Filter:   f,
		Logger:   logr.Discard(),
		Interval: time.Minute,
	}

	// First refresh populates blacklist
	p.refresh(context.Background())
	if f.IsAllowed("10.244.0.5/32") {
		t.Fatal("expected 10.244.0.5/32 to be blocked after first refresh")
	}

	// Rebuild client with no nodes
	cl2 := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()
	p.Client = cl2

	// Second refresh clears blacklist
	p.refresh(context.Background())
	if !f.IsAllowed("10.244.0.5/32") {
		t.Fatal("expected 10.244.0.5/32 to be allowed after empty refresh")
	}
}
