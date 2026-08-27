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

package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	v1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func TestConversionRoundTrip_MinimalSpec(t *testing.T) {
	original := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Source: Source{
				Type: SourceTypeContainerImage,
				ContainerImage: &ContainerImageSource{
					Ref: "ghcr.io/example/mcp-server:v1.0.0",
				},
			},
			Config: ServerConfig{
				Port: 8080,
			},
		},
	}

	hub := &v1beta1.MCPServer{}
	if err := original.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	roundTripped := &MCPServer{}
	if err := roundTripped.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if diff := cmp.Diff(original, roundTripped); diff != "" {
		t.Errorf("round-trip mismatch (-original +roundTripped):\n%s", diff)
	}
}

func TestConversionRoundTrip_FullyPopulated(t *testing.T) {
	original := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "full-server",
			Namespace:   "production",
			Labels:      map[string]string{"app": "test"},
			Annotations: map[string]string{"note": "test"},
		},
		Spec: MCPServerSpec{
			ExtraLabels:      map[string]string{"team": "platform"},
			ExtraAnnotations: map[string]string{"env": "prod"},
			Source: Source{
				Type: SourceTypeContainerImage,
				ContainerImage: &ContainerImageSource{
					Ref:        "ghcr.io/example/mcp-server@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					PullPolicy: corev1.PullAlways,
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: "registry-credentials"},
					},
				},
			},
			Config: ServerConfig{
				Port:      9090,
				Arguments: []string{"--verbose", "--config", "/etc/config.yaml"},
				Env: []corev1.EnvVar{
					{Name: "LOG_LEVEL", Value: "debug"},
					{Name: "SECRET_KEY", ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
							Key:                  "key",
						},
					}},
				},
				EnvFrom: []corev1.EnvFromSource{
					{ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"},
					}},
				},
				Storage: []StorageMount{
					{
						Path:        "/data/config",
						Permissions: MountPermissionsReadOnly,
						Source: StorageSource{
							Type: StorageTypeConfigMap,
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "server-config"},
							},
						},
					},
					{
						Path:        "/data/secrets",
						Permissions: MountPermissionsRecursiveReadOnly,
						Source: StorageSource{
							Type: StorageTypeSecret,
							Secret: &corev1.SecretVolumeSource{
								SecretName: "server-secrets",
							},
						},
					},
					{
						Path:        "/tmp/scratch",
						Permissions: MountPermissionsReadWrite,
						Source: StorageSource{
							Type:     StorageTypeEmptyDir,
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
				Path: "/api/v1/mcp",
			},
			Runtime: RuntimeConfig{
				Replicas: ptr.To[int32](3),
				Security: SecurityConfig{
					ServiceAccountName: "mcp-sa",
					PodSecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true), //nolint:modernize // ptr.To(true) != new(bool)
					},
					SecurityContext: &corev1.SecurityContext{
						ReadOnlyRootFilesystem: ptr.To(true), //nolint:modernize // ptr.To(true) != new(bool)
					},
				},
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				},
				Health: HealthConfig{
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(8080)},
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(8080)},
						},
					},
				},
			},
			MCP: MCPConfig{
				Stateless: ptr.To(true), //nolint:modernize // ptr.To(true) != new(bool)
			},
			Network: &NetworkConfig{
				IngressFrom: []networkingv1.NetworkPolicyPeer{
					{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"role": "client"},
						},
					},
				},
				EgressTo: []networkingv1.NetworkPolicyPeer{
					{
						IPBlock: &networkingv1.IPBlock{
							CIDR: "10.0.0.0/8",
						},
					},
				},
				EgressPorts: []networkingv1.NetworkPolicyPort{
					{
						Port: ptr.To(intstr.FromInt32(443)), //nolint:modernize // FromInt32 is not a simple value
					},
				},
			},
			Transport: &TransportConfig{
				TLS: &TLSClientConfig{
					Enabled:            true,
					InsecureSkipVerify: true,
					CABundleSecret: &SecretReference{
						Name: "ca-bundle",
					},
				},
			},
		},
		Status: MCPServerStatus{
			ObservedGeneration:  5,
			DeploymentName:      "full-server",
			ServiceName:         "full-server",
			HandshakeRetryCount: 2,
			Replicas:            3,
			ReadyReplicas:       2,
			Address: &MCPServerAddress{
				URL: "http://full-server.production.svc.cluster.local:9090/api/v1/mcp",
			},
			ServerInfo: &MCPServerInfo{
				Name:            "Full MCP Server",
				Version:         "2.0.0",
				ProtocolVersion: "2025-03-26",
				Instructions:    "Use this server for testing",
				Capabilities: &MCPServerCapabilities{
					Tools:       true,
					Resources:   true,
					Prompts:     true,
					Logging:     true, //nolint:staticcheck // testing deprecated field round-trip
					Completions: true,
				},
			},
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 5,
					Reason:             "Available",
					Message:            "Server is ready",
				},
			},
		},
	}

	hub := &v1beta1.MCPServer{}
	if err := original.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	// Verify v1beta1 has Available + Verified instead of Ready
	if len(hub.Status.Conditions) != 2 {
		t.Fatalf("expected 2 conditions in hub (Available, Verified), got %d", len(hub.Status.Conditions))
	}

	roundTripped := &MCPServer{}
	if err := roundTripped.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if diff := cmp.Diff(original, roundTripped); diff != "" {
		t.Errorf("round-trip mismatch (-original +roundTripped):\n%s", diff)
	}
}

