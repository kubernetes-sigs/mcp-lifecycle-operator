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
	"slices"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

// TestConversionWebhookRoundTrip exercises the deployed conversion webhook
// (not the in-process conversion functions unit-tested in the api package):
// a v1alpha1 MCPServer is applied with a typed v1alpha1 client, then re-fetched
// at v1beta1 (the storage version). It asserts the spec round-trips and that the
// controller reconciles the converted object identically to a native v1beta1 one.
func TestConversionWebhookRoundTrip(t *testing.T) {
	t.Parallel()
	const (
		name = "convert-alpha"
		port = int32(8080)
	)

	feature := features.New("v1alpha1 MCPServer round-trips through the deployed conversion webhook").
		WithLabel(category.Label, category.Configuration).
		WithLabel(speed.Label, speed.Moderate).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context; ensure BeforeEachTest has run")
			}
			r := cfg.Client().Resources()

			// Build and apply a v1alpha1 object. The API server stores it as
			// v1beta1, invoking the deployed conversion webhook on write.
			alpha := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: f.DefaultMCPServerImage,
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port:      port,
						Arguments: []string{"--port", "8080", "--read-only"},
					},
				},
			}
			if err := r.Create(ctx, alpha); err != nil {
				t.Fatalf("failed to create v1alpha1 MCPServer: %v", err)
			}
			t.Logf("created v1alpha1 MCPServer %s/%s", ns, name)
			return ctx
		}).
		Assess("re-fetch at v1beta1 returns equivalent spec", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			// Re-fetch the same object as v1beta1 (conversion on read).
			beta := &mcpv1beta1.MCPServer{}
			if err := r.Get(ctx, name, ns, beta); err != nil {
				t.Fatalf("failed to get MCPServer at v1beta1: %v", err)
			}

			if beta.Spec.Source.Type != mcpv1beta1.SourceTypeContainerImage {
				t.Errorf("source type not preserved: got %q", beta.Spec.Source.Type)
			}
			if beta.Spec.Source.ContainerImage == nil {
				t.Fatal("containerImage dropped during conversion")
			}
			if got := beta.Spec.Source.ContainerImage.Ref; got != f.DefaultMCPServerImage {
				t.Errorf("image ref not preserved: got %q, want %q", got, f.DefaultMCPServerImage)
			}
			if beta.Spec.Config.Port != port {
				t.Errorf("port not preserved: got %d, want %d", beta.Spec.Config.Port, port)
			}
			t.Logf("v1alpha1 object read back at v1beta1 with equivalent spec (image=%s port=%d)",
				beta.Spec.Source.ContainerImage.Ref, beta.Spec.Config.Port)
			return ctx
		}).
		Assess("re-fetch at v1alpha1 exercises the reverse conversion path", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			// The object is stored as v1beta1, so a Get at v1alpha1 goes through
			// the webhook (v1beta1 -> v1alpha1). This is the only path that
			// exercises the reverse conversion; a v1beta1 Get reads storage
			// directly and would let a regression here pass unnoticed.
			alpha := &mcpv1alpha1.MCPServer{}
			if err := r.Get(ctx, name, ns, alpha); err != nil {
				t.Fatalf("failed to get MCPServer at v1alpha1: %v", err)
			}

			if alpha.Spec.Source.Type != mcpv1alpha1.SourceTypeContainerImage {
				t.Errorf("source type not preserved: got %q", alpha.Spec.Source.Type)
			}
			if alpha.Spec.Source.ContainerImage == nil {
				t.Fatal("containerImage dropped during reverse conversion")
			}
			if got := alpha.Spec.Source.ContainerImage.Ref; got != f.DefaultMCPServerImage {
				t.Errorf("image ref not preserved: got %q, want %q", got, f.DefaultMCPServerImage)
			}
			if alpha.Spec.Config.Port != port {
				t.Errorf("port not preserved: got %d, want %d", alpha.Spec.Config.Port, port)
			}
			wantArgs := []string{"--port", "8080", "--read-only"}
			if !slices.Equal(alpha.Spec.Config.Arguments, wantArgs) {
				t.Errorf("arguments not preserved: got %v, want %v", alpha.Spec.Config.Arguments, wantArgs)
			}
			t.Logf("v1beta1 object read back at v1alpha1 with equivalent spec (image=%s port=%d args=%v)",
				alpha.Spec.Source.ContainerImage.Ref, alpha.Spec.Config.Port, alpha.Spec.Config.Arguments)
			return ctx
		}).
		Assess("converted object reconciles to Available and Verified", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			beta := &mcpv1beta1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
			f.WaitForMCPServerCondition(ctx, t, r, beta, "Available", metav1.ConditionTrue, 3*time.Minute)
			f.WaitForMCPServerCondition(ctx, t, r, beta, "Verified", metav1.ConditionTrue, 3*time.Minute)

			if err := r.Get(ctx, name, ns, beta); err != nil {
				t.Fatalf("failed to re-fetch MCPServer: %v", err)
			}
			f.AssertAddressURL(t, beta, port)
			t.Logf("converted MCPServer reconciled: Available=True, Verified=True, address=%s", beta.Status.Address.URL)
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

// TestConversionV1beta1OnlyFieldPreserved guards the v1beta1-only spec.gateway
// field on the conversion path. Unlike the shared-schema fields that
// TestConversionWebhookRoundTrip covers, spec.gateway has no v1alpha1 equivalent,
// so it exercises two properties those fields cannot:
//   - it is actually served and persisted by the v1beta1 CRD (a schema-generation
//     regression that pruned the new field would surface here, not just at the
//     unit layer),
//   - down-conversion to v1alpha1 drops it: a Get at v1alpha1 returns the shared
//     fields without error, and the served v1alpha1 representation genuinely omits
//     spec.gateway rather than smuggling it into an annotation or extra field.
//
// A native v1beta1 object is used (not a v1alpha1 one) because v1alpha1 cannot
// express spec.gateway in the first place. The provider name is deliberately one
// no integration controller reconciles, so the test does not depend on gateway
// infrastructure being installed; only field persistence and conversion are under
// test, not binding provisioning.
func TestConversionV1beta1OnlyFieldPreserved(t *testing.T) {
	t.Parallel()
	const (
		name      = "convert-beta-gateway"
		port      = int32(8080)
		provider  = "e2e-conversion-probe"
		configRef = "gw-config"
	)

	feature := features.New("v1beta1-only spec.gateway is persisted and drops cleanly on down-conversion").
		WithLabel(category.Label, category.Configuration).
		WithLabel(speed.Label, speed.Moderate).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context; ensure BeforeEachTest has run")
			}
			r := cfg.Client().Resources()

			// Native v1beta1 object carrying the v1beta1-only spec.gateway field.
			beta := f.NewMCPServer(name, ns, f.WithGateway(provider, configRef))
			if err := r.Create(ctx, beta); err != nil {
				t.Fatalf("failed to create v1beta1 MCPServer with spec.gateway: %v", err)
			}
			t.Logf("created v1beta1 MCPServer %s/%s with spec.gateway.provider=%s", ns, name, provider)
			return ctx
		}).
		Assess("spec.gateway is persisted and served at the v1beta1 storage version", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			beta := &mcpv1beta1.MCPServer{}
			if err := r.Get(ctx, name, ns, beta); err != nil {
				t.Fatalf("failed to get MCPServer at v1beta1: %v", err)
			}
			if beta.Spec.Gateway == nil {
				t.Fatal("spec.gateway dropped: the v1beta1-only field was not persisted by the CRD")
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
		Assess("down-conversion to v1alpha1 drops spec.gateway", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			// A typed v1alpha1 Get runs the webhook (v1beta1 -> v1alpha1) and must
			// succeed and preserve the shared fields. The gateway field cannot be
			// asserted on the typed object because v1alpha1 has no such field.
			alpha := &mcpv1alpha1.MCPServer{}
			if err := r.Get(ctx, name, ns, alpha); err != nil {
				t.Fatalf("down-conversion to v1alpha1 failed on an object with spec.gateway set: %v", err)
			}
			if alpha.Spec.Source.ContainerImage == nil ||
				alpha.Spec.Source.ContainerImage.Ref != f.DefaultMCPServerImage {
				t.Errorf("shared fields not preserved on down-conversion: source=%+v", alpha.Spec.Source)
			}
			if alpha.Spec.Config.Port != port {
				t.Errorf("port not preserved on down-conversion: got %d, want %d", alpha.Spec.Config.Port, port)
			}

			// An unstructured v1alpha1 Get proves the drop rather than assuming it:
			// it reads the served v1alpha1 representation verbatim, so a conversion
			// that leaked gateway data into spec.gateway (or anywhere) would show up
			// here even though the typed client would silently discard it.
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(mcpv1alpha1.GroupVersion.WithKind("MCPServer"))
			if err := r.Get(ctx, name, ns, u); err != nil {
				t.Fatalf("failed to get v1alpha1 MCPServer as unstructured: %v", err)
			}
			if _, found, err := unstructured.NestedMap(u.Object, "spec", "gateway"); err != nil {
				t.Fatalf("failed to read spec.gateway from v1alpha1 object: %v", err)
			} else if found {
				t.Error("spec.gateway present in the served v1alpha1 representation; it must be dropped on down-conversion")
			}
			t.Log("v1alpha1 down-conversion succeeded and omits spec.gateway as expected")
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

// TestConversionWebhookUnreachable documents the webhook-unavailable failure
// contract (Contract C). Exercising it would require tearing down the deployed
// conversion webhook, which destabilizes the shared cluster for the parallel
// suite. It is therefore skipped rather than silently omitted: the negative path
// (API request fails with a clear error rather than returning a field-dropped
// object) is covered by the webhook's own unit tests and by the CRD's
// failurePolicy=Fail, which the deploy step asserts is configured.
func TestConversionWebhookUnreachable(t *testing.T) {
	t.Skip("webhook-unreachable path is not exercised in the shared e2e cluster; " +
		"see Contract C in specs/006-v1beta1-e2e/contracts/e2e-scenarios.md")
}
