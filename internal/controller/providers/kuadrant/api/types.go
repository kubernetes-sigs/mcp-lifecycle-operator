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

// +kubebuilder:skip

// Local types for mcp.kuadrant.io/v1alpha1 MCPServerRegistration.
// Defined here instead of importing from the Kuadrant project because the
// mcp-gateway repository is not published as a standalone Go module and the
// kuadrant-operator module does not contain MCPServerRegistration types.
package api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	SchemeGroupVersion = schema.GroupVersion{Group: "mcp.kuadrant.io", Version: "v1alpha1"}
	SchemeBuilder      = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme        = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&MCPServerRegistration{},
		&MCPServerRegistrationList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

type MCPServerRegistration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              MCPServerRegistrationSpec `json:"spec"`
}

type MCPServerRegistrationSpec struct {
	TargetRef TargetReference `json:"targetRef"`
	Prefix    string          `json:"prefix,omitempty"`
	Path      string          `json:"path,omitempty"`
	State     string          `json:"state,omitempty"`
}

type TargetReference struct {
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type MCPServerRegistrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPServerRegistration `json:"items"`
}

func (in *MCPServerRegistration) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(MCPServerRegistration)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPServerRegistration) DeepCopyInto(out *MCPServerRegistration) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
}

func (in *MCPServerRegistrationList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(MCPServerRegistrationList)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPServerRegistrationList) DeepCopyInto(out *MCPServerRegistrationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]MCPServerRegistration, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
