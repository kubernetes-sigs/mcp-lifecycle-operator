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

package conformance

import (
	"context"
	"net/url"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scope"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

// RunGatewayConformanceSuite runs the shared gateway conformance tests against
// the given provider. Each test is a subtest of t, individually runnable
// with -run.
func RunGatewayConformanceSuite(t *testing.T, testenv env.Environment, prov Provider) {
	t.Run("binding lifecycle", func(t *testing.T) {
		runBindingLifecycle(t, testenv, prov)
	})
	t.Run("removal", func(t *testing.T) {
		runRemoval(t, testenv, prov)
	})
	t.Run("HTTP reachability", func(t *testing.T) {
		runHTTPReachability(t, testenv, prov)
	})
	t.Run("hostname separation", func(t *testing.T) {
		runHostnameSeparation(t, testenv, prov)
	})
	t.Run("recovery", func(t *testing.T) {
		runRecovery(t, testenv, prov)
	})
	for _, tc := range []byoConfiguration{
		{name: "BYO workload", externalWorkload: true},
		{name: "BYO workload and Service", externalWorkload: true, externalService: true},
		{name: "BYO Service", externalService: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runBYO(t, testenv, prov, tc)
		})
	}
}

func runBindingLifecycle(t *testing.T, testenv env.Environment, prov Provider) {
	const configMapName = "gw-conformance-config"

	feature := features.New("Gateway conformance: binding lifecycle").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			prov.Setup(ctx, t, cfg, ns)
			configData := prov.ConfigData()
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			return f.SetupMCPServer(ctx, t, cfg, "conformance-lifecycle", false,
				f.WithGateway(prov.Name(), configMapName),
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
			if binding.Spec.Provider != prov.Name() {
				t.Fatalf("expected provider %s, got %s", prov.Name(), binding.Spec.Provider)
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

func runRemoval(t *testing.T, testenv env.Environment, prov Provider) {
	const configMapName = "gw-removal-config"

	feature := features.New("Gateway conformance: removal").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			prov.Setup(ctx, t, cfg, ns)
			configData := prov.ConfigData()
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "conformance-removal", false,
				f.WithGateway(prov.Name(), configMapName),
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

func runHTTPReachability(t *testing.T, testenv env.Environment, prov Provider) {
	const configMapName = "gw-reachability-config"

	var gwAddr string

	feature := features.New("Gateway conformance: HTTP reachability").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			prov.Setup(ctx, t, cfg, ns)
			gwAddr = prov.GatewayAddress(ctx, t, cfg)
			configData := prov.ConfigData()
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			createBackendPodReader(ctx, t, cfg.Client().Resources(), ns)
			ctx = f.SetupMCPServer(ctx, t, cfg, "conformance-http", true,
				f.WithGateway(prov.Name(), configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)
			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionTrue)
			return ctx
		}).
		Assess("backend tool call succeeds through gateway", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
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

			assertBackendPodListed(ctx, t, gwAddr, parsed.Hostname(), parsed.Path, server.Namespace, cfg.Client().Resources(),
				map[string]string{mcpcontroller.LabelKeyMCPServer: server.Name})

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func runHostnameSeparation(t *testing.T, testenv env.Environment, prov Provider) {
	const configMapName = "gw-hostname-sep-config"

	var gwAddr string

	feature := features.New("Gateway conformance: hostname separation").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			prov.Setup(ctx, t, cfg, ns)
			gwAddr = prov.GatewayAddress(ctx, t, cfg)
			configData := prov.ConfigData()
			configData["route-hostname"] = "internal.mcp.local"
			configData["public-hostname"] = "public.example.com"
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			createBackendPodReader(ctx, t, cfg.Client().Resources(), ns)
			ctx = f.SetupMCPServer(ctx, t, cfg, "hostname-sep", true,
				f.WithGateway(prov.Name(), configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			return ctx
		}).
		Assess("status URL does not use route-hostname", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			parsed, err := url.Parse(server.Status.Address.URL)
			if err != nil {
				t.Fatalf("failed to parse status URL %q: %v", server.Status.Address.URL, err)
			}
			if parsed.Hostname() == "internal.mcp.local" {
				t.Fatalf("status URL hostname must differ from route-hostname, got %s", server.Status.Address.URL)
			}
			t.Logf("status URL hostname (%s) differs from route-hostname (internal.mcp.local)", parsed.Hostname())
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
		Assess("backend tool call succeeds via route hostname", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			assertBackendPodListed(ctx, t, gwAddr, "internal.mcp.local", "/mcp", server.Namespace, cfg.Client().Resources(),
				map[string]string{mcpcontroller.LabelKeyMCPServer: server.Name})
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func runRecovery(t *testing.T, testenv env.Environment, prov Provider) {
	const configMapName = "gw-recover-config"

	feature := features.New("Gateway conformance: recover on ConfigMap update").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			prov.Setup(ctx, t, cfg, ns)
			invalidConfig := prov.InvalidConfigData()
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, invalidConfig)
			ctx = f.SetupMCPServer(ctx, t, cfg, "recover-cfg", false,
				f.WithGateway(prov.Name(), configMapName),
				f.WithPath("/mcp"),
			)
			return ctx
		}).
		Assess("GatewayRegistered=False with invalid config", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionFalse)
			t.Log("GatewayRegistered=False as expected with invalid config")
			return ctx
		}).
		Assess("update ConfigMap to valid config", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			validConfig := prov.ConfigData()
			f.UpdateGatewayConfigMap(ctx, t, cfg, configMapName, ns, validConfig)
			t.Log("updated ConfigMap to valid config")
			return ctx
		}).
		Assess("GatewayRegistered recovers to True", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			t.Logf("GatewayRegistered recovered to True with address set")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
