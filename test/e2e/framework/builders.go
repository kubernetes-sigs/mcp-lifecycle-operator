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

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

const (
	DefaultMCPServerImage    = "quay.io/matzew/mcp-everything@sha256:537cdedad807bb56140caca9c332d3577b16e533584164bbc3f27abac7b5ba15"            // :2026.7.10
	AlternateMCPServerImage  = "quay.io/matzew/mcp-everything@sha256:d93b35b4f0d2f9b9d4c7b49aecfbebd73ce1933b18e03875698bca9615c178f8"            // :2026.8.13
	BusyboxImage             = "docker.io/library/busybox@sha256:9db7b59979c38555a39def84a31fb98b5296952f9e3afd4f6f11f05b07adfab0"                // :1.37
	KubernetesMCPServerImage = "quay.io/containers/kubernetes_mcp_server@sha256:3a4bee3f07c1c81458f9810b160d9123d9edd91a6602d2b3c91baf7c24277090" // :v0.0.66
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

// WithArguments sets the MCPServer container arguments.
func WithArguments(args ...string) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Config.Arguments = args
	}
}

// WithEnvFrom sets the MCPServer envFrom sources.
func WithEnvFrom(envFrom ...corev1.EnvFromSource) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Config.EnvFrom = envFrom
	}
}

// WithStorage sets the MCPServer storage mounts.
func WithStorage(storage ...mcpv1alpha1.StorageMount) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Config.Storage = storage
	}
}

// WithPath sets the MCPServer HTTP path.
func WithPath(path string) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Config.Path = path
	}
}

// WithSecurityContext sets the container security context.
func WithSecurityContext(sc *corev1.SecurityContext) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Runtime.Security.SecurityContext = sc
	}
}

// WithPodSecurityContext sets the pod-level security context.
func WithPodSecurityContext(psc *corev1.PodSecurityContext) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Runtime.Security.PodSecurityContext = psc
	}
}

// WithTransport sets the MCPServer transport configuration.
func WithTransport(transport *mcpv1alpha1.TransportConfig) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Transport = transport
	}
}

// WithExtraLabels sets custom labels on the MCPServer.
func WithExtraLabels(labels map[string]string) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.ExtraLabels = labels
	}
}

// WithExtraAnnotations sets custom annotations on the MCPServer.
func WithExtraAnnotations(annotations map[string]string) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.ExtraAnnotations = annotations
	}
}

// WithReplicas sets the number of pod replicas.
func WithReplicas(n int32) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Runtime.Replicas = &n
	}
}

// WithGateway sets the gateway integration on the MCPServer.
func WithGateway(className, configRef string) MCPServerOption {
	return func(s *mcpv1alpha1.MCPServer) {
		s.Spec.Gateway = &mcpv1alpha1.GatewaySpec{
			ClassName: className,
			ConfigRef: configRef,
		}
	}
}

// NewMCPServer creates an MCPServer with sensible defaults for e2e tests.
// Defaults: image=DefaultMCPServerImage, port=3001.
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
					Ref: DefaultMCPServerImage,
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
