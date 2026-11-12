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
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scenario"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

const (
	// codeModeProvider is the sandbox provider the operator ships and registers.
	codeModeProvider = "agentsandbox"

	// conditionReady / conditionServersResolved are the MCPToolset status
	// condition types published by the controller.
	conditionReady           = "Ready"
	conditionServersResolved = "ServersResolved"
)

// toolsetHasCondition reports whether the toolset carries condType at status.
func toolsetHasCondition(ts *mcpv1alpha1.MCPToolset, condType string, status metav1.ConditionStatus) bool {
	for _, c := range ts.Status.Conditions {
		if c.Type == condType && c.Status == status {
			return true
		}
	}
	return false
}

// waitForToolsetCondition blocks until the toolset reports condType at status.
func waitForToolsetCondition(ctx context.Context, t *testing.T, cfg *envconf.Config,
	ts *mcpv1alpha1.MCPToolset, condType string, status metav1.ConditionStatus, timeout time.Duration) {
	t.Helper()
	r := cfg.Client().Resources()
	err := wait.For(
		conditions.New(r).ResourceMatch(ts, func(obj k8s.Object) bool {
			return toolsetHasCondition(obj.(*mcpv1alpha1.MCPToolset), condType, status)
		}),
		wait.WithContext(ctx),
		wait.WithTimeout(timeout),
		wait.WithInterval(2*time.Second),
	)
	if err != nil {
		t.Fatalf("MCPToolset %s/%s: timed out waiting for %s=%s: %v",
			ts.Namespace, ts.Name, condType, status, err)
	}
}

