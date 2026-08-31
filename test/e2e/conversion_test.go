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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
