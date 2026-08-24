/*
Copyright 2026 The Kubernetes Authors

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MCPGatewayBindingSpec defines the desired state of MCPGatewayBinding.
type MCPGatewayBindingSpec struct {
	// MCPServerRef is the name of the MCPServer resource in the same namespace
	// that this binding is for.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	MCPServerRef string `json:"mcpServerRef"`

	// Provider identifies which integration controller should handle this binding.
	// Integration controllers filter on this field to only process bindings they own.
	// Example: "httproute" for the reference Gateway API HTTPRoute controller.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Provider string `json:"provider"`

	// ConfigRef is the name of a ConfigMap in the same namespace containing
	// provider-specific configuration.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	ConfigRef string `json:"configRef,omitempty"`
}

// MCPGatewayBindingStatus defines the observed state of MCPGatewayBinding.
type MCPGatewayBindingStatus struct {
	// URL is the gateway endpoint URL, set by the integration controller.
	// The MCPServer reconciler reflects this into MCPServer status.
	// +optional
	URL string `json:"url,omitempty"`

	// Conditions represent the latest available observations of the binding's state.
	//
	// Standard condition types:
	// - "Registered": The integration controller has processed this binding
	//   and created the necessary gateway resources.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:ac:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="MCPServer",type=string,JSONPath=`.spec.mcpServerRef`
// +kubebuilder:printcolumn:name="Registered",type=string,JSONPath=`.status.conditions[?(@.type=="Registered")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MCPGatewayBinding binds an MCPServer to a gateway integration provider.
// The MCP Lifecycle Operator creates this resource when an MCPServer has
// spec.gateway configured. Integration controllers watch MCPGatewayBinding
// resources filtered by spec.provider and create provider-specific gateway
// resources (e.g., HTTPRoute for the "httproute" provider).
type MCPGatewayBinding struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec MCPGatewayBindingSpec `json:"spec"`
	// +optional
	Status MCPGatewayBindingStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MCPGatewayBindingList contains a list of MCPGatewayBinding.
type MCPGatewayBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MCPGatewayBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MCPGatewayBinding{}, &MCPGatewayBindingList{})
}
