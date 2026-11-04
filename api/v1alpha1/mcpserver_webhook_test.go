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
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	webhookpolicy "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/webhook"
)

func testContext() context.Context {
	logger := zap.New(zap.UseDevMode(true)).WithName("test")
	return logr.NewContext(context.Background(), logger)
}

func newMCPServer(imageRef string) *MCPServer {
	return &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Source: Source{
				Type: SourceTypeContainerImage,
				ContainerImage: &ContainerImageSource{
					Ref: imageRef,
				},
			},
			Config: ServerConfig{Port: 8080},
		},
	}
}

func TestValidateUpdate_AlphaReferenceLossWarning(t *testing.T) {
	alphaKind := metav1.GroupVersionKind{Group: GroupVersion.Group, Version: GroupVersion.Version, Kind: MCPServerKind}
	betaKind := metav1.GroupVersionKind{Group: v1beta1.GroupVersion.Group, Version: v1beta1.GroupVersion.Version, Kind: MCPServerKind}
	for _, tc := range []struct {
		name        string
		serviceRef  bool
		workloadRef bool
		requestKind *metav1.GroupVersionKind
		dryRun      bool
		missing     bool
		denied      bool
		create      bool
		wantWarning bool
	}{
		{name: "alpha service reference", serviceRef: true, requestKind: &alphaKind, wantWarning: true},
		{name: "alpha workload reference", workloadRef: true, requestKind: &alphaKind, wantWarning: true},
		{name: "original kind omitted", serviceRef: true, wantWarning: true},
		{name: "beta converted for equivalent admission", serviceRef: true, requestKind: &betaKind},
		{name: "alpha without beta references", requestKind: &alphaKind},
		{name: "dry run also warns", serviceRef: true, requestKind: &alphaKind, dryRun: true, wantWarning: true},
		{name: "read failure does not block admission", serviceRef: true, requestKind: &alphaKind, missing: true},
		{name: "policy rejects update", serviceRef: true, requestKind: &alphaKind, denied: true},
		{name: "create", serviceRef: true, requestKind: &alphaKind, create: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, v1beta1.AddToScheme(scheme))
			server := newMCPServer("docker.io/any:latest")
			stored := &v1beta1.MCPServer{ObjectMeta: server.ObjectMeta}
			if tc.serviceRef {
				stored.Spec.ServiceRef = &v1beta1.ServiceReference{Name: "external"}
			}
			if tc.workloadRef {
				stored.Spec.WorkloadRef = &v1beta1.WorkloadReference{Name: "external", Kind: v1beta1.WorkloadKindDeployment}
			}
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if !tc.missing {
				builder.WithObjects(stored)
			}
			validator := &MCPServerCustomValidator{Reader: builder.Build()}
			if tc.denied {
				validator.Policy = &webhookpolicy.AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/"}}
			}
			operation := admissionv1.Update
			if tc.create {
				operation = admissionv1.Create
			}
			ctx := admission.NewContextWithRequest(testContext(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Kind: alphaKind, RequestKind: tc.requestKind, Operation: operation, DryRun: new(tc.dryRun),
			}})
			var err error
			var warnings admission.Warnings
			if tc.create {
				warnings, err = validator.ValidateCreate(ctx, server)
			} else {
				warnings, err = validator.ValidateUpdate(ctx, server.DeepCopy(), server)
			}
			if tc.denied {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.wantWarning {
				assert.Equal(t, admission.Warnings{
					"Updating through mcp.x-k8s.io/v1alpha1 may discard workloadRef or serviceRef; use mcp.x-k8s.io/v1beta1 instead",
				}, warnings)
			} else {
				assert.Empty(t, warnings)
			}
		})
	}
}

func TestValidateCreate_NoPolicy(t *testing.T) {
	v := &MCPServerCustomValidator{Policy: nil}
	warnings, err := v.ValidateCreate(testContext(), newMCPServer("docker.io/any:latest"))
	assert.NoError(t, err)
	assert.Nil(t, warnings)
}

func TestValidateCreate_EmptyPolicy(t *testing.T) {
	v := &MCPServerCustomValidator{Policy: &webhookpolicy.AdmissionPolicy{}}
	warnings, err := v.ValidateCreate(testContext(), newMCPServer("docker.io/any:latest"))
	assert.NoError(t, err)
	assert.Nil(t, warnings)
}

func TestValidateCreate_AllowlistAllowed(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			ImageAllowlist: []string{"ghcr.io/modelcontextprotocol/"},
		},
	}
	_, err := v.ValidateCreate(testContext(), newMCPServer("ghcr.io/modelcontextprotocol/server:v1"))
	assert.NoError(t, err)
}

func TestValidateCreate_AllowlistDenied(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			ImageAllowlist: []string{"ghcr.io/modelcontextprotocol/"},
		},
	}
	_, err := v.ValidateCreate(testContext(), newMCPServer("docker.io/malicious/server:latest"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match any allowed registry prefix")
}

func TestValidateCreate_DigestRequired_TagRejected(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			RequireImageDigest: true,
		},
	}
	_, err := v.ValidateCreate(testContext(), newMCPServer("ghcr.io/mcp/server:latest"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must use a digest reference (@sha256:<64 hex chars>)")
}

func TestValidateCreate_DigestRequired_DigestAllowed(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			RequireImageDigest: true,
		},
	}
	_, err := v.ValidateCreate(testContext(), newMCPServer("ghcr.io/mcp/server@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"))
	assert.NoError(t, err)
}

