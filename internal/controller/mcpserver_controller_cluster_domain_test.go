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
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestMCPServiceURLUsesConfiguredClusterDomain(t *testing.T) {
	server := &mcpv1beta1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "server", Namespace: "apps"}, Spec: mcpv1beta1.MCPServerSpec{Config: mcpv1beta1.ServerConfig{Port: 8080}}}
	if got, want := mcpServiceURL(server, ServiceEndpoint{Name: "server", Port: 8080}, "cozy.local", "/mcp"); got != want {
		t.Fatalf("mcpServiceURL() = %q, want %q", got, want)
	}
}

func TestMCPServiceURLDefaultsClusterDomain(t *testing.T) {
	server := &mcpv1beta1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "server", Namespace: "apps"}, Spec: mcpv1beta1.MCPServerSpec{Config: mcpv1beta1.ServerConfig{Port: 9090}}}
	if got, want := mcpServiceURL(server, ServiceEndpoint{Name: "server", Port: 9090}, "  ", "/stream"); got != want {
		t.Fatalf("mcpServiceURL() = %q, want %q", got, want)
	}
}
