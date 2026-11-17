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
	"time"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	v1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1"
)

func roundTripFixture() *MCPServer {
	transition := metav1.NewTime(time.Date(2026, time.November, 1, 12, 30, 0, 0, time.UTC))
	return &MCPServer{
		APIVersion:        GroupVersion.String(),
		Kind:              MCPServerKind,
		Name:              "example",
		Namespace:         "default",
		Labels:            map[string]string{"team": "platform"},
		Annotations:       map[string]string{"note": "round-trip"},
		Generation:        3,
		CreationTimestamp: transition,
		Finalizers:        []string{"mcp.x-k8s.io/cleanup"},
		Spec: MCPServerSpec{
			ExtraLabels:      map[string]string{"app": "demo"},
			ExtraAnnotations: map[string]string{"owner": "test"},
			Source: Source{Type: SourceTypeContainerImage, ContainerImage: &ContainerImageSource{
				Ref: "ghcr.io/example/server:v1", PullPolicy: corev1.PullAlways,
				ImagePullSecrets: []corev1.LocalObjectReference{{Name: "pull-secret"}},
			}},
			Config: ServerConfig{
				Port: 8080, Path: "/mcp", Arguments: []string{"--config", "/etc/mcp/config"},
				Env: []corev1.EnvVar{{Name: "TOKEN", ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Name: "credentials", Key: "token",
					},
				}}},
				EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{
					Name: "settings",
				}}},
				Storage: []StorageMount{{
					Path: "/etc/mcp", Permissions: MountPermissionsReadOnly,
					Source: StorageSource{Type: StorageTypeConfigMap, ConfigMap: &corev1.ConfigMapVolumeSource{
						Name: "settings",
					}},
				}},
			},
			Runtime: RuntimeConfig{
				Replicas: new(int32(0)),
				Strategy: &appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType,
					RollingUpdate: &appsv1.RollingUpdateDeployment{
						MaxSurge: new(intstr.FromString("25%")), MaxUnavailable: new(intstr.FromInt32(0)),
					}},
				Security: SecurityConfig{
					ServiceAccountName: "mcp-server",
					PodSecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: new(true)},
					SecurityContext:    &corev1.SecurityContext{ReadOnlyRootFilesystem: new(true)},
				},
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
				},
				Health: HealthConfig{
					LivenessProbe:  &corev1.Probe{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8080)}},
					ReadinessProbe: &corev1.Probe{HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(8080)}},
				},
			},
			MCP: MCPConfig{Stateless: new(true)},
			Network: &NetworkConfig{
				IngressFrom: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"role": "client"},
				}}},
				EgressTo: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{
					CIDR: "10.0.0.0/24", Except: []string{"10.0.0.1/32"},
				}}},
				EgressPorts: []networkingv1.NetworkPolicyPort{{
					Protocol: ptr.To(corev1.ProtocolTCP), Port: new(intstr.FromInt32(443)),
				}},
				DNSEgressPeer: &networkingv1.NetworkPolicyPeer{PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "dns"},
				}},
			},
			Transport: &TransportConfig{TLS: &TLSClientConfig{
				Enabled: true, CABundleSecret: &SecretReference{Name: "server-ca"}, InsecureSkipVerify: true,
			}},
			Gateway: &GatewaySpec{Provider: "httproute", ConfigRef: "gateway-config"},
		},
		Status: MCPServerStatus{
			ObservedGeneration:  3,
			DeploymentName:      "example",
			ServiceName:         "example",
			WorkloadSummary:     "ghcr.io/example/server:v1",
			GatewayBinding:      &GatewayBindingStatus{Name: "example", Provider: "httproute"},
			Address:             &MCPServerAddress{URL: "https://example.test/mcp"},
			VerifiedEndpointURL: "https://example.default.svc:8080/mcp",
			ServerInfo: &MCPServerInfo{
				Name: "example", Version: "1.2.3", ProtocolVersion: "2025-06-18",
				Instructions: "Use the tools", Capabilities: &MCPServerCapabilities{
					Tools: true, Resources: true, Prompts: true, Logging: true, Completions: true, //nolint:staticcheck // intentionally omitted by v1
				},
			},
			Replicas: 0, ReadyReplicas: 0,
			Conditions: []metav1.Condition{{
				Type: "Verified", Status: metav1.ConditionTrue, Reason: "Verified",
				Message: "Handshake succeeded", LastTransitionTime: transition, ObservedGeneration: 3,
			}},
		},
	}
}

func assertV1RoundTrips(t *testing.T, original *MCPServer) {
	t.Helper()
	stored := &v1.MCPServer{}
	if err := original.ConvertTo(stored); err != nil {
		t.Fatal(err)
	}
	read := &MCPServer{}
	if err := read.ConvertFrom(stored); err != nil {
		t.Fatal(err)
	}
	expected := original.DeepCopy()
	if expected.Status.ServerInfo != nil && expected.Status.ServerInfo.Capabilities != nil {
		expected.Status.ServerInfo.Capabilities.Logging = false //nolint:staticcheck // v1 deliberately omits the deprecated field
	}
	if diff := cmp.Diff(expected, read); diff != "" {
		t.Errorf("v1beta1 -> v1 -> v1beta1 changed shared fields (-want +got):\n%s", diff)
	}
	storedAgain := &v1.MCPServer{}
	if err := read.ConvertTo(storedAgain); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(stored, storedAgain); diff != "" {
		t.Errorf("v1 -> v1beta1 -> v1 changed fields (-want +got):\n%s", diff)
	}
}

func TestV1RoundTripsRepresentativeObjects(t *testing.T) {
	managed := roundTripFixture()
	assertV1RoundTrips(t, managed)

	byo := roundTripFixture()
	byo.Spec.WorkloadRef = &WorkloadReference{Name: "external", Kind: WorkloadKindDeployment}
	byo.Spec.ServiceRef = &ServiceReference{Name: "external"}
	byo.Spec.Source = Source{}
	byo.Spec.Config = ServerConfig{}
	byo.Spec.Runtime = RuntimeConfig{}
	byo.Spec.Network = nil
	byo.Spec.MCP = MCPConfig{}
	byo.Status.WorkloadName = "Deployment/external"
	assertV1RoundTrips(t, byo)
}

func FuzzV1RoundTrip(f *testing.F) {
	f.Add("example", "ghcr.io/example/server:v1", "/mcp", "Use the tools", int32(8080), int32(0), true, false)
	f.Add("unicode", "ghcr.io/example/server:v2", "/api/mcp", "Tools, ресурсы, 提示", int32(65535), int32(3), false, true)

	f.Fuzz(func(t *testing.T, name, image, path, instructions string, port, replicas int32, stateless, logging bool) {
		for _, value := range []string{name, image, path, instructions} {
			if !utf8.ValidString(value) || len(value) > 256 {
				return
			}
		}
		if port < 0 || port > 65535 || replicas < 0 || replicas > 1000 {
			return
		}
		original := roundTripFixture()
		original.Name = name
		original.Spec.Source.ContainerImage.Ref = image
		original.Spec.Config.Path = path
		original.Spec.Config.Port = port
		original.Spec.Runtime.Replicas = &replicas
		original.Spec.MCP.Stateless = &stateless
		original.Status.ServerInfo.Instructions = instructions
		original.Status.ServerInfo.Capabilities.Logging = logging //nolint:staticcheck // check the intentional v1 omission
		assertV1RoundTrips(t, original)
	})
}
