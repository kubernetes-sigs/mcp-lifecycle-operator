//go:build e2e && e2e_gateway

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
	"net/url"
	"os"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scope"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

func TestGatewayConformanceBindingLifecycle(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-conformance-config"

	feature := features.New("Gateway conformance: binding lifecycle").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName, _ := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
			prov.ConfigData["section-name"] = listenerName
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, prov.ConfigData)
			return f.SetupMCPServer(ctx, t, cfg, "conformance-lifecycle", false,
				f.WithGateway(prov.Name, configMapName),
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

			if err := r.Get(ctx, bindingName, server.Namespace, binding); err != nil {
				t.Fatalf("failed to get MCPGatewayBinding: %v", err)
			}
			if binding.Spec.Provider != prov.Name {
				t.Fatalf("expected provider %s, got %s", prov.Name, binding.Spec.Provider)
			}
			if binding.Spec.MCPServerRef != server.Name {
				t.Fatalf("expected mcpServerRef %s, got %s", server.Name, binding.Spec.MCPServerRef)
			}
			t.Logf("MCPGatewayBinding %s is Registered", bindingName)
			return ctx
		}).
		Assess("MCPServer reflects gateway status", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			if server.Status.Address == nil || server.Status.Address.URL == "" {
				t.Fatal("expected status.address.url to be set")
			}
			t.Logf("MCPServer gateway status verified: address=%s", server.Status.Address.URL)
			return ctx
		}).
		Assess("MCPServer is fully ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)
			t.Log("MCPServer is Available and Verified")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestGatewayConformanceRemoval(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-removal-config"

	feature := features.New("Gateway conformance: removal").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName, _ := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
			prov.ConfigData["section-name"] = listenerName
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, prov.ConfigData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "conformance-removal", false,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionTrue)
			return ctx
		}).
		Assess("remove gateway from spec", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.UpdateWithRetry(ctx, t, r, server, func(s *mcpv1beta1.MCPServer) {
				s.Spec.Gateway = nil
			})
			t.Log("removed spec.gateway from MCPServer")
			return ctx
		}).
		Assess("binding is deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
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

			f.AssertAddressURL(t, server, 8080)
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

func TestGatewayConformanceHTTPReachability(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-reachability-config"

	var gwAddr string

	feature := features.New("Gateway conformance: HTTP reachability").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName, addr := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
			prov.ConfigData["section-name"] = listenerName
			gwAddr = addr
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, prov.ConfigData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "conformance-http", true,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)
			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionTrue)
			return ctx
		}).
		Assess("MCP handshake through gateway", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			if server.Status.Address == nil || server.Status.Address.URL == "" {
				t.Fatal("status.address.url is not set")
			}

			parsed, err := url.Parse(server.Status.Address.URL)
			if err != nil {
				t.Fatalf("failed to parse status.address.url %q: %v", server.Status.Address.URL, err)
			}

			f.AssertMCPReachable(ctx, t, gwAddr, parsed.Hostname(), parsed.Path)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestGatewayConformanceHostnameSeparation(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-hostname-sep-config"

	var gwAddr string

	feature := features.New("Gateway conformance: hostname separation").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName, addr := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
			gwAddr = addr

			configData := map[string]string{
				"gateway-name":      prov.ConfigData["gateway-name"],
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      listenerName,
				"route-hostname":    "internal.mcp.local",
				"public-hostname":   "public.example.com",
			}
			if prefix, ok := prov.ConfigData["prefix"]; ok {
				configData["prefix"] = prefix
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "hostname-sep", true,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			return ctx
		}).
		Assess("status URL uses public-hostname", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertGatewayAddressURL(t, server, "public.example.com", "/mcp")
			t.Logf("status.address.url correctly uses public-hostname: %s", server.Status.Address.URL)
			return ctx
		}).
		Assess("HTTPRoute uses route-hostname", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			bindingName := server.Name + "-gateway-binding"
			route := &gatewayv1.HTTPRoute{}
			if err := r.Get(ctx, bindingName, server.Namespace, route); err != nil {
				t.Fatalf("HTTPRoute not found: %v", err)
			}

			if len(route.Spec.Hostnames) != 1 || string(route.Spec.Hostnames[0]) != "internal.mcp.local" {
				t.Fatalf("expected HTTPRoute hostname internal.mcp.local, got %v", route.Spec.Hostnames)
			}
			t.Log("HTTPRoute correctly uses route-hostname: internal.mcp.local")
			return ctx
		}).
		Assess("MCP server is reachable via route hostname", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			f.AssertMCPReachable(ctx, t, gwAddr, "internal.mcp.local", "/mcp")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestGatewayConformanceFallbackToRouteHostname(t *testing.T) {
	if provider := os.Getenv("GATEWAY_PROVIDER"); provider != "httproute" {
		t.Skipf("skipping: fallback to route-hostname only applies to httproute provider (got %s)", provider)
	}

	prov := f.ActiveProvider(t)
	const configMapName = "gw-fallback-route-config"

	feature := features.New("Gateway conformance: fallback to route-hostname").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName, _ := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])

			configData := map[string]string{
				"gateway-name":      prov.ConfigData["gateway-name"],
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      listenerName,
				"route-hostname":    "route.mcp.local",
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "fallback-route", true,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			return ctx
		}).
		Assess("status URL uses route-hostname as fallback", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertGatewayAddressURL(t, server, "route.mcp.local", "/mcp")
			t.Logf("status.address.url correctly falls back to route-hostname: %s", server.Status.Address.URL)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestGatewayConformanceFallbackToGatewayAddress(t *testing.T) {
	if provider := os.Getenv("GATEWAY_PROVIDER"); provider != "httproute" {
		t.Skipf("skipping: fallback to gateway address only applies to httproute provider (got %s)", provider)
	}

	prov := f.ActiveProvider(t)
	const configMapName = "gw-fallback-addr-config"

	var gwAddr string

	feature := features.New("Gateway conformance: fallback to gateway address").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName, addr := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
			gwAddr = addr

			configData := map[string]string{
				"gateway-name":      prov.ConfigData["gateway-name"],
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      listenerName,
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "fallback-addr", true,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			return ctx
		}).
		Assess("status URL contains gateway LoadBalancer address", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			statusURL := server.Status.Address.URL
			if !strings.Contains(statusURL, gwAddr) {
				t.Fatalf("expected status.address.url to contain gateway address %s, got %s", gwAddr, statusURL)
			}
			t.Logf("status.address.url uses gateway LoadBalancer address: %s", statusURL)
			return ctx
		}).
		Assess("MCP server is reachable via gateway address", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			f.AssertMCPReachable(ctx, t, gwAddr, gwAddr, "/mcp")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestGatewayConformanceRecoverOnConfigMapUpdate(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-recover-config"

	feature := features.New("Gateway conformance: recover on ConfigMap update").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName, _ := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])

			configData := map[string]string{
				"gateway-name":      "nonexistent-gateway",
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      listenerName,
				"route-hostname":    "recover.mcp.local",
				"public-hostname":   "recover.mcp.local",
			}
			if prefix, ok := prov.ConfigData["prefix"]; ok {
				configData["prefix"] = prefix
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "recover-cfg", false,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)
			return ctx
		}).
		Assess("GatewayRegistered=False with invalid gateway", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionFalse)
			t.Log("GatewayRegistered=False as expected with invalid gateway reference")
			return ctx
		}).
		Assess("update ConfigMap to valid gateway", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName, _ := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
			configData := map[string]string{
				"gateway-name":      prov.ConfigData["gateway-name"],
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      listenerName,
				"route-hostname":    "recover.mcp.local",
				"public-hostname":   "recover.mcp.local",
			}
			if prefix, ok := prov.ConfigData["prefix"]; ok {
				configData["prefix"] = prefix
			}
			f.UpdateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			t.Log("updated ConfigMap to valid gateway reference")
			return ctx
		}).
		Assess("GatewayRegistered recovers to True", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			f.AssertGatewayAddressURL(t, server, "recover.mcp.local", "/mcp")
			t.Logf("GatewayRegistered recovered to True with address: %s", server.Status.Address.URL)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestGatewayConformanceConfigMapUpdateTriggersStatusUpdate(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-cm-update-config"

	feature := features.New("Gateway conformance: ConfigMap update triggers status update").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName, _ := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])

			configData := map[string]string{
				"gateway-name":      prov.ConfigData["gateway-name"],
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      listenerName,
				"route-hostname":    "first.mcp.local",
				"public-hostname":   "first.mcp.local",
			}
			if prefix, ok := prov.ConfigData["prefix"]; ok {
				configData["prefix"] = prefix
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "cm-update", true,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			return ctx
		}).
		Assess("initial status uses first hostname", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertGatewayAddressURL(t, server, "first.mcp.local", "/mcp")
			t.Logf("initial status uses first hostname: %s", server.Status.Address.URL)
			return ctx
		}).
		Assess("update ConfigMap to second hostname", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			listenerName := prov.ConfigData["section-name"]
			configData := map[string]string{
				"gateway-name":      prov.ConfigData["gateway-name"],
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      listenerName,
				"route-hostname":    "second.mcp.local",
				"public-hostname":   "second.mcp.local",
			}
			if prefix, ok := prov.ConfigData["prefix"]; ok {
				configData["prefix"] = prefix
			}
			f.UpdateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			t.Log("updated ConfigMap to second.mcp.local")
			return ctx
		}).
		Assess("status URL updates to second hostname", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerAddressContains(ctx, t, r, server, "second.mcp.local")

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertGatewayAddressURL(t, server, "second.mcp.local", "/mcp")
			t.Logf("status URL updated to second hostname: %s", server.Status.Address.URL)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
