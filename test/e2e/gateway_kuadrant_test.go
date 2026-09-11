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
			prov.ConfigData["section-name"] = f.EnsureGateway(ctx, t, cfg, prov.ConfigData["gateway-name"], prov.ConfigData["gateway-namespace"], prov.ConfigData["gateway-class"])
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
