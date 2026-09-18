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
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scope"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

var registerKuadrantOnce sync.Once

func ensureKuadrantScheme(t *testing.T, scheme *runtime.Scheme) {
	t.Helper()
	registerKuadrantOnce.Do(func() {
		if err := kuadrantapi.AddToScheme(scheme); err != nil {
			t.Fatalf("failed to register Kuadrant types: %v", err)
		}
	})
}

func TestKuadrantProviderResources(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-kuadrant-config"

	feature := features.New("Kuadrant provider: resources").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.Kuadrant).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ensureKuadrantScheme(t, cfg.Client().Resources().GetScheme())

			ns := ctx.Value(f.NsKey).(string)
			listenerName, _ := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
			prov.ConfigData["section-name"] = listenerName
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, prov.ConfigData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "kuadrant-resources", false,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

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
			return ctx
		}).
		Assess("MCPServerRegistration is created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			bindingName := server.Name + "-gateway-binding"
			reg := &kuadrantapi.MCPServerRegistration{}
			if err := r.Get(ctx, bindingName, server.Namespace, reg); err != nil {
				t.Fatalf("MCPServerRegistration not found: %v", err)
			}

			if reg.Spec.TargetRef.Group != "gateway.networking.k8s.io" {
				t.Fatalf("expected targetRef group gateway.networking.k8s.io, got %s", reg.Spec.TargetRef.Group)
			}
			if reg.Spec.TargetRef.Kind != "HTTPRoute" {
				t.Fatalf("expected targetRef kind HTTPRoute, got %s", reg.Spec.TargetRef.Kind)
			}
			if reg.Spec.TargetRef.Name != bindingName {
				t.Fatalf("expected targetRef name %s, got %s", bindingName, reg.Spec.TargetRef.Name)
			}
			if reg.Spec.Path != "/mcp" {
				t.Fatalf("expected path /mcp, got %s", reg.Spec.Path)
			}
			if reg.Spec.State != "Enabled" {
				t.Fatalf("expected state Enabled, got %s", reg.Spec.State)
			}

			ownerRef := metav1.GetControllerOf(reg)
			if ownerRef == nil || ownerRef.Kind != "MCPGatewayBinding" {
				t.Fatal("MCPServerRegistration should be owned by MCPGatewayBinding")
			}
			t.Logf("MCPServerRegistration %s verified", bindingName)
			return ctx
		}).
		Assess("HTTPRoute is created with correct owner", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			bindingName := server.Name + "-gateway-binding"
			route := &gatewayv1.HTTPRoute{}
			if err := r.Get(ctx, bindingName, server.Namespace, route); err != nil {
				t.Fatalf("HTTPRoute not found: %v", err)
			}

			ownerRef := metav1.GetControllerOf(route)
			if ownerRef == nil || ownerRef.Kind != "MCPGatewayBinding" {
				t.Fatal("HTTPRoute should be owned by MCPGatewayBinding")
			}
			t.Logf("HTTPRoute %s verified", bindingName)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestKuadrantAutoConstructedHostname(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-auto-hostname-config"

	feature := features.New("Kuadrant: auto-constructed hostname from wildcard listener").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.Kuadrant).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ensureKuadrantScheme(t, cfg.Client().Resources().GetScheme())

			ns := ctx.Value(f.NsKey).(string)
			wildcard := gatewayv1.Hostname("*.mcp.local")
			f.EnsureMultiListenerGateway(ctx, t, cfg,
				"kuadrant-wildcard-gw", prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"],
				[]f.ListenerSpec{
					{Name: "mcps", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: &wildcard},
				},
			)

			configData := map[string]string{
				"gateway-name":      "kuadrant-wildcard-gw",
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      "mcps",
				"prefix":            prov.ConfigData["prefix"],
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "auto-host", false,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)
			return ctx
		}).
		Assess("HTTPRoute hostname is auto-constructed from wildcard", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			bindingName := server.Name + "-gateway-binding"
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: server.Namespace},
			}
			f.WaitForBindingRegistered(ctx, t, r, binding, metav1.ConditionFalse)

			route := &gatewayv1.HTTPRoute{}
			if err := r.Get(ctx, bindingName, server.Namespace, route); err != nil {
				t.Fatalf("HTTPRoute not found: %v", err)
			}

			expectedHostname := server.Name + ".mcp.local"
			if len(route.Spec.Hostnames) != 1 || string(route.Spec.Hostnames[0]) != expectedHostname {
				t.Fatalf("expected HTTPRoute hostname %s, got %v", expectedHostname, route.Spec.Hostnames)
			}
			t.Logf("HTTPRoute hostname auto-constructed: %s", expectedHostname)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestKuadrantDefaultSectionName(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-default-section-config"

	feature := features.New("Kuadrant: default sectionName mcps").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.Kuadrant).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ensureKuadrantScheme(t, cfg.Client().Resources().GetScheme())

			ns := ctx.Value(f.NsKey).(string)
			f.EnsureReferenceGrant(ctx, t, cfg, ns, prov.ConfigData["gateway-namespace"])

			wildcard := gatewayv1.Hostname("*.mcp.local")
			f.EnsureMultiListenerGateway(ctx, t, cfg,
				"kuadrant-default-gw", prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"],
				[]f.ListenerSpec{
					{Name: "mcps", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: &wildcard},
				},
			)

			f.CreateMCPGatewayExtension(ctx, t, cfg,
				"default-ext", ns,
				"kuadrant-default-gw", prov.ConfigData["gateway-namespace"],
				f.WithPublicHost("default-sec.public.example.com"),
				f.WithSectionName("mcps"),
			)

			configData := map[string]string{
				"gateway-name":      "kuadrant-default-gw",
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"prefix":            prov.ConfigData["prefix"],
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "default-sec", true,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionTrue)
			return ctx
		}).
		Assess("GatewayRegistered=True with default mcps sectionName", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			cond := f.GetMCPServerCondition(server, "GatewayRegistered")
			if cond == nil || cond.Status != metav1.ConditionTrue {
				t.Fatal("expected GatewayRegistered=True with default sectionName mcps")
			}
			t.Log("GatewayRegistered=True with default sectionName mcps (no section-name in ConfigMap)")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestKuadrantPublicHostnamePriority(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-priority-config"

	feature := features.New("Kuadrant: ConfigMap public-hostname takes priority over extension").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.Kuadrant).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ensureKuadrantScheme(t, cfg.Client().Resources().GetScheme())

			ns := ctx.Value(f.NsKey).(string)
			f.EnsureReferenceGrant(ctx, t, cfg, ns, prov.ConfigData["gateway-namespace"])

			listenerName, _ := f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])

			f.CreateMCPGatewayExtension(ctx, t, cfg,
				"priority-ext", ns,
				prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"],
				f.WithPublicHost("extension.example.com"),
				f.WithSectionName(listenerName),
			)

			configData := map[string]string{
				"gateway-name":      prov.ConfigData["gateway-name"],
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      listenerName,
				"route-hostname":    "route.mcp.local",
				"public-hostname":   "configmap.example.com",
				"prefix":            prov.ConfigData["prefix"],
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "priority", true,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			return ctx
		}).
		Assess("status URL uses ConfigMap public-hostname, not extension", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertGatewayAddressURL(t, server, "configmap.example.com", "/mcp")
			t.Logf("ConfigMap public-hostname takes priority: %s", server.Status.Address.URL)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestKuadrantExtensionFallback(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-ext-fallback-config"

	feature := features.New("Kuadrant: extension on different listener found via port-based matching").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.Kuadrant).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ensureKuadrantScheme(t, cfg.Client().Resources().GetScheme())

			ns := ctx.Value(f.NsKey).(string)
			f.EnsureReferenceGrant(ctx, t, cfg, ns, prov.ConfigData["gateway-namespace"])

			wildcard := gatewayv1.Hostname("*.mcp.local")
			f.EnsureMultiListenerGateway(ctx, t, cfg,
				"kuadrant-ext-fallback-gw", prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"],
				[]f.ListenerSpec{
					{Name: "mcp", Port: 80, Protocol: gatewayv1.HTTPProtocolType},
					{Name: "mcps", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: &wildcard},
				},
			)

			f.CreateMCPGatewayExtension(ctx, t, cfg,
				"fallback-ext", ns,
				"kuadrant-ext-fallback-gw", prov.ConfigData["gateway-namespace"],
				f.WithPublicHost("public.example.com"),
				f.WithSectionName("mcp"),
			)

			configData := map[string]string{
				"gateway-name":      "kuadrant-ext-fallback-gw",
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      "mcps",
				"prefix":            prov.ConfigData["prefix"],
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "ext-fallback", true,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			return ctx
		}).
		Assess("status URL uses extension publicHost", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertGatewayAddressURL(t, server, "public.example.com", "/mcp")
			t.Logf("extension publicHost used via port-based fallback: %s", server.Status.Address.URL)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestKuadrantAmbiguousExtensions(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-ambiguous-ext-config"

	feature := features.New("Kuadrant: GatewayRegistered=False on ambiguous extensions").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.Kuadrant).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ensureKuadrantScheme(t, cfg.Client().Resources().GetScheme())

			ns := ctx.Value(f.NsKey).(string)
			f.EnsureReferenceGrant(ctx, t, cfg, ns, prov.ConfigData["gateway-namespace"])

			f.EnsureMultiListenerGateway(ctx, t, cfg,
				"kuadrant-ambiguous-gw", prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"],
				[]f.ListenerSpec{
					{Name: "mcp", Port: 80, Protocol: gatewayv1.HTTPProtocolType},
				},
			)

			f.CreateMCPGatewayExtension(ctx, t, cfg,
				"ambig-ext-1", ns,
				"kuadrant-ambiguous-gw", prov.ConfigData["gateway-namespace"],
				f.WithPublicHost("host-1.example.com"),
				f.WithSectionName("mcp"),
			)
			f.CreateMCPGatewayExtension(ctx, t, cfg,
				"ambig-ext-2", ns,
				"kuadrant-ambiguous-gw", prov.ConfigData["gateway-namespace"],
				f.WithPublicHost("host-2.example.com"),
				f.WithSectionName("mcp"),
			)

			configData := map[string]string{
				"gateway-name":      "kuadrant-ambiguous-gw",
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      "mcp",
				"route-hostname":    "ambig.mcp.local",
				"prefix":            prov.ConfigData["prefix"],
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "ambig-ext", false,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)
			return ctx
		}).
		Assess("GatewayRegistered=False due to ambiguous extensions", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerCondition(ctx, t, r, server, "GatewayRegistered", metav1.ConditionFalse)
			t.Log("GatewayRegistered=False as expected with ambiguous extensions")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestKuadrantCrossNamespaceExtension(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-cross-ns-config"

	feature := features.New("Kuadrant: cross-namespace extension discovery").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.Kuadrant).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ensureKuadrantScheme(t, cfg.Client().Resources().GetScheme())

			ns := ctx.Value(f.NsKey).(string)
			f.EnsureReferenceGrant(ctx, t, cfg, ns, prov.ConfigData["gateway-namespace"])

			f.EnsureMultiListenerGateway(ctx, t, cfg,
				"kuadrant-cross-ns-gw", prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"],
				[]f.ListenerSpec{
					{Name: "mcp", Port: 80, Protocol: gatewayv1.HTTPProtocolType},
				},
			)

			f.CreateMCPGatewayExtension(ctx, t, cfg,
				"cross-ns-ext", ns,
				"kuadrant-cross-ns-gw", prov.ConfigData["gateway-namespace"],
				f.WithPublicHost("cross-ns.example.com"),
				f.WithSectionName("mcp"),
			)

			configData := map[string]string{
				"gateway-name":      "kuadrant-cross-ns-gw",
				"gateway-namespace": prov.ConfigData["gateway-namespace"],
				"section-name":      "mcp",
				"route-hostname":    "cross-ns.mcp.local",
				"prefix":            prov.ConfigData["prefix"],
			}
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, configData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "cross-ns", true,
				f.WithGateway(prov.Name, configMapName),
				f.WithPath("/mcp"),
			)

			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			return ctx
		}).
		Assess("extension in test namespace is discovered", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertGatewayAddressURL(t, server, "cross-ns.example.com", "/mcp")
			t.Logf("cross-namespace extension discovered: %s", server.Status.Address.URL)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