// TestCodeModeToolsetMissingServerNotReady validates the controller's not-ready
// path on a live cluster: a toolset referencing a server that does not exist must
// report ServersResolved=False (naming the missing server) and Ready=False, and
// must NOT deploy a dispatcher against an unready toolset. This reaches the server
// resolver and returns before any provider setup, so it needs only the operator
// and the MCPToolset CRD; it does not require agent-sandbox or built images.
func TestCodeModeToolsetMissingServerNotReady(t *testing.T) {
	t.Parallel()
	const (
		ns            = "default"
		toolsetName   = "codemode-missing-server"
		missingServer = "no-such-mcpserver"
	)

	feature := features.New("Code mode: toolset with a missing server stays not-ready (SC-007)").
		WithLabel(category.Label, category.Lifecycle).
		WithLabel(speed.Label, speed.Fast).
		WithLabel(scenario.Label, scenario.Deploy).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ts := &mcpv1alpha1.MCPToolset{
				ObjectMeta: metav1.ObjectMeta{Name: toolsetName, Namespace: ns},
				Spec: mcpv1alpha1.MCPToolsetSpec{
					Servers:  []string{missingServer},
					Provider: codeModeProvider,
				},
			}
			if err := cfg.Client().Resources().Create(ctx, ts); err != nil {
				t.Fatalf("creating MCPToolset: %v", err)
			}
			return ctx
		}).
		Assess("reports ServersResolved=False naming the missing server and Ready=False", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()
			ts := &mcpv1alpha1.MCPToolset{ObjectMeta: metav1.ObjectMeta{Name: toolsetName, Namespace: ns}}

			waitForToolsetCondition(ctx, t, cfg, ts, conditionServersResolved, metav1.ConditionFalse, 2*time.Minute)

			if err := r.Get(ctx, toolsetName, ns, ts); err != nil {
				t.Fatalf("get MCPToolset: %v", err)
			}
			if !toolsetHasCondition(ts, conditionReady, metav1.ConditionFalse) {
				t.Fatalf("toolset conditions = %+v; want Ready=False", ts.Status.Conditions)
			}
			var resolved *metav1.Condition
			for i := range ts.Status.Conditions {
				if ts.Status.Conditions[i].Type == conditionServersResolved {
					resolved = &ts.Status.Conditions[i]
				}
			}
			if resolved == nil || !strings.Contains(resolved.Message, missingServer) {
				t.Fatalf("ServersResolved message = %q; want it to name %q", func() string {
					if resolved == nil {
						return "<absent>"
					}
					return resolved.Message
				}(), missingServer)
			}
			t.Logf("toolset not ready as expected: %s", resolved.Message)
			return ctx
		}).
		Assess("no dispatcher is deployed against an unready toolset", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()
			// The dispatcher Deployment is named "<toolset>-execute" (see the
			// example and the controller's dispatcher naming). It must be absent.
			dep := &appsv1.Deployment{}
			err := r.Get(ctx, toolsetName+"-execute", ns, dep)
			if err == nil {
				t.Fatalf("dispatcher Deployment %s-execute exists but the toolset is not ready", toolsetName)
			}
			if !apierrors.IsNotFound(err) {
				t.Fatalf("unexpected error checking for dispatcher Deployment: %v", err)
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ts := &mcpv1alpha1.MCPToolset{ObjectMeta: metav1.ObjectMeta{Name: toolsetName, Namespace: ns}}
			if err := cfg.Client().Resources().Delete(ctx, ts); err != nil && !apierrors.IsNotFound(err) {
				t.Logf("deleting MCPToolset: %v", err)
			}
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

// TestCodeModeToolsetBecomesReady validates the full reconcile output on a live
// cluster: a toolset referencing a Ready MCPServer reaches Ready=True, publishes
// status.executeEndpoint, and has its dispatcher Deployment and Service created.
// Unlike the envtest integration test, this exercises the real agentsandbox
// provider Setup and real resource creation, so it is opt-in: it requires the
// agent-sandbox controller/CRDs and the dispatcher image reachable in-cluster.
func TestCodeModeToolsetBecomesReady(t *testing.T) {
	t.Parallel()
	feature := features.New("Code mode: toolset reconciles to Ready and publishes the execute endpoint").
		WithLabel(category.Label, category.Lifecycle).
		WithLabel(speed.Label, speed.Slow).
		WithLabel(scenario.Label, scenario.Deploy).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			if os.Getenv(codeModeE2EEnv) == "" {
				t.Skipf("set %s=1 to run the toolset-reconcile scenario; it requires agent-sandbox installed "+
					"and the dispatcher image reachable in-cluster", codeModeE2EEnv)
			}
			// Reuse the MCPServer fixture so the toolset references a real, Ready
			// server in the same namespace.
			return f.SetupMCPServer(ctx, t, cfg, "codemode-backend", false)
		}).
		Assess("toolset reaches Ready and publishes the execute endpoint", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)

			ts := &mcpv1alpha1.MCPToolset{
				ObjectMeta: metav1.ObjectMeta{Name: "codemode-ready", Namespace: server.Namespace},
				Spec: mcpv1alpha1.MCPToolsetSpec{
					Servers:  []string{server.Name},
					Provider: codeModeProvider,
				},
			}
			if err := r.Create(ctx, ts); err != nil {
				t.Fatalf("creating MCPToolset: %v", err)
			}
			t.Cleanup(func() {
				_ = r.Delete(context.Background(), ts)
			})

			waitForToolsetCondition(ctx, t, cfg, ts, conditionReady, metav1.ConditionTrue, 3*time.Minute)

			if err := r.Get(ctx, ts.Name, ts.Namespace, ts); err != nil {
				t.Fatalf("get MCPToolset: %v", err)
			}
			if ts.Status.ExecuteEndpoint == "" {
				t.Fatalf("Ready=True but status.executeEndpoint is empty")
			}
			t.Logf("toolset Ready; executeEndpoint=%s", ts.Status.ExecuteEndpoint)

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, ts.Name+"-execute", ts.Namespace, dep); err != nil {
				t.Fatalf("dispatcher Deployment not created: %v", err)
			}
			svc := &corev1.Service{}
			if err := r.Get(ctx, ts.Name+"-execute", ts.Namespace, svc); err != nil {
				t.Fatalf("dispatcher Service not created: %v", err)
			}
			t.Logf("dispatcher Deployment and Service %s-execute exist", ts.Name)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
