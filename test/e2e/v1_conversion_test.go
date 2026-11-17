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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

// TestV1MCPServerConversion exercises both served versions through the API
// server: creating at v1beta1 converts to v1 storage, while reading a native v1
// object at v1beta1 converts back through the deployed webhook.
func TestV1MCPServerConversion(t *testing.T) {
	t.Parallel()
	const (
		v1Name   = "convert-v1"
		betaName = "convert-beta"
		port     = int32(8080)
		path     = "/mcp"
	)

	feature := features.New("MCPServer v1 and v1beta1 are served through the conversion webhook").
		WithLabel(category.Label, category.Configuration).
		WithLabel(speed.Label, speed.Moderate).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()
			stable := &mcpv1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{Name: v1Name, Namespace: ns},
				Spec: mcpv1.MCPServerSpec{
					Source: mcpv1.Source{Type: mcpv1.SourceTypeContainerImage,
						ContainerImage: &mcpv1.ContainerImageSource{Ref: f.DefaultMCPServerImage}},
					Config: mcpv1.ServerConfig{Port: port, Path: path},
				},
			}
			if err := r.Create(ctx, stable); err != nil {
				t.Fatalf("create v1 MCPServer: %v", err)
			}
			beta := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{Name: betaName, Namespace: ns},
				Spec: mcpv1beta1.MCPServerSpec{
					Source: mcpv1beta1.Source{Type: mcpv1beta1.SourceTypeContainerImage,
						ContainerImage: &mcpv1beta1.ContainerImageSource{Ref: f.DefaultMCPServerImage}},
					Config: mcpv1beta1.ServerConfig{Port: port, Path: path},
				},
			}
			if err := r.Create(ctx, beta); err != nil {
				t.Fatalf("create v1beta1 MCPServer: %v", err)
			}
			return ctx
		}).
		Assess("both objects retain their spec through the other served version", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()
			betaView := &mcpv1beta1.MCPServer{}
			if err := r.Get(ctx, v1Name, ns, betaView); err != nil {
				t.Fatalf("read v1 object at v1beta1: %v", err)
			}
			if betaView.Spec.Source.ContainerImage == nil {
				t.Fatal("v1 image source missing on v1beta1 read")
			}
			if betaView.Spec.Source.ContainerImage.Ref != f.DefaultMCPServerImage ||
				betaView.Spec.Config.Port != port || betaView.Spec.Config.Path != path {
				t.Errorf("v1 object changed when read at v1beta1: image=%q port=%d path=%q",
					betaView.Spec.Source.ContainerImage.Ref, betaView.Spec.Config.Port, betaView.Spec.Config.Path)
			}
			v1View := &mcpv1.MCPServer{}
			if err := r.Get(ctx, betaName, ns, v1View); err != nil {
				t.Fatalf("read v1beta1 object at v1: %v", err)
			}
			if v1View.Spec.Source.ContainerImage == nil {
				t.Fatal("v1beta1 image source missing on v1 read")
			}
			if v1View.Spec.Source.ContainerImage.Ref != f.DefaultMCPServerImage ||
				v1View.Spec.Config.Port != port || v1View.Spec.Config.Path != path {
				t.Errorf("v1beta1 object changed when read at v1: image=%q port=%d path=%q",
					v1View.Spec.Source.ContainerImage.Ref, v1View.Spec.Config.Port, v1View.Spec.Config.Path)
			}
			return ctx
		}).
		Assess("both objects reconcile and report status at v1", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()
			for _, name := range []string{v1Name, betaName} {
				beta := &mcpv1beta1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
				f.WaitForMCPServerCondition(ctx, t, r, beta, "Available", metav1.ConditionTrue, 3*time.Minute)
				f.WaitForMCPServerCondition(ctx, t, r, beta, "Verified", metav1.ConditionTrue, 3*time.Minute)
				if err := r.Get(ctx, name, ns, beta); err != nil {
					t.Fatalf("read reconciled %s at v1beta1: %v", name, err)
				}
				f.AssertAddressURL(t, beta, port)
				stable := &mcpv1.MCPServer{}
				if err := r.Get(ctx, name, ns, stable); err != nil {
					t.Fatalf("read reconciled %s at v1: %v", name, err)
				}
				if cond := meta.FindStatusCondition(stable.Status.Conditions, "Verified"); cond == nil || cond.Status != metav1.ConditionTrue {
					t.Errorf("%s: v1 Verified condition is not True: %v", name, cond)
				}
				if stable.Status.Address == nil || stable.Status.Address.URL != beta.Status.Address.URL {
					t.Errorf("%s: v1 address does not match v1beta1 address", name)
				}
			}
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}
