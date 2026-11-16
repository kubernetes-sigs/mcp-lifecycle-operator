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

package v1beta1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1"
)

func TestV1ConversionPreservesSharedFields(t *testing.T) {
	original := &MCPServer{
		APIVersion: GroupVersion.String(),
		Kind:       MCPServerKind,
		Name:       "example",
		Namespace:  "default",
		Labels:     map[string]string{"owner": "test"},
		Spec: MCPServerSpec{
			Source:      Source{Type: SourceTypeContainerImage, ContainerImage: &ContainerImageSource{Ref: "example:latest"}},
			Config:      ServerConfig{Port: 8080},
			WorkloadRef: &WorkloadReference{Name: "workload", Kind: WorkloadKindDeployment},
			ServiceRef:  &ServiceReference{Name: "service"},
		},
		Status: MCPServerStatus{ServerInfo: &MCPServerInfo{
			Name: "example", Capabilities: &MCPServerCapabilities{Tools: true, Logging: true}, //nolint:staticcheck // conversion deliberately drops deprecated logging
		}},
	}
	stored := &v1.MCPServer{}
	if err := original.ConvertTo(stored); err != nil {
		t.Fatal(err)
	}
	if stored.APIVersion != v1.GroupVersion.String() || stored.Kind != v1.MCPServerKind {
		t.Fatalf("conversion returned wrong GVK: %s %s", stored.APIVersion, stored.Kind)
	}
	if stored.Status.ServerInfo.Capabilities.Tools != true {
		t.Fatal("shared tools capability lost")
	}
	roundTripped := &MCPServer{}
	if err := roundTripped.ConvertFrom(stored); err != nil {
		t.Fatal(err)
	}
	original.Status.ServerInfo.Capabilities.Logging = false //nolint:staticcheck // deprecated field intentionally absent from v1
	if diff := cmp.Diff(original, roundTripped); diff != "" {
		t.Fatalf("shared fields changed (-want +got):\n%s", diff)
	}
}

func TestV1ConversionRejectsMalformedManagedFields(t *testing.T) {
	metadata := metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{{
		FieldsV1: metav1.NewFieldsV1("{invalid"),
	}}}
	if err := (&MCPServer{ObjectMeta: metadata}).ConvertTo(&v1.MCPServer{}); err == nil {
		t.Fatal("expected beta-to-v1 conversion to reject malformed managed fields")
	}
	if err := (&MCPServer{}).ConvertFrom(&v1.MCPServer{ObjectMeta: metadata}); err == nil {
		t.Fatal("expected v1-to-beta conversion to reject malformed managed fields")
	}
}
