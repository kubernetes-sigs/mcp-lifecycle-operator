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
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

func TestNetworkPolicyCreated(t *testing.T) {
	feature := features.New("MCPServer creates NetworkPolicy").
		WithLabel("type", "networkpolicy").
		WithLabel("component", "mcpserver").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "netpol-test", true)
		}).
		Assess("NetworkPolicy exists with correct spec", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			netpol := &networkingv1.NetworkPolicy{}
			if err := r.Get(ctx, server.Name, server.Namespace, netpol); err != nil {
				t.Fatalf("NetworkPolicy not found: %v", err)
			}

			// Verify podSelector targets MCP server pods
			if val, ok := netpol.Spec.PodSelector.MatchLabels["mcp-server"]; !ok || val != server.Name {
				t.Fatalf("expected podSelector {mcp-server: %s}, got %v", server.Name, netpol.Spec.PodSelector.MatchLabels)
			}

			// Verify only Ingress policyType
			if len(netpol.Spec.PolicyTypes) != 1 || netpol.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
				t.Fatalf("expected policyTypes [Ingress], got %v", netpol.Spec.PolicyTypes)
			}

			// Verify ingress allows only the configured port
			expectedPort := int(server.Spec.Config.Port)
			if len(netpol.Spec.Ingress) != 1 {
				t.Fatalf("expected 1 ingress rule, got %d", len(netpol.Spec.Ingress))
			}
			rule := netpol.Spec.Ingress[0]
			if len(rule.Ports) != 1 {
				t.Fatalf("expected 1 port in ingress rule, got %d", len(rule.Ports))
			}
			if rule.Ports[0].Port.IntValue() != expectedPort {
				t.Fatalf("expected ingress port %d, got %d", expectedPort, rule.Ports[0].Port.IntValue())
			}
			if *rule.Ports[0].Protocol != corev1.ProtocolTCP {
				t.Fatalf("expected protocol TCP, got %s", *rule.Ports[0].Protocol)
			}

			// Verify ingress From is empty (all sources allowed on MCP port)
			if len(rule.From) != 0 {
				t.Fatalf("expected empty From (allow all sources), got %d entries", len(rule.From))
			}

			// Verify owner reference
			if len(netpol.OwnerReferences) == 0 {
				t.Fatal("expected owner reference on NetworkPolicy")
			}
			if netpol.OwnerReferences[0].Name != server.Name {
				t.Fatalf("expected owner %s, got %s", server.Name, netpol.OwnerReferences[0].Name)
			}

			t.Logf("NetworkPolicy %s exists with correct spec: podSelector={mcp-server: %s}, port=3001/TCP, ingress-only",
				netpol.Name, server.Name)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestNetworkPolicyPortUpdate(t *testing.T) {
	feature := features.New("MCPServer NetworkPolicy port update").
		WithLabel("type", "networkpolicy").
		WithLabel("scenario", "port-update").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "netpol-port", true)
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
		Assess("NetworkPolicy reflects updated port after reconciliation", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			netpol := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
			}
			err := wait.For(
				conditions.New(r).ResourceMatch(netpol, func(obj k8s.Object) bool {
					np := obj.(*networkingv1.NetworkPolicy)
					if len(np.Spec.Ingress) != 1 || len(np.Spec.Ingress[0].Ports) != 1 {
						return false
					}
					return np.Spec.Ingress[0].Ports[0].Port.IntValue() == 3002
				}),
				wait.WithTimeout(1*time.Minute),
				wait.WithInterval(2*time.Second),
				wait.WithContext(ctx),
			)
			if err != nil {
				t.Fatalf("NetworkPolicy port did not update to 3002: %v", err)
			}

			t.Log("NetworkPolicy port updated to 3002")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestNetworkPolicyGarbageCollected(t *testing.T) {
	feature := features.New("MCPServer NetworkPolicy garbage collection").
		WithLabel("type", "networkpolicy").
		WithLabel("scenario", "gc").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "netpol-gc", true)
		}).
		Assess("NetworkPolicy exists", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			netpol := &networkingv1.NetworkPolicy{}
			if err := r.Get(ctx, server.Name, server.Namespace, netpol); err != nil {
				t.Fatalf("NetworkPolicy not found: %v", err)
			}
			t.Logf("NetworkPolicy %s exists", netpol.Name)
			return ctx
		}).
		Assess("NetworkPolicy is deleted when MCPServer is deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Delete(ctx, server); err != nil {
				t.Fatalf("failed to delete MCPServer: %v", err)
			}
			t.Logf("deleted MCPServer %s/%s", server.Namespace, server.Name)

			netpol := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
			}
			err := wait.For(
				conditions.New(r).ResourceDeleted(netpol),
				wait.WithTimeout(1*time.Minute),
				wait.WithInterval(2*time.Second),
				wait.WithContext(ctx),
			)
			if err != nil {
				t.Fatalf("NetworkPolicy was not garbage collected: %v", err)
			}
			t.Log("NetworkPolicy was garbage collected")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