func TestValidateCreate_DigestNotRequired(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			RequireImageDigest: false,
		},
	}
	_, err := v.ValidateCreate(testContext(), newMCPServer("ghcr.io/mcp/server:latest"))
	assert.NoError(t, err)
}

func TestValidateCreate_MaxStorageMountsExceeded(t *testing.T) {
	maxMounts := 2
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			MaxStorageMounts: &maxMounts,
		},
	}
	mcpServer := newMCPServer("ghcr.io/mcp/server:v1")
	mcpServer.Spec.Config.Storage = []StorageMount{
		{Path: "/a", Source: StorageSource{Type: StorageTypeConfigMap, ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "a"}}}},
		{Path: "/b", Source: StorageSource{Type: StorageTypeConfigMap, ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "b"}}}},
		{Path: "/c", Source: StorageSource{Type: StorageTypeConfigMap, ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "c"}}}},
	}
	_, err := v.ValidateCreate(testContext(), mcpServer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed")
}

func TestValidateCreate_MaxStorageMountsWithinLimit(t *testing.T) {
	maxMounts := 3
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			MaxStorageMounts: &maxMounts,
		},
	}
	mcpServer := newMCPServer("ghcr.io/mcp/server:v1")
	mcpServer.Spec.Config.Storage = []StorageMount{
		{Path: "/a", Source: StorageSource{Type: StorageTypeConfigMap, ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "a"}}}},
	}
	_, err := v.ValidateCreate(testContext(), mcpServer)
	assert.NoError(t, err)
}

func TestValidateCreate_RequiredLabelMissing(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			RequiredLabels: []string{"app.kubernetes.io/managed-by"},
		},
	}
	_, err := v.ValidateCreate(testContext(), newMCPServer("ghcr.io/mcp/server:v1"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app.kubernetes.io/managed-by")
}

func TestValidateCreate_RequiredLabelPresent(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			RequiredLabels: []string{"app.kubernetes.io/managed-by"},
		},
	}
	mcpServer := newMCPServer("ghcr.io/mcp/server:v1")
	mcpServer.Labels = map[string]string{"app.kubernetes.io/managed-by": "operator"}
	_, err := v.ValidateCreate(testContext(), mcpServer)
	assert.NoError(t, err)
}

func TestValidateCreate_CombinedViolations(t *testing.T) {
	maxMounts := 1
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			ImageAllowlist:     []string{"ghcr.io/allowed/"},
			RequireImageDigest: true,
			MaxStorageMounts:   &maxMounts,
			RequiredLabels:     []string{"team"},
		},
	}
	mcpServer := newMCPServer("docker.io/bad/server:latest")
	mcpServer.Spec.Config.Storage = []StorageMount{
		{Path: "/a", Source: StorageSource{Type: StorageTypeConfigMap, ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "a"}}}},
		{Path: "/b", Source: StorageSource{Type: StorageTypeConfigMap, ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "b"}}}},
	}
	_, err := v.ValidateCreate(testContext(), mcpServer)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match any allowed registry prefix")
	assert.Contains(t, err.Error(), "must use a digest reference (@sha256:<64 hex chars>)")
	assert.Contains(t, err.Error(), "exceeds maximum allowed")
	assert.Contains(t, err.Error(), "team")
}

func TestValidateUpdate_ImageChangeDenied(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			ImageAllowlist: []string{"ghcr.io/allowed/"},
		},
	}
	old := newMCPServer("ghcr.io/allowed/server:v1")
	updated := newMCPServer("docker.io/malicious/server:v2")
	_, err := v.ValidateUpdate(testContext(), old, updated)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match any allowed registry prefix")
}

func TestValidateUpdate_ImageChangeAllowed(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			ImageAllowlist: []string{"ghcr.io/allowed/"},
		},
	}
	old := newMCPServer("ghcr.io/allowed/server:v1")
	updated := newMCPServer("ghcr.io/allowed/server:v2")
	_, err := v.ValidateUpdate(testContext(), old, updated)
	assert.NoError(t, err)
}

func TestValidateDelete_AlwaysAllowed(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			ImageAllowlist:     []string{"ghcr.io/only/"},
			RequireImageDigest: true,
		},
	}
	_, err := v.ValidateDelete(testContext(), newMCPServer("docker.io/any:latest"))
	assert.NoError(t, err)
}

func TestValidateCreate_NoContainerImage(t *testing.T) {
	v := &MCPServerCustomValidator{
		Policy: &webhookpolicy.AdmissionPolicy{
			ImageAllowlist:     []string{"ghcr.io/allowed/"},
			RequireImageDigest: true,
		},
	}
	mcpServer := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Source: Source{Type: SourceTypeContainerImage},
			Config: ServerConfig{Port: 8080},
		},
	}
	_, err := v.ValidateCreate(testContext(), mcpServer)
	assert.NoError(t, err)
}
