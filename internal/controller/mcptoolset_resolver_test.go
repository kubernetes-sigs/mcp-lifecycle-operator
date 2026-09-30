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

package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func resolverScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := mcpv1beta1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return s
}

// readyServer builds a verified MCPServer with a published address URL.
func readyServer(namespace, name, url string) *mcpv1beta1.MCPServer {
	return &mcpv1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Status: mcpv1beta1.MCPServerStatus{
			Address: &mcpv1beta1.MCPServerAddress{URL: url},
			Conditions: []metav1.Condition{{
				Type:   ConditionTypeVerified,
				Status: metav1.ConditionTrue,
				Reason: ReasonVerified,
			}},
		},
	}
}

func TestResolveServersAllReady(t *testing.T) {
	s := resolverScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		readyServer("demo", "kubernetes", "http://kubernetes.demo.svc.cluster.local:8080/mcp"),
		readyServer("demo", "github", "http://github.demo.svc.cluster.local:8080/mcp"),
	).Build()

	// Order preservation: request github before kubernetes.
	endpoints, unresolved, err := ResolveServers(context.Background(), c, "demo", []string{"github", "kubernetes"})
	if err != nil {
		t.Fatalf("ResolveServers: %v", err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %v; want none", unresolved)
	}
	if len(endpoints) != 2 {
		t.Fatalf("endpoints len = %d; want 2", len(endpoints))
	}
	if endpoints[0].Server != "github" || endpoints[1].Server != "kubernetes" {
		t.Errorf("endpoint order = [%q %q]; want [github kubernetes]", endpoints[0].Server, endpoints[1].Server)
	}
	if endpoints[0].URL != "http://github.demo.svc.cluster.local:8080/mcp" {
		t.Errorf("github URL = %q; unexpected", endpoints[0].URL)
	}
}

func TestResolveServersMissing(t *testing.T) {
	s := resolverScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		readyServer("demo", "kubernetes", "http://kubernetes.demo.svc.cluster.local:8080/mcp"),
	).Build()

	endpoints, unresolved, err := ResolveServers(context.Background(), c, "demo", []string{"kubernetes", "ghost"})
	if err != nil {
		t.Fatalf("ResolveServers: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Server != "kubernetes" {
		t.Fatalf("endpoints = %v; want only kubernetes", endpoints)
	}
	if len(unresolved) != 1 {
		t.Fatalf("unresolved len = %d; want 1", len(unresolved))
	}
	if unresolved[0].Server != "ghost" || unresolved[0].Reason != ResolveReasonNotFound {
		t.Errorf("unresolved[0] = %+v; want ghost/NotFound", unresolved[0])
	}
}

func TestResolveServersNotReady(t *testing.T) {
	s := resolverScheme(t)

	// Server exists but has no address and no Verified condition.
	pending := &mcpv1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: "demo", Name: "kubernetes"},
	}
	// Server has a stale address but is not Verified.
	staleAddr := &mcpv1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Namespace: "demo", Name: "github"},
		Status: mcpv1beta1.MCPServerStatus{
			Address: &mcpv1beta1.MCPServerAddress{URL: "http://github.demo.svc.cluster.local:8080/mcp"},
			Conditions: []metav1.Condition{{
				Type:   ConditionTypeVerified,
				Status: metav1.ConditionFalse,
				Reason: ReasonNotVerified,
			}},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(pending, staleAddr).Build()

	endpoints, unresolved, err := ResolveServers(context.Background(), c, "demo", []string{"kubernetes", "github"})
	if err != nil {
		t.Fatalf("ResolveServers: %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("endpoints = %v; want none", endpoints)
	}
	if len(unresolved) != 2 {
		t.Fatalf("unresolved len = %d; want 2", len(unresolved))
	}
	for _, u := range unresolved {
		if u.Reason != ResolveReasonNotReady {
			t.Errorf("unresolved %q reason = %q; want NotReady", u.Server, u.Reason)
		}
	}
}
