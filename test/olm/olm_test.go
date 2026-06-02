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

package olm

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

func TestOLMInstallOwnNamespace(t *testing.T) {
	operatorNs := envconf.RandomName("olm-own", 16)
	otherNs := envconf.RandomName("olm-other", 16)

	feature := features.New("OLM OwnNamespace install mode").
		WithLabel("type", "olm").
		WithLabel("install-mode", "OwnNamespace").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {

			createNamespace(ctx, t, cfg, operatorNs)
			createNamespace(ctx, t, cfg, otherNs)
			ctx = context.WithValue(ctx, f.NsKey, operatorNs)
			installBundle(t, operatorNs, "OwnNamespace")
			return ctx
		}).
		Assess("MCPServer in operator namespace becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "test-own-ns", true)
		}).
		Assess("MCPServer in other namespace is not reconciled", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()
			server := f.NewMCPServer("test-other-ns", otherNs)
			if err := r.Create(ctx, server); err != nil {
				t.Fatalf("failed to create MCPServer in other namespace: %v", err)
			}
			t.Log("created MCPServer in other namespace, waiting to confirm it is not reconciled...")
			assertNotReconciled(ctx, t, r, server, 30*time.Second)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			cleanupOperator(t, operatorNs)
			deleteNamespace(ctx, t, cfg, operatorNs)
			deleteNamespace(ctx, t, cfg, otherNs)
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestOLMInstallSingleNamespace(t *testing.T) {
	operatorNs := envconf.RandomName("olm-op", 16)
	watchNs := envconf.RandomName("olm-watch", 16)

	feature := features.New("OLM SingleNamespace install mode").
		WithLabel("type", "olm").
		WithLabel("install-mode", "SingleNamespace").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {

			createNamespace(ctx, t, cfg, operatorNs)
			createNamespace(ctx, t, cfg, watchNs)
			ctx = context.WithValue(ctx, f.NsKey, watchNs)
			installBundle(t, operatorNs, "SingleNamespace="+watchNs)
			return ctx
		}).
		Assess("MCPServer in watched namespace becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "test-watched", true)
		}).
		Assess("MCPServer in operator namespace is not reconciled", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()
			server := f.NewMCPServer("test-operator-ns", operatorNs)
			if err := r.Create(ctx, server); err != nil {
				t.Fatalf("failed to create MCPServer in operator namespace: %v", err)
			}
			t.Log("created MCPServer in operator namespace, waiting to confirm it is not reconciled...")
			assertNotReconciled(ctx, t, r, server, 30*time.Second)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			cleanupOperator(t, operatorNs)
			deleteNamespace(ctx, t, cfg, operatorNs)
			deleteNamespace(ctx, t, cfg, watchNs)
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestOLMInstallMultiNamespace(t *testing.T) {
	operatorNs := envconf.RandomName("olm-op", 16)
	watchNs1 := envconf.RandomName("olm-w1", 16)
	watchNs2 := envconf.RandomName("olm-w2", 16)
	excludedNs := envconf.RandomName("olm-excl", 16)

	feature := features.New("OLM MultiNamespace install mode").
		WithLabel("type", "olm").
		WithLabel("install-mode", "MultiNamespace").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {

			createNamespace(ctx, t, cfg, operatorNs)
			createNamespace(ctx, t, cfg, watchNs1)
			createNamespace(ctx, t, cfg, watchNs2)
			createNamespace(ctx, t, cfg, excludedNs)
			installBundle(t, operatorNs, "MultiNamespace="+watchNs1+","+watchNs2)
			return ctx
		}).
		Assess("MCPServer in first watched namespace becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ctx = context.WithValue(ctx, f.NsKey, watchNs1)
			return f.SetupMCPServer(ctx, t, cfg, "test-w1", true)
		}).
		Assess("MCPServer in second watched namespace becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()
			server := f.NewMCPServer("test-w2", watchNs2)
			if err := r.Create(ctx, server); err != nil {
				t.Fatalf("failed to create MCPServer: %v", err)
			}
			f.WaitForMCPServerCondition(ctx, t, r, server, "Ready", metav1.ConditionTrue)
			t.Log("MCPServer in second watched namespace is Ready")
			return ctx
		}).
		Assess("MCPServer in excluded namespace is not reconciled", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()
			server := f.NewMCPServer("test-excluded", excludedNs)
			if err := r.Create(ctx, server); err != nil {
				t.Fatalf("failed to create MCPServer in excluded namespace: %v", err)
			}
			t.Log("created MCPServer in excluded namespace, waiting to confirm it is not reconciled...")
			assertNotReconciled(ctx, t, r, server, 30*time.Second)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			cleanupOperator(t, operatorNs)
			deleteNamespace(ctx, t, cfg, operatorNs)
			deleteNamespace(ctx, t, cfg, watchNs1)
			deleteNamespace(ctx, t, cfg, watchNs2)
			deleteNamespace(ctx, t, cfg, excludedNs)
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestOLMInstallAllNamespaces(t *testing.T) {
	operatorNs := envconf.RandomName("olm-op", 16)
	otherNs := envconf.RandomName("olm-any", 16)

	feature := features.New("OLM AllNamespaces install mode").
		WithLabel("type", "olm").
		WithLabel("install-mode", "AllNamespaces").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {

			createNamespace(ctx, t, cfg, operatorNs)
			createNamespace(ctx, t, cfg, otherNs)
			ctx = context.WithValue(ctx, f.NsKey, otherNs)
			installBundle(t, operatorNs, "AllNamespaces")
			return ctx
		}).
		Assess("MCPServer in any namespace becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "test-all-ns", true)
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			cleanupOperator(t, operatorNs)
			deleteNamespace(ctx, t, cfg, operatorNs)
			deleteNamespace(ctx, t, cfg, otherNs)
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}
