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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

func TestMCPServerHappyPath(t *testing.T) {
	feature := features.New("MCPServer happy path").
		WithLabel("type", "lifecycle").
		WithLabel("component", "mcpserver").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "test-server", false)
		}).
		Assess("MCPServer becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerCondition(ctx, t, r, server, "Ready", metav1.ConditionTrue)
			t.Log("MCPServer is Ready")

			return ctx
		}).
		Assess("Deployment and Service are created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			ns := server.Namespace
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, ns, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}
			t.Logf("Deployment %s exists with %d ready replicas", dep.Name, dep.Status.ReadyReplicas)

			svc := &corev1.Service{}
			if err := r.Get(ctx, server.Name, ns, svc); err != nil {
				t.Fatalf("Service not found: %v", err)
			}
			if svc.Spec.Type != corev1.ServiceTypeClusterIP {
				t.Fatalf("expected Service type ClusterIP, got %s", svc.Spec.Type)
			}
			t.Logf("Service %s exists with type %s", svc.Name, svc.Spec.Type)

			return ctx
		}).
		Assess("status fields are populated correctly", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			// Re-fetch to get latest status.
			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			// observedGeneration matches generation
			if server.Status.ObservedGeneration != server.Generation {
				t.Fatalf("observedGeneration %d != generation %d",
					server.Status.ObservedGeneration, server.Generation)
			}

			// address URL is correct
			f.AssertAddressURL(t, server, 3001)

			// Accepted condition is True
			accepted := f.GetMCPServerCondition(server, "Accepted")
			if accepted == nil || accepted.Status != metav1.ConditionTrue {
				t.Fatal("Accepted condition is not True")
			}

			t.Logf("status: observedGeneration=%d, address=%s, Accepted=True",
				server.Status.ObservedGeneration, server.Status.Address.URL)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestMCPServerUpdatePort(t *testing.T) {
	feature := features.New("MCPServer port update").
		WithLabel("type", "update").
		WithLabel("component", "mcpserver").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "test-server", true)
		}).
		Assess("update port from 3001 to 3002", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.UpdateWithRetry(ctx, t, r, server, func(s *mcpv1alpha1.MCPServer) {
				s.Spec.Config.Port = 3002
			})
			t.Log("updated MCPServer port to 3002")

			return ctx
		}).
		Assess("controller reconciles the port change", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerReconciled(ctx, t, r, server)
			t.Log("MCPServer reconciled after port update")

			return ctx
		}).
		Assess("resources reflect updated port", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertAddressURL(t, server, 3002)

			svc := &corev1.Service{}
			if err := r.Get(ctx, server.Name, server.Namespace, svc); err != nil {
				t.Fatalf("Service not found: %v", err)
			}
			if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].Port != 3002 {
				actual := int32(0)
				if len(svc.Spec.Ports) > 0 {
					actual = svc.Spec.Ports[0].Port
				}
				t.Fatalf("expected Service port 3002, got %d", actual)
			}

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}
			foundPort := false
			for _, container := range dep.Spec.Template.Spec.Containers {
				for _, port := range container.Ports {
					if port.ContainerPort == 3002 {
						foundPort = true
						break
					}
				}
				if foundPort {
					break
				}
			}
			if !foundPort {
				t.Fatal("expected a container port 3002 in the Deployment")
			}

			t.Logf("port update verified: address=%s, servicePort=%d, containerPort=3002",
				server.Status.Address.URL, svc.Spec.Ports[0].Port)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
