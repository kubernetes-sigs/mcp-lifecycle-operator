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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

func TestAlphaMCPServerNotServed(t *testing.T) {
	t.Parallel()
	feature := features.New("alpha MCPServer serving is disabled").
		WithLabel(category.Label, category.Configuration).
		WithLabel(speed.Label, speed.Moderate).
		Assess("alpha MCPServer is unavailable while MCPGatewayBinding remains served", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client, err := discovery.NewDiscoveryClientForConfig(cfg.Client().RESTConfig())
			if err != nil {
				t.Fatal(err)
			}
			resources, err := client.ServerResourcesForGroupVersion("mcp.x-k8s.io/v1alpha1")
			if err != nil {
				t.Fatal(err)
			}
			bindingFound := false
			for _, resource := range resources.APIResources {
				if resource.Name == "mcpservers" || resource.Name == "mcpservers/status" {
					t.Errorf("alpha MCPServer still appears in discovery: %s", resource.Name)
				}
				if resource.Name == "mcpgatewaybindings" {
					bindingFound = true
				}
			}
			if !bindingFound {
				t.Error("alpha MCPGatewayBinding missing from discovery")
			}
			dynamicClient, err := dynamic.NewForConfig(cfg.Client().RESTConfig())
			if err != nil {
				t.Fatal(err)
			}
			namespace := ctx.Value(f.NsKey).(string)
			alpha := dynamicClient.Resource(schema.GroupVersionResource{Group: "mcp.x-k8s.io", Version: "v1alpha1", Resource: "mcpservers"}).Namespace(namespace)
			if _, err := alpha.List(ctx, metav1.ListOptions{}); !apierrors.IsNotFound(err) {
				t.Errorf("alpha LIST: want NotFound, got %v", err)
			}
			fixture := v1AdmissionFixture("v1alpha1", namespace, "alpha-unserved", false)
			if _, err := alpha.Create(ctx, fixture, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}); !apierrors.IsNotFound(err) {
				t.Errorf("alpha CREATE: want NotFound, got %v", err)
			}
			return ctx
		}).Feature()
	testenv.Test(t, feature)
}

// TestConversionGatewayFieldPreserved retains coverage of gateway field persistence
// through the beta API and v1 storage without requiring the retired alpha endpoint.
func TestConversionGatewayFieldPreserved(t *testing.T) {
	t.Parallel()
	const (
		name      = "convert-beta-gateway"
		provider  = "e2e-conversion-probe"
		configRef = "gw-config"
	)

	feature := features.New("spec.gateway survives beta writes through v1 storage").
		WithLabel(category.Label, category.Configuration).
		WithLabel(speed.Label, speed.Moderate).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context; ensure BeforeEachTest has run")
			}
			r := cfg.Client().Resources()

			// Native v1beta1 object carrying spec.gateway.
			beta := f.NewMCPServer(name, ns, f.WithGateway(provider, configRef))
			if err := r.Create(ctx, beta); err != nil {
				t.Fatalf("failed to create v1beta1 MCPServer with spec.gateway: %v", err)
			}
			t.Logf("created v1beta1 MCPServer %s/%s with spec.gateway.provider=%s", ns, name, provider)
			return ctx
		}).
		Assess("spec.gateway is persisted and served at v1beta1", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			beta := &mcpv1beta1.MCPServer{}
			if err := r.Get(ctx, name, ns, beta); err != nil {
				t.Fatalf("failed to get MCPServer at v1beta1: %v", err)
			}
			if beta.Spec.Gateway == nil {
				t.Fatal("spec.gateway dropped: the gateway field was not persisted by the CRD")
			}
			if got := beta.Spec.Gateway.Provider; got != provider {
				t.Errorf("spec.gateway.provider not preserved: got %q, want %q", got, provider)
			}
			if got := beta.Spec.Gateway.ConfigRef; got != configRef {
				t.Errorf("spec.gateway.configRef not preserved: got %q, want %q", got, configRef)
			}
			t.Logf("spec.gateway preserved at v1beta1 (provider=%s configRef=%s)",
				beta.Spec.Gateway.Provider, beta.Spec.Gateway.ConfigRef)
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}
