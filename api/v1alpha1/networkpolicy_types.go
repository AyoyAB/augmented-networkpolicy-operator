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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// NetworkPolicyPort describes a port to allow traffic on.
type NetworkPolicyPort struct {
	// Protocol is the protocol (TCP, UDP, or SCTP) which traffic must match.
	// +optional
	Protocol *corev1.Protocol `json:"protocol,omitempty"`

	// Port is the port on the given protocol.
	// +optional
	Port *intstr.IntOrString `json:"port,omitempty"`

	// EndPort indicates the last port in a range of ports.
	// +optional
	EndPort *int32 `json:"endPort,omitempty"`
}

// EgressPeer describes a peer to allow traffic to.
type EgressPeer struct {
	// Hostname is the DNS name to resolve to IP addresses for this peer.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?$`
	Hostname string `json:"hostname"`
}

// EgressRule describes an egress rule allowing traffic to resolved hostnames.
type EgressRule struct {
	// Ports is a list of destination ports for outgoing traffic.
	// +optional
	Ports []NetworkPolicyPort `json:"ports,omitempty"`

	// To is a list of destinations for outgoing traffic specified by hostname.
	// +optional
	To []EgressPeer `json:"to,omitempty"`
}

// NetworkPolicySpec defines the desired state of NetworkPolicy.
type NetworkPolicySpec struct {
	// PodSelector selects the pods to which this NetworkPolicy applies.
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// Egress is a list of egress rules to be applied to the selected pods.
	// +optional
	Egress []EgressRule `json:"egress,omitempty"`

	// PolicyTypes describes which types of policy this applies to.
	// +optional
	PolicyTypes []networkingv1.PolicyType `json:"policyTypes,omitempty"`

	// ResolutionInterval is how often to re-resolve DNS hostnames.
	// Defaults to 5 minutes.
	// +optional
	ResolutionInterval *metav1.Duration `json:"resolutionInterval,omitempty"`
}

// NetworkPolicyStatus defines the observed state of NetworkPolicy.
type NetworkPolicyStatus struct {
	// Conditions represent the latest available observations of the NetworkPolicy's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ResolvedAddresses maps hostnames to their resolved IP addresses.
	// +optional
	ResolvedAddresses map[string][]string `json:"resolvedAddresses,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=anp
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NetworkPolicy is the Schema for the networkpolicies API.
type NetworkPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkPolicySpec   `json:"spec,omitempty"`
	Status NetworkPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NetworkPolicyList contains a list of NetworkPolicy.
type NetworkPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetworkPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NetworkPolicy{}, &NetworkPolicyList{})
}
