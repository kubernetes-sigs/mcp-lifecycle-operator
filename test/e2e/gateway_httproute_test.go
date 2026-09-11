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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scope"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

func TestHTTPRouteProviderResources(t *testing.T) {
	prov := f.ActiveProvider(t)
	const configMapName = "gw-httproute-config"

	feature := features.New("HTTPRoute provider: resources").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scope.Label, scope.HTTPRoute).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			prov.ConfigData["section-name"] = f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
			f.CreateGatewayConfigMap(ctx, t, cfg, configMapName, ns, prov.ConfigData)
			ctx = f.SetupMCPServer(ctx, t, cfg, "httproute-resources", false,
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
			gwName := prov.ConfigData["gateway-name"]
			gwNamespace := prov.ConfigData["gateway-namespace"]
			if string(route.Spec.ParentRefs[0].Name) != gwName {
				t.Fatalf("expected parentRef name %s, got %s", gwName, route.Spec.ParentRefs[0].Name)
			}
			if route.Spec.ParentRefs[0].Namespace == nil || string(*route.Spec.ParentRefs[0].Namespace) != gwNamespace {
				t.Fatalf("expected parentRef namespace %s", gwNamespace)
			}

			if len(route.Spec.Rules) != 1 || len(route.Spec.Rules[0].BackendRefs) != 1 {
				t.Fatal("expected 1 rule with 1 backendRef")
			}
			if string(route.Spec.Rules[0].BackendRefs[0].Name) != server.Name {
				t.Fatalf("expected backendRef name %s, got %s", server.Name, route.Spec.Rules[0].BackendRefs[0].Name)
			}

			hostname := prov.ConfigData["hostname"]
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
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
