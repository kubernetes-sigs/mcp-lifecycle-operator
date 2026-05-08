//go:build e2e

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

package framework

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// MCPServerOption configures an MCPServer for testing.
type MCPServerOption func(*mcpv1alpha1.MCPServer)

// WithPort sets the MCPServer port.
func WithPort(port int32) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Config.Port = port
	}
}

// WithImage sets the container image ref.
func WithImage(ref string) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Source.ContainerImage.Ref = ref
	}
}

// NewMCPServer creates an MCPServer with sensible defaults for e2e tests.
// Defaults: image=quay.io/matzew/mcp-everything:latest, port=3001.
func NewMCPServer(name, namespace string, opts ...MCPServerOption) *mcpv1alpha1.MCPServer {
	server := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Source: mcpv1alpha1.Source{
				Type: mcpv1alpha1.SourceTypeContainerImage,
				ContainerImage: &mcpv1alpha1.ContainerImageSource{
					Ref: "quay.io/matzew/mcp-everything:latest",
				},
			},
			Config: mcpv1alpha1.ServerConfig{
				Port: 3001,
			},
		},
	}
	for _, opt := range opts {
		opt(server)
	}
	return server
}
