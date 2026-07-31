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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

func TestGatewayBindingCreation(t *testing.T) {
	const (
		configMapName = "gw-config"
		gwName        = "my-gateway"
		gwNamespace   = "gateway-system"
		hostname      = "mcp.example.com"
	)

	feature := features.New("Gateway binding creation").
		WithLabel("type", "gateway").
		WithLabel("component", "mcpgatewaybinding").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, gwName, gwNamespace, hostname)
			return f.SetupMCPServer(ctx, t, cfg, "test-gw-server", false,
				f.WithGateway("httproute", configMapName),
				f.WithPath("/mcp"),
			)
		}).
		Assess("MCPGatewayBinding is created and Registered", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			bindingName := server.Name + "-gateway-binding"
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: server.Namespace,
				},
			}
			f.WaitForBindingRegistered(ctx, t, r, binding, metav1.ConditionTrue)
			t.Logf("MCPGatewayBinding %s is Registered", bindingName)

			if err := r.Get(ctx, bindingName, server.Namespace, binding); err != nil {
				t.Fatalf("failed to get MCPGatewayBinding: %v", err)
			}
			if binding.Spec.Provider != "httproute" {
				t.Fatalf("expected provider httproute, got %s", binding.Spec.Provider)
			}
			if binding.Spec.MCPServerRef != server.Name {
				t.Fatalf("expected mcpServerRef %s, got %s", server.Name, binding.Spec.MCPServerRef)
			}
			if binding.Spec.ConfigRef != configMapName {
				t.Fatalf("expected configRef %s, got %s", configMapName, binding.Spec.ConfigRef)
			}

			return ctx
		}).
		Assess("HTTPRoute is created with correct spec", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			bindingName := server.Name + "-gateway-binding"
			route := &gatewayv1.HTTPRoute{}
			if err := r.Get(ctx, bindingName, server.Namespace, route); err != nil {
				t.Fatalf("HTTPRoute not found: %v", err)
			}

			if len(route.Spec.ParentRefs) != 1 {
				t.Fatalf("expected 1 parentRef, got %d", len(route.Spec.ParentRefs))
			}
			if string(route.Spec.ParentRefs[0].Name) != gwName {
				t.Fatalf("expected parentRef name %s, got %s", gwName, route.Spec.ParentRefs[0].Name)
			}
			if route.Spec.ParentRefs[0].Namespace == nil || string(*route.Spec.ParentRefs[0].Namespace) != gwNamespace {
				t.Fatal("expected parentRef namespace gateway-system")
			}

			if len(route.Spec.Rules) != 1 || len(route.Spec.Rules[0].BackendRefs) != 1 {
				t.Fatal("expected 1 rule with 1 backendRef")
			}
			if string(route.Spec.Rules[0].BackendRefs[0].Name) != server.Name {
				t.Fatalf("expected backendRef name %s, got %s", server.Name, route.Spec.Rules[0].BackendRefs[0].Name)
			}

			if len(route.Spec.Hostnames) != 1 || string(route.Spec.Hostnames[0]) != hostname {
				t.Fatalf("expected hostname %s, got %v", hostname, route.Spec.Hostnames)
			}

			ownerRef := metav1.GetControllerOf(route)
			if ownerRef == nil || ownerRef.Kind != "MCPGatewayBinding" {
				t.Fatal("HTTPRoute should be owned by MCPGatewayBinding")
			}
			t.Logf("HTTPRoute %s verified", bindingName)

			return ctx
		}).
		Assess("MCPServer reflects gateway status", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionTrue)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			if server.Status.GatewayBinding == nil {
				t.Fatal("expected GatewayBinding status to be set")
			}
			if server.Status.GatewayBinding.Provider != "httproute" {
				t.Fatalf("expected gateway binding provider httproute, got %s", server.Status.GatewayBinding.Provider)
			}

			f.AssertGatewayAddressURL(t, server, hostname, "/mcp")
			t.Logf("MCPServer gateway status verified: address=%s", server.Status.Address.URL)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestGatewayRemoval(t *testing.T) {
	const (
		configMapName = "gw-config"
		gwName        = "my-gateway"
		gwNamespace   = "gateway-system"
		hostname      = "mcp.example.com"
	)

	feature := features.New("Gateway removal").
		WithLabel("type", "gateway").
		WithLabel("component", "mcpgatewaybinding").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, gwName, gwNamespace, hostname)
			ctx = f.SetupMCPServer(ctx, t, cfg, "test-gw-remove", false,
				f.WithGateway("httproute", configMapName),
			)
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionTrue)
			return ctx
		}).
		Assess("remove gateway from spec", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.UpdateWithRetry(ctx, t, r, server, func(s *mcpv1alpha1.MCPServer) {
				s.Spec.Gateway = nil
			})
			t.Log("removed spec.gateway from MCPServer")
			return ctx
		}).
		Assess("binding and HTTPRoute are deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			bindingName := server.Name + "-gateway-binding"
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: server.Namespace,
				},
			}
			f.WaitForBindingDeleted(ctx, t, r, binding)
			t.Logf("MCPGatewayBinding %s deleted", bindingName)

			return ctx
		}).
		Assess("MCPServer address reverts to service URL", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertAddressURL(t, server, 3001)
			t.Logf("MCPServer address reverted to service URL: %s", server.Status.Address.URL)

			cond := f.GetMCPServerCondition(server, "GatewayRegistered")
			if cond != nil {
				t.Fatalf("expected no GatewayRegistered condition after removal, but found one: %s", cond.Status)
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
