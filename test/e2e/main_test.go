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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

var testenv env.Environment

func TestMain(m *testing.M) {
	cfg, err := envconf.NewFromFlags()
	if err != nil {
		panic(err)
	}

	testenv = env.NewWithConfig(cfg)

	// Register MCPServer types so the client can work with them.
	if err := mcpv1alpha1.AddToScheme(cfg.Client().Resources().GetScheme()); err != nil {
		panic(err)
	}

	// Create a unique namespace before each test, delete it after.
	testenv.BeforeEachTest(func(ctx context.Context, cfg *envconf.Config, t *testing.T) (context.Context, error) {
		ns := envconf.RandomName("e2e", 16)
		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		if err := cfg.Client().Resources().Create(ctx, nsObj); err != nil {
			return ctx, err
		}
		t.Logf("created namespace %s", ns)
		ctx = context.WithValue(ctx, f.NsKey, ns)
		return ctx, nil
	})

	testenv.AfterEachTest(func(ctx context.Context, cfg *envconf.Config, t *testing.T) (context.Context, error) {
		ns, ok := ctx.Value(f.NsKey).(string)
		if !ok || ns == "" {
			t.Log("namespace not found in context, skipping cleanup")
			return ctx, nil
		}
		if t.Failed() {
			dumpDiagnostics(ctx, t, cfg, ns)
		}
		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		if err := cfg.Client().Resources().Delete(ctx, nsObj); err != nil {
			t.Logf("failed to delete namespace %s: %v", ns, err)
		}
		return ctx, nil
	})

	os.Exit(testenv.Run(m))
}

// dumpDiagnostics logs cluster state for debugging when a test fails.
func dumpDiagnostics(ctx context.Context, t *testing.T, cfg *envconf.Config, ns string) {
	t.Log("=== DIAGNOSTICS DUMP (test failed) ===")
	r := cfg.Client().Resources(ns)

	var servers mcpv1alpha1.MCPServerList
	if err := r.List(ctx, &servers); err == nil {
		for _, s := range servers.Items {
			t.Logf("MCPServer %s/%s generation=%d observedGeneration=%d",
				s.Namespace, s.Name, s.Generation, s.Status.ObservedGeneration)
			if s.Status.Address != nil {
				t.Logf("  address: %s", s.Status.Address.URL)
			}
			for _, c := range s.Status.Conditions {
				t.Logf("  condition %s=%s reason=%s message=%q",
					c.Type, c.Status, c.Reason, c.Message)
			}
		}
	}

	var deployments appsv1.DeploymentList
	if err := r.List(ctx, &deployments); err == nil {
		for _, d := range deployments.Items {
			desired := int32(1)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}
			t.Logf("Deployment %s replicas=%d/%d available=%d",
				d.Name, d.Status.ReadyReplicas, desired, d.Status.AvailableReplicas)
		}
	}

	var pods corev1.PodList
	if err := r.List(ctx, &pods); err == nil {
		for _, p := range pods.Items {
			t.Logf("Pod %s phase=%s", p.Name, p.Status.Phase)
			for _, cs := range p.Status.ContainerStatuses {
				t.Logf("  container %s ready=%v restarts=%d", cs.Name, cs.Ready, cs.RestartCount)
				if cs.State.Waiting != nil {
					t.Logf("    waiting: %s - %s", cs.State.Waiting.Reason, cs.State.Waiting.Message)
				}
			}
		}
	}

	var events corev1.EventList
	if err := r.List(ctx, &events); err == nil {
		for _, e := range events.Items {
			t.Logf("Event %s %s/%s: %s - %s",
				e.Type, e.InvolvedObject.Kind, e.InvolvedObject.Name,
				e.Reason, e.Message)
		}
	}

	// Also dump controller-manager pod logs from the operator namespace,
	// since the controller is relevant to all test failures.
	t.Log("--- controller-manager logs ---")
	controllerNs := "mcp-lifecycle-operator-system"
	rCtrl := cfg.Client().Resources(controllerNs)
	var ctrlPods corev1.PodList
	if err := rCtrl.List(ctx, &ctrlPods); err == nil {
		for _, p := range ctrlPods.Items {
			if p.Status.Phase == corev1.PodRunning {
				logs := f.PodLogs(ctx, t, cfg, p.Name, controllerNs)
				// Truncate to last 50 lines to keep output manageable.
				lines := strings.Split(logs, "\n")
				if len(lines) > 50 {
					lines = lines[len(lines)-50:]
				}
				t.Logf("Pod %s (last %d lines):\n%s", p.Name, len(lines), strings.Join(lines, "\n"))
			}
		}
	}

	t.Log("=== END DIAGNOSTICS ===")
}