func TestConversion_HandshakeRetryCountPreserved(t *testing.T) {
	original := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "retry-test",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Source: Source{Type: SourceTypeContainerImage, ContainerImage: &ContainerImageSource{Ref: "test:v1"}},
			Config: ServerConfig{Port: 8080},
		},
		Status: MCPServerStatus{
			HandshakeRetryCount: 5, //nolint:staticcheck // testing deprecated field round-trip
		},
	}

	hub := &v1beta1.MCPServer{}
	if err := original.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	if hub.Status.HandshakeRetryCount != 5 { //nolint:staticcheck // testing deprecated field round-trip
		t.Errorf("expected HandshakeRetryCount=5 in hub, got %d", hub.Status.HandshakeRetryCount) //nolint:staticcheck // testing deprecated field round-trip
	}

	roundTripped := &MCPServer{}
	if err := roundTripped.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if roundTripped.Status.HandshakeRetryCount != 5 {
		t.Errorf("expected HandshakeRetryCount=5 after round-trip, got %d",
			roundTripped.Status.HandshakeRetryCount)
	}
}

func TestConversionRoundTrip_NilOptionalFields(t *testing.T) {
	original := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nil-fields",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Source: Source{
				Type:           SourceTypeContainerImage,
				ContainerImage: nil,
			},
			Config: ServerConfig{
				Port: 8080,
			},
			Network:   nil,
			Transport: nil,
		},
		Status: MCPServerStatus{
			Address:    nil,
			ServerInfo: nil,
		},
	}

	hub := &v1beta1.MCPServer{}
	if err := original.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	if hub.Spec.Source.ContainerImage != nil {
		t.Error("expected nil ContainerImage in hub after ConvertTo")
	}
	if hub.Spec.Network != nil {
		t.Error("expected nil Network in hub after ConvertTo")
	}
	if hub.Spec.Transport != nil {
		t.Error("expected nil Transport in hub after ConvertTo")
	}
	if hub.Status.Address != nil {
		t.Error("expected nil Address in hub after ConvertTo")
	}
	if hub.Status.ServerInfo != nil {
		t.Error("expected nil ServerInfo in hub after ConvertTo")
	}

	roundTripped := &MCPServer{}
	if err := roundTripped.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if diff := cmp.Diff(original, roundTripped); diff != "" {
		t.Errorf("round-trip mismatch (-original +roundTripped):\n%s", diff)
	}
}

func TestConversionRoundTrip_ServerInfoWithNilCapabilities(t *testing.T) {
	original := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-caps",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Source: Source{Type: SourceTypeContainerImage, ContainerImage: &ContainerImageSource{Ref: "test:v1"}},
			Config: ServerConfig{Port: 8080},
		},
		Status: MCPServerStatus{
			ServerInfo: &MCPServerInfo{
				Name:         "test-server",
				Version:      "1.0",
				Capabilities: nil,
			},
		},
	}

	hub := &v1beta1.MCPServer{}
	if err := original.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	if hub.Status.ServerInfo.Capabilities != nil {
		t.Error("expected nil Capabilities in hub after ConvertTo")
	}

	roundTripped := &MCPServer{}
	if err := roundTripped.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if diff := cmp.Diff(original, roundTripped); diff != "" {
		t.Errorf("round-trip mismatch (-original +roundTripped):\n%s", diff)
	}
}

