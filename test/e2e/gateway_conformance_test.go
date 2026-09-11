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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
			prov.ConfigData["section-name"] = f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
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

			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionTrue)

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
			prov.ConfigData["section-name"] = f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
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

	feature := features.New("Gateway conformance: HTTP reachability").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.GatewayConformance).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			prov.ConfigData["section-name"] = f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
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

			httpClient, proxyURL := f.GatewayProxyHTTPClient(t, cfg, prov.GatewayService, parsed.Path)
			httpClient = f.WithHostOverride(httpClient, parsed.Hostname())

			mcpClient := mcp.NewClient(
				&mcp.Implementation{
					Name:    "e2e-gateway-test-client",
					Version: "v0.0.1",
				},
				nil,
			)

			transport := &mcp.StreamableClientTransport{
				Endpoint:   proxyURL,
				HTTPClient: httpClient,
			}

			connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			session, err := mcpClient.Connect(connectCtx, transport, nil)
			if err != nil {
				t.Fatalf("failed MCP handshake through gateway: %v", err)
			}
			defer session.Close()

			initResult := session.InitializeResult()
			if initResult == nil {
				t.Fatal("InitializeResult is nil")
			}
			t.Logf("MCP handshake through gateway succeeded: server=%s version=%s",
				initResult.ServerInfo.Name, initResult.ServerInfo.Version)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
