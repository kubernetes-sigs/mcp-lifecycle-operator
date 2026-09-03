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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

const (
	DefaultMCPServerImage   = "quay.io/containers/kubernetes_mcp_server@sha256:6d650f4bd6ac303ad82713c997e73a2d001602f9bf17392c9b9a0e30e29c6423" // :v0.0.66
	AlternateMCPServerImage = "quay.io/containers/kubernetes_mcp_server@sha256:5df586e2c7ced2a3125f6e78923388d80b69de0a2ad1470325b05318f12725bd" // :v0.0.65
	BusyboxImage            = "docker.io/library/busybox@sha256:9db7b59979c38555a39def84a31fb98b5296952f9e3afd4f6f11f05b07adfab0"                // :1.37
)

// MCPServerOption configures an MCPServer for testing.
type MCPServerOption func(*mcpv1beta1.MCPServer)

// WithPort sets the MCPServer port.
func WithPort(port int32) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Config.Port = port
	}
}

// WithImage sets the container image ref.
func WithImage(ref string) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Source.ContainerImage.Ref = ref
	}
}

// WithArguments sets the MCPServer container arguments.
func WithArguments(args ...string) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Config.Arguments = args
	}
}

// WithEnvFrom sets the MCPServer envFrom sources.
func WithEnvFrom(envFrom ...corev1.EnvFromSource) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Config.EnvFrom = envFrom
	}
}

// WithStorage sets the MCPServer storage mounts.
func WithStorage(storage ...mcpv1beta1.StorageMount) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Config.Storage = storage
	}
}

// WithPath sets the MCPServer HTTP path.
func WithPath(path string) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Config.Path = path
	}
}

// WithSecurityContext sets the container security context.
func WithSecurityContext(sc *corev1.SecurityContext) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Runtime.Security.SecurityContext = sc
	}
}

// WithPodSecurityContext sets the pod-level security context.
func WithPodSecurityContext(psc *corev1.PodSecurityContext) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Runtime.Security.PodSecurityContext = psc
	}
}

// WithTransport sets the MCPServer transport configuration.
func WithTransport(transport *mcpv1beta1.TransportConfig) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Transport = transport
	}
}

// WithExtraLabels sets custom labels on the MCPServer.
func WithExtraLabels(labels map[string]string) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.ExtraLabels = labels
	}
}

// WithExtraAnnotations sets custom annotations on the MCPServer.
func WithExtraAnnotations(annotations map[string]string) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.ExtraAnnotations = annotations
	}
}

// WithReplicas sets the number of pod replicas.
func WithReplicas(n int32) MCPServerOption {
	return func(s *mcpv1beta1.MCPServer) {
		s.Spec.Runtime.Replicas = &n
	}
}

// NewMCPServer creates an MCPServer with sensible defaults for e2e tests.
// Defaults: image=DefaultMCPServerImage, port=8080, args=["--port","8080","--read-only"].
func NewMCPServer(name, namespace string, opts ...MCPServerOption) *mcpv1beta1.MCPServer {
	server := &mcpv1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: mcpv1beta1.MCPServerSpec{
			Source: mcpv1beta1.Source{
				Type: mcpv1beta1.SourceTypeContainerImage,
				ContainerImage: &mcpv1beta1.ContainerImageSource{
					Ref: DefaultMCPServerImage,
				},
			},
			Config: mcpv1beta1.ServerConfig{
				Port:      8080,
				Arguments: []string{"--port", "8080", "--read-only"},
			},
		},
	}
	for _, opt := range opts {
		opt(server)
	}
	return server
}