func TestConversionRoundTrip_HubToSpoke_NoConditions(t *testing.T) {
	hub := &v1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "from-hub",
			Namespace: "test-ns",
		},
		Spec: v1beta1.MCPServerSpec{
			Source: v1beta1.Source{
				Type: v1beta1.SourceTypeContainerImage,
				ContainerImage: &v1beta1.ContainerImageSource{
					Ref: "registry.io/server:v2",
				},
			},
			Config: v1beta1.ServerConfig{
				Port:      3000,
				Arguments: []string{"serve"},
				Path:      "/mcp",
			},
			MCP: v1beta1.MCPConfig{
				Stateless: new(bool),
			},
		},
		Status: v1beta1.MCPServerStatus{
			ObservedGeneration: 1,
			DeploymentName:     "from-hub",
			ServiceName:        "from-hub",
			Replicas:           1,
			ReadyReplicas:      1,
		},
	}

	spoke := &MCPServer{}
	if err := spoke.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	hubRoundTripped := &v1beta1.MCPServer{}
	if err := spoke.ConvertTo(hubRoundTripped); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	if diff := cmp.Diff(hub, hubRoundTripped); diff != "" {
		t.Errorf("hub round-trip mismatch (-original +roundTripped):\n%s", diff)
	}
}

func TestConversionRoundTrip_HubToSpoke_WithConditions(t *testing.T) {
	hub := &v1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "from-hub-conds",
			Namespace: "test-ns",
		},
		Spec: v1beta1.MCPServerSpec{
			Source: v1beta1.Source{
				Type:           v1beta1.SourceTypeContainerImage,
				ContainerImage: &v1beta1.ContainerImageSource{Ref: "test:v1"},
			},
			Config: v1beta1.ServerConfig{Port: 8080},
		},
		Status: v1beta1.MCPServerStatus{
			ObservedGeneration: 3,
			Conditions: []metav1.Condition{
				{
					Type:               "Accepted",
					Status:             metav1.ConditionTrue,
					Reason:             "Valid",
					ObservedGeneration: 3,
				},
				{
					Type:               "Available",
					Status:             metav1.ConditionTrue,
					Reason:             "Available",
					Message:            "Workload is running",
					ObservedGeneration: 3,
				},
				{
					Type:               "Verified",
					Status:             metav1.ConditionTrue,
					Reason:             "Verified",
					Message:            "MCP handshake succeeded",
					ObservedGeneration: 3,
				},
			},
		},
	}

	spoke := &MCPServer{}
	if err := spoke.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	// v1alpha1 should have Accepted + Ready (merged from Available+Verified)
	if len(spoke.Status.Conditions) != 2 {
		t.Fatalf("expected 2 conditions in spoke (Accepted, Ready), got %d", len(spoke.Status.Conditions))
	}

	hubRoundTripped := &v1beta1.MCPServer{}
	if err := spoke.ConvertTo(hubRoundTripped); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	if diff := cmp.Diff(hub, hubRoundTripped); diff != "" {
		t.Errorf("hub round-trip mismatch (-original +roundTripped):\n%s", diff)
	}
}

func TestConversion_ReadyConditionSplit(t *testing.T) {
	tests := []struct {
		name           string
		readyCondition metav1.Condition
		wantAvailable  metav1.ConditionStatus
		wantVerified   metav1.ConditionStatus
	}{
		{
			name: "Available maps to Available=True, Verified=True",
			readyCondition: metav1.Condition{
				Type:   "Ready",
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			wantAvailable: metav1.ConditionTrue,
			wantVerified:  metav1.ConditionTrue,
		},
		{
			name: "MCPEndpointUnavailable maps to Available=True, Verified=False",
			readyCondition: metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "MCPEndpointUnavailable",
				Message: "connection refused",
			},
			wantAvailable: metav1.ConditionTrue,
			wantVerified:  metav1.ConditionFalse,
		},
		{
			name: "DeploymentUnavailable maps to Available=False, Verified=Unknown",
			readyCondition: metav1.Condition{
				Type:   "Ready",
				Status: metav1.ConditionFalse,
				Reason: "DeploymentUnavailable",
			},
			wantAvailable: metav1.ConditionFalse,
			wantVerified:  metav1.ConditionUnknown,
		},
		{
			name: "Initializing maps to Available=Unknown, Verified=Unknown",
			readyCondition: metav1.Condition{
				Type:   "Ready",
				Status: metav1.ConditionUnknown,
				Reason: "Initializing",
			},
			wantAvailable: metav1.ConditionUnknown,
			wantVerified:  metav1.ConditionUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: MCPServerSpec{
					Source: Source{Type: SourceTypeContainerImage, ContainerImage: &ContainerImageSource{Ref: "test:v1"}},
					Config: ServerConfig{Port: 8080},
				},
				Status: MCPServerStatus{
					Conditions: []metav1.Condition{tt.readyCondition},
				},
			}

			hub := &v1beta1.MCPServer{}
			if err := original.ConvertTo(hub); err != nil {
				t.Fatalf("ConvertTo failed: %v", err)
			}

			if len(hub.Status.Conditions) != 2 {
				t.Fatalf("expected 2 conditions (Available, Verified), got %d", len(hub.Status.Conditions))
			}

			var gotAvailable, gotVerified *metav1.Condition
			for i := range hub.Status.Conditions {
				switch hub.Status.Conditions[i].Type {
				case "Available":
					gotAvailable = &hub.Status.Conditions[i]
				case "Verified":
					gotVerified = &hub.Status.Conditions[i]
				}
			}

			if gotAvailable == nil {
				t.Fatal("missing Available condition")
			}
			if gotVerified == nil {
				t.Fatal("missing Verified condition")
			}
			if gotAvailable.Status != tt.wantAvailable {
				t.Errorf("Available.Status = %v, want %v", gotAvailable.Status, tt.wantAvailable)
			}
			if gotVerified.Status != tt.wantVerified {
				t.Errorf("Verified.Status = %v, want %v", gotVerified.Status, tt.wantVerified)
			}
		})
	}
}

