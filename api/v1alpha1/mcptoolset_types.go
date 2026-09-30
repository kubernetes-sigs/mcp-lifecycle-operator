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

// IsolationLevel is the minimum sandbox isolation a code-mode toolset requires
// of its provider. Levels are ordered: each level implies the guarantees of the
// levels below it.
// +kubebuilder:validation:Enum=baseline;egress
type IsolationLevel string

const (
	// IsolationBaseline requires per-execution ephemeral, filesystem-isolated
	// sandboxes. The bare "pod" provider satisfies this.
	IsolationBaseline IsolationLevel = "baseline"
	// IsolationEgress additionally requires controlled network egress from the
	// sandbox. The bare "pod" provider does NOT satisfy this.
	IsolationEgress IsolationLevel = "egress"
)

// MCPToolsetPolicy captures the isolation policy a toolset requires of its
// provider.
type MCPToolsetPolicy struct {
	// MinIsolation is the minimum isolation the selected provider must satisfy.
	// If the provider's declared guarantees are weaker, the toolset is rejected
	// (Ready=False, reason InsufficientIsolation) and no dispatcher is deployed.
	// Defaults to "baseline".
	// +optional
	// +kubebuilder:default=baseline
	MinIsolation IsolationLevel `json:"minIsolation,omitempty"`
}

// MCPToolsetSpec defines the desired state of MCPToolset.
//
// A MCPToolset is a binding-style resource: it names one or more MCPServer
// resources in the same namespace and selects a sandbox provider that runs
// submitted code (code mode) against those servers, returning only the final
// result.
type MCPToolsetSpec struct {
	// Servers is the list of MCPServer resource names, in the same namespace as
	// this MCPToolset, whose tools the toolset exposes to submitted code. A
	// script running against this toolset may only reach the servers listed here
	// (the containment boundary). At least one server is required; the list
	// enables a multi-server sandbox.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="self.all(s, self.exists_one(x, x == s))",message="servers must not contain duplicate entries"
	Servers []string `json:"servers"`

	// Provider selects the sandbox execution backend. It must match the name of
	// a registered SandboxProvider. The MVP ships a single "pod" provider that
	// runs each execution in a bare Kubernetes Pod.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Provider string `json:"provider"`

	// Policy captures the isolation policy the provider must satisfy.
	// +optional
	Policy MCPToolsetPolicy `json:"policy,omitzero"`

	// TimeoutSeconds is the default per-execution time budget applied to code
	// submitted against this toolset. It also caps the lifetime of the scoped
	// credential injected into the sandbox. Defaults to 120 seconds.
	// +optional
	// +kubebuilder:default=120
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`
}

// MCPToolsetStatus defines the observed state of MCPToolset.
type MCPToolsetStatus struct {
	// ObservedGeneration reflects the generation most recently observed by the
	// controller. When it matches metadata.generation, the status is up to date.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ExecuteEndpoint is the in-cluster URL of the code-mode dispatcher for this
	// toolset. Callers submit code here; the physical topology behind this URL
	// (a per-toolset Deployment today, possibly a shared router later) is an
	// implementation detail. Populated only when Ready is True.
	// +optional
	ExecuteEndpoint string `json:"executeEndpoint,omitempty"`

	// ResolvedServers lists the servers that were successfully resolved to a
	// reachable MCPServer Service endpoint. Observability only.
	// +optional
	// +listType=atomic
	ResolvedServers []string `json:"resolvedServers,omitempty"`

	// Conditions represent the latest available observations of the toolset's
	// state.
	//
	// Standard condition types:
	// - "ServersResolved": every entry in spec.servers maps to an existing,
	//   ready MCPServer Service. False names the missing/not-ready server.
	// - "Ready": all servers resolved, the provider satisfied the isolation
	//   policy, and the dispatcher is available with ExecuteEndpoint published.
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
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.executeEndpoint`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MCPToolset enables "code mode" for a set of MCPServers: an agent submits a
// script that composes multiple MCP tool calls, the script runs in an isolated
// sandbox provisioned by the selected provider, and only the final result is
// returned. The operator reconciles the long-lived dispatcher infrastructure
// and publishes status.executeEndpoint; it is not on the per-execution path.
type MCPToolset struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec MCPToolsetSpec `json:"spec"`
	// +optional
	Status MCPToolsetStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MCPToolsetList contains a list of MCPToolset.
type MCPToolsetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MCPToolset `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MCPToolset{}, &MCPToolsetList{})
}
