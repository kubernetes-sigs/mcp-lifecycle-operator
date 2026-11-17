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

package api

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToScheme(t *testing.T) {
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}

	for _, tc := range []struct {
		kind     string
		expected runtime.Object
	}{
		{"MCPServerRegistration", &MCPServerRegistration{}},
		{"MCPServerRegistrationList", &MCPServerRegistrationList{}},
		{"MCPGatewayExtension", &MCPGatewayExtension{}},
		{"MCPGatewayExtensionList", &MCPGatewayExtensionList{}},
	} {
		gvk := SchemeGroupVersion.WithKind(tc.kind)
		if _, err := s.New(gvk); err != nil {
			t.Fatalf("scheme does not know %s: %v", gvk, err)
		}
	}
}

func TestMCPServerRegistrationDeepCopyObject(t *testing.T) {
	reg := &MCPServerRegistration{
		Name:      "test",
		Namespace: "default",
		Spec: MCPServerRegistrationSpec{
			TargetRef: TargetReference{
				Group: "gateway.networking.k8s.io",
				Kind:  "HTTPRoute",
				Name:  "my-route",
			},
			Path:   "/mcp",
			Prefix: "pfx_",
			State:  "Enabled",
		},
	}

	copied := reg.DeepCopyObject()
	typedCopy, ok := copied.(*MCPServerRegistration)
	if !ok {
		t.Fatalf("expected *MCPServerRegistration, got %T", copied)
	}
	if typedCopy.Name != "test" || typedCopy.Spec.Path != "/mcp" {
		t.Fatal("deep copy does not match original")
	}

	typedCopy.Spec.Path = "/changed"
	if reg.Spec.Path == "/changed" {
		t.Fatal("mutating copy affected original")
	}
}

func TestMCPServerRegistrationDeepCopyObjectNil(t *testing.T) {
	var reg *MCPServerRegistration
	if reg.DeepCopyObject() != nil {
		t.Fatal("expected nil for nil receiver")
	}
}

func TestMCPServerRegistrationListDeepCopyObject(t *testing.T) {
	list := &MCPServerRegistrationList{
		Items: []MCPServerRegistration{
			{
				Name: "a",
				Spec: MCPServerRegistrationSpec{Path: "/a"},
			},
			{
				Name: "b",
				Spec: MCPServerRegistrationSpec{Path: "/b"},
			},
		},
	}

	copied := list.DeepCopyObject()
	typedCopy, ok := copied.(*MCPServerRegistrationList)
	if !ok {
		t.Fatalf("expected *MCPServerRegistrationList, got %T", copied)
	}
	if len(typedCopy.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(typedCopy.Items))
	}

	typedCopy.Items[0].Spec.Path = "/changed"
	if list.Items[0].Spec.Path == "/changed" {
		t.Fatal("mutating copy affected original")
	}
}

func TestMCPServerRegistrationListDeepCopyObjectNil(t *testing.T) {
	var list *MCPServerRegistrationList
	if list.DeepCopyObject() != nil {
		t.Fatal("expected nil for nil receiver")
	}
}

func TestMCPServerRegistrationListDeepCopyIntoNilItems(t *testing.T) {
	list := &MCPServerRegistrationList{}
	out := &MCPServerRegistrationList{}
	list.DeepCopyInto(out)
	if out.Items != nil {
		t.Fatal("expected nil items when source has nil items")
	}
}

func TestMCPGatewayExtensionDeepCopyObject(t *testing.T) {
	ext := &MCPGatewayExtension{
		Name:      "test-ext",
		Namespace: "default",
		Spec: MCPGatewayExtensionSpec{
			PublicHost: "mcp.example.com",
			TargetRef: TargetReference{
				Group:       "gateway.networking.k8s.io",
				Kind:        "Gateway",
				Name:        "my-gw",
				Namespace:   "gw-ns",
				SectionName: "mcp",
			},
		},
	}

	copied := ext.DeepCopyObject()
	typedCopy, ok := copied.(*MCPGatewayExtension)
	if !ok {
		t.Fatalf("expected *MCPGatewayExtension, got %T", copied)
	}
	if typedCopy.Name != "test-ext" || typedCopy.Spec.PublicHost != "mcp.example.com" {
		t.Fatal("deep copy does not match original")
	}

	typedCopy.Spec.PublicHost = "changed.example.com"
	if ext.Spec.PublicHost == "changed.example.com" {
		t.Fatal("mutating copy affected original")
	}
}

func TestMCPGatewayExtensionDeepCopyObjectNil(t *testing.T) {
	var ext *MCPGatewayExtension
	if ext.DeepCopyObject() != nil {
		t.Fatal("expected nil for nil receiver")
	}
}

func TestMCPGatewayExtensionListDeepCopyObject(t *testing.T) {
	list := &MCPGatewayExtensionList{
		Items: []MCPGatewayExtension{
			{
				Name: "a",
				Spec: MCPGatewayExtensionSpec{PublicHost: "a.example.com"},
			},
			{
				Name: "b",
				Spec: MCPGatewayExtensionSpec{PublicHost: "b.example.com"},
			},
		},
	}

	copied := list.DeepCopyObject()
	typedCopy, ok := copied.(*MCPGatewayExtensionList)
	if !ok {
		t.Fatalf("expected *MCPGatewayExtensionList, got %T", copied)
	}
	if len(typedCopy.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(typedCopy.Items))
	}

	typedCopy.Items[0].Spec.PublicHost = "changed.example.com"
	if list.Items[0].Spec.PublicHost == "changed.example.com" {
		t.Fatal("mutating copy affected original")
	}
}

func TestMCPGatewayExtensionListDeepCopyObjectNil(t *testing.T) {
	var list *MCPGatewayExtensionList
	if list.DeepCopyObject() != nil {
		t.Fatal("expected nil for nil receiver")
	}
}

func TestMCPGatewayExtensionListDeepCopyIntoNilItems(t *testing.T) {
	list := &MCPGatewayExtensionList{}
	out := &MCPGatewayExtensionList{}
	list.DeepCopyInto(out)
	if out.Items != nil {
		t.Fatal("expected nil items when source has nil items")
	}
}
