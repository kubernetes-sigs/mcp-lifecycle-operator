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

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

func TestImagePullFailure(t *testing.T) {
	feature := features.New("MCPServer image pull failure").
		WithLabel("type", "failure").
		WithLabel("failure", "image-pull").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "img-pull-fail", false,
				f.WithImage("invalid.example.com/nonexistent/image:v0.0.1"),
			)
		}).
		Assess("Ready condition reports image pull failure", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionMessageContains(ctx, t, r, server,
				"Ready", metav1.ConditionFalse, "DeploymentUnavailable", "Image pull failed",
				3*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			ready := f.GetMCPServerCondition(server, "Ready")
			t.Logf("Ready condition: status=%s reason=%s message=%q", ready.Status, ready.Reason, ready.Message)

			return ctx
		}).
		Assess("Accepted condition is True", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			accepted := f.GetMCPServerCondition(server, "Accepted")
			if accepted == nil || accepted.Status != metav1.ConditionTrue {
				t.Fatal("expected Accepted=True (config is valid, only image does not exist)")
			}

			return ctx
		}).
		Assess("failure condition is stable", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.AssertConditionStable(ctx, t, r, server, "Ready", metav1.ConditionFalse, 15*time.Second)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestContainerCrashLoop(t *testing.T) {
	feature := features.New("MCPServer container crash loop").
		WithLabel("type", "failure").
		WithLabel("failure", "crash-loop").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "crash-loop", false,
				f.WithImage("docker.io/library/busybox:1.37"),
				f.WithArguments("sh", "-c", "exit 1"),
				f.WithPort(8080),
				f.WithSecurityContext(&corev1.SecurityContext{}),
			)
		}).
		Assess("Ready condition reports crash loop", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionMessageContains(ctx, t, r, server,
				"Ready", metav1.ConditionFalse, "DeploymentUnavailable", "Container crashing",
				4*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			ready := f.GetMCPServerCondition(server, "Ready")
			t.Logf("Ready condition: status=%s reason=%s message=%q", ready.Status, ready.Reason, ready.Message)

			if !strings.Contains(ready.Message, "exit code") {
				t.Errorf("expected message to contain exit code details, got %q", ready.Message)
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestMCPHandshakeFailure(t *testing.T) {
	feature := features.New("MCPServer handshake failure").
		WithLabel("type", "failure").
		WithLabel("failure", "mcp-handshake").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "handshake-fail", false,
				f.WithPath("/not-mcp"),
			)
		}).
		Assess("Ready condition reports MCP endpoint unavailable", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Ready", metav1.ConditionFalse, "MCPEndpointUnavailable",
				5*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			ready := f.GetMCPServerCondition(server, "Ready")
			t.Logf("Ready condition: status=%s reason=%s message=%q", ready.Status, ready.Reason, ready.Message)

			if !strings.Contains(ready.Message, "MCP endpoint is not serving a valid MCP protocol") {
				t.Errorf("expected message about MCP protocol failure, got %q", ready.Message)
			}

			return ctx
		}).
		Assess("Accepted condition is True", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			accepted := f.GetMCPServerCondition(server, "Accepted")
			if accepted == nil || accepted.Status != metav1.ConditionTrue {
				t.Fatal("expected Accepted=True (config is valid)")
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestMissingConfigMapReference(t *testing.T) {
	feature := features.New("MCPServer missing ConfigMap reference").
		WithLabel("type", "failure").
		WithLabel("failure", "missing-configmap").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "missing-cm", false,
				f.WithEnvFrom(corev1.EnvFromSource{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "nonexistent-configmap",
						},
					},
				}),
			)
		}).
		Assess("Accepted condition is False with reason Invalid", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid",
				2*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			accepted := f.GetMCPServerCondition(server, "Accepted")
			t.Logf("Accepted condition: status=%s reason=%s message=%q", accepted.Status, accepted.Reason, accepted.Message)

			if !strings.Contains(accepted.Message, "nonexistent-configmap") {
				t.Errorf("expected message to mention the missing ConfigMap, got %q", accepted.Message)
			}

			return ctx
		}).
		Assess("Ready condition is False with reason ConfigurationInvalid", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Ready", metav1.ConditionFalse, "ConfigurationInvalid",
				30*time.Second)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestMissingSecretReference(t *testing.T) {
	feature := features.New("MCPServer missing Secret reference").
		WithLabel("type", "failure").
		WithLabel("failure", "missing-secret").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "missing-secret", false,
				f.WithEnvFrom(corev1.EnvFromSource{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "nonexistent-secret",
						},
					},
				}),
			)
		}).
		Assess("Accepted condition is False with reason Invalid", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid",
				2*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			accepted := f.GetMCPServerCondition(server, "Accepted")
			t.Logf("Accepted condition: status=%s reason=%s message=%q", accepted.Status, accepted.Reason, accepted.Message)

			if !strings.Contains(accepted.Message, "nonexistent-secret") {
				t.Errorf("expected message to mention the missing Secret, got %q", accepted.Message)
			}

			return ctx
		}).
		Assess("Ready condition is False with reason ConfigurationInvalid", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Ready", metav1.ConditionFalse, "ConfigurationInvalid",
				30*time.Second)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestMissingStorageConfigMapReference(t *testing.T) {
	feature := features.New("MCPServer missing storage ConfigMap reference").
		WithLabel("type", "failure").
		WithLabel("failure", "missing-storage-configmap").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "missing-storage", false,
				f.WithStorage(mcpv1alpha1.StorageMount{
					Path: "/etc/mcp-config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "nonexistent-storage-cm",
							},
						},
					},
				}),
			)
		}).
		Assess("Accepted condition is False with reason Invalid", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid",
				2*time.Minute)

			return ctx
		}).
		Assess("Ready condition is False with reason ConfigurationInvalid", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Ready", metav1.ConditionFalse, "ConfigurationInvalid",
				30*time.Second)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestRecoveryFromMissingConfigMap(t *testing.T) {
	feature := features.New("MCPServer recovery from missing ConfigMap").
		WithLabel("type", "recovery").
		WithLabel("failure", "missing-configmap").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "recovery-cm", false,
				f.WithEnvFrom(corev1.EnvFromSource{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "recovery-configmap",
						},
					},
				}),
			)
		}).
		Assess("initially Accepted=False", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid",
				2*time.Minute)
			t.Log("confirmed Accepted=False due to missing ConfigMap")

			return ctx
		}).
		Assess("create missing ConfigMap and recover", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			ns := server.Namespace
			r := cfg.Client().Resources()

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recovery-configmap",
					Namespace: ns,
				},
				Data: map[string]string{
					"key": "value",
				},
			}
			if err := r.Create(ctx, cm); err != nil {
				t.Fatalf("failed to create ConfigMap: %v", err)
			}
			t.Log("created recovery-configmap")

			f.WaitForMCPServerCondition(ctx, t, r, server,
				"Accepted", metav1.ConditionTrue, 2*time.Minute)
			t.Log("Accepted=True after ConfigMap creation")

			f.WaitForMCPServerCondition(ctx, t, r, server,
				"Ready", metav1.ConditionTrue, 3*time.Minute)
			t.Log("Ready=True — full recovery complete")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestRecoveryFromImagePullFailure(t *testing.T) {
	feature := features.New("MCPServer recovery from image pull failure").
		WithLabel("type", "recovery").
		WithLabel("failure", "image-pull").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "recovery-img", false,
				f.WithImage("invalid.example.com/nonexistent/image:v0.0.1"),
			)
		}).
		Assess("initially Ready=False with image pull failure", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionMessageContains(ctx, t, r, server,
				"Ready", metav1.ConditionFalse, "DeploymentUnavailable", "Image pull failed",
				3*time.Minute)
			t.Log("confirmed Ready=False with image pull failure")

			return ctx
		}).
		Assess("update to valid image and recover", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			server.Spec.Source.ContainerImage.Ref = "quay.io/matzew/mcp-everything:latest"
			if err := r.Update(ctx, server); err != nil {
				t.Fatalf("failed to update MCPServer image: %v", err)
			}
			t.Log("updated image to quay.io/matzew/mcp-everything:latest")

			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server, 5*time.Minute)
			t.Log("Ready=True — recovery from image pull failure complete")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