func TestConversionRoundTrip_ZeroReplicas(t *testing.T) {
	original := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scaled-to-zero",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Source: Source{Type: SourceTypeContainerImage, ContainerImage: &ContainerImageSource{Ref: "test:v1"}},
			Config: ServerConfig{Port: 8080},
			Runtime: RuntimeConfig{
				Replicas: ptr.To[int32](0),
			},
		},
	}

	hub := &v1beta1.MCPServer{}
	if err := original.ConvertTo(hub); err != nil {
		t.Fatalf("ConvertTo failed: %v", err)
	}

	if hub.Spec.Runtime.Replicas == nil || *hub.Spec.Runtime.Replicas != 0 {
		t.Errorf("expected replicas=0 in hub, got %v", hub.Spec.Runtime.Replicas)
	}

	roundTripped := &MCPServer{}
	if err := roundTripped.ConvertFrom(hub); err != nil {
		t.Fatalf("ConvertFrom failed: %v", err)
	}

	if diff := cmp.Diff(original, roundTripped); diff != "" {
		t.Errorf("round-trip mismatch (-original +roundTripped):\n%s", diff)
	}
}

// TestConversionRoundTrip_ReadyConditionReasons verifies that Ready conditions
// survive a v1alpha1 -> v1beta1 -> v1alpha1 round-trip with their reason and
// message intact. ScaledToZero is a Ready=True state with a non-"Available"
// reason, which is the case that previously got flattened to "Available".
func TestConversionRoundTrip_ReadyConditionReasons(t *testing.T) {
	tests := []struct {
		name      string
		condition metav1.Condition
	}{
		{
			name: "ScaledToZero preserves reason and message",
			condition: metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionTrue,
				Reason:  "ScaledToZero",
				Message: "Server is ready (scaled to 0 replicas)",
			},
		},
		{
			name: "Available round-trips",
			condition: metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionTrue,
				Reason:  "Available",
				Message: "Server is ready",
			},
		},
		{
			name: "DeploymentUnavailable round-trips",
			condition: metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "DeploymentUnavailable",
				Message: "Deployment is not available",
			},
		},
		{
			name: "MCPEndpointUnavailable round-trips",
			condition: metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "MCPEndpointUnavailable",
				Message: "connection refused",
			},
		},
		{
			name: "Initializing round-trips",
			condition: metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionUnknown,
				Reason:  "Initializing",
				Message: "Waiting for Deployment to report status",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &MCPServer{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: MCPServerSpec{
					Source: Source{Type: SourceTypeContainerImage, ContainerImage: &ContainerImageSource{Ref: "test:v1"}},
					Config: ServerConfig{Port: 8080},
				},
				Status: MCPServerStatus{
					Conditions: []metav1.Condition{tt.condition},
				},
			}

			hub := &v1beta1.MCPServer{}
			if err := original.ConvertTo(hub); err != nil {
				t.Fatalf("ConvertTo failed: %v", err)
			}

			roundTripped := &MCPServer{}
			if err := roundTripped.ConvertFrom(hub); err != nil {
				t.Fatalf("ConvertFrom failed: %v", err)
			}

			if len(roundTripped.Status.Conditions) != 1 {
				t.Fatalf("expected 1 Ready condition after round-trip, got %d", len(roundTripped.Status.Conditions))
			}
			got := roundTripped.Status.Conditions[0]
			if got.Status != tt.condition.Status {
				t.Errorf("Ready.Status = %v, want %v", got.Status, tt.condition.Status)
			}
			if got.Reason != tt.condition.Reason {
				t.Errorf("Ready.Reason = %q, want %q", got.Reason, tt.condition.Reason)
			}
			if got.Message != tt.condition.Message {
				t.Errorf("Ready.Message = %q, want %q", got.Message, tt.condition.Message)
			}
		})
	}
}
