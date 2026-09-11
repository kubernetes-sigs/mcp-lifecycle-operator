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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToScheme(t *testing.T) {
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}

	gvk := SchemeGroupVersion.WithKind("MCPServerRegistration")
	obj, err := s.New(gvk)
	if err != nil {
		t.Fatalf("scheme does not know %s: %v", gvk, err)
	}
	if _, ok := obj.(*MCPServerRegistration); !ok {
		t.Fatalf("expected *MCPServerRegistration, got %T", obj)
	}

	listGVK := SchemeGroupVersion.WithKind("MCPServerRegistrationList")
	listObj, err := s.New(listGVK)
	if err != nil {
		t.Fatalf("scheme does not know %s: %v", listGVK, err)
	}
	if _, ok := listObj.(*MCPServerRegistrationList); !ok {
		t.Fatalf("expected *MCPServerRegistrationList, got %T", listObj)
	}
}

func TestMCPServerRegistrationDeepCopyObject(t *testing.T) {
	reg := &MCPServerRegistration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
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
				ObjectMeta: metav1.ObjectMeta{Name: "a"},
				Spec:       MCPServerRegistrationSpec{Path: "/a"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "b"},
				Spec:       MCPServerRegistrationSpec{Path: "/b"},
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
