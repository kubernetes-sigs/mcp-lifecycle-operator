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
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scenario"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

// Opt-in and configuration for the code-mode SC-006 scenario. It is skipped by
// default because it requires the dispatcher and runner images to be built and
// an MCPToolset already reconciled to Ready on the cluster.
const (
	// codeModeE2EEnv, when set to a non-empty value, enables this scenario.
	codeModeE2EEnv = "CODEMODE_E2E"
	// codeModeDispatcherDeploymentEnv names the dispatcher Deployment to observe.
	// Applying examples/mcptoolset-ops.yaml produces "ops-toolset-execute".
	codeModeDispatcherDeploymentEnv = "CODEMODE_DISPATCHER_DEPLOYMENT"
	// codeModeDispatcherNamespaceEnv is the dispatcher Deployment's namespace
	// (defaults to "default", matching the example).
	codeModeDispatcherNamespaceEnv = "CODEMODE_DISPATCHER_NAMESPACE"
	// operatorDeploymentName is the operator's controller-manager Deployment.
	operatorDeploymentName = "mcp-lifecycle-operator-controller-manager"
)

// TestCodeModeOperatorOffHotPath documents and (opt-in) validates SC-006 / FR-009:
// the operator reconciles long-lived infrastructure only and is NOT on the
// per-execution hot path. The dispatcher claims sandboxes directly from the
// provider, so executions must keep working with the operator scaled to zero.
//
// Documented manual procedure (quickstart S6):
//
//  1. Apply examples/mcptoolset-ops.yaml and wait for the MCPToolset to report
//     Ready=True; capture status.executeEndpoint.
//  2. Run an execute_code call against the endpoint (quickstart S3) and confirm
//     it returns a single combined result.
//  3. Scale the operator to zero:
//     kubectl -n <operator-ns> scale deploy mcp-lifecycle-operator-controller-manager --replicas=0
//  4. Re-run the execute_code call from step 2.
//
// Expected: the execution still succeeds, because the dispatcher data plane runs
// independently of the reconcile loop. The in-process complement (dispatcher
// claiming from a fake SandboxProvider with no controller running) is covered by
// TestRunReturnsSingleCombinedResult in cmd/execute-code (research R8).
//
// When CODEMODE_E2E is set, this test performs the infrastructure-observable core
// of the invariant: it scales the operator to zero and asserts the dispatcher
// Deployment keeps its available replicas (the data plane survives operator-down).
func TestCodeModeOperatorOffHotPath(t *testing.T) {
	t.Parallel()
	feature := features.New("Code mode: operator off the execution hot path (SC-006)").
		WithLabel(category.Label, category.Resilience).
		WithLabel(speed.Label, speed.Slow).
		WithLabel(scenario.Label, scenario.Recovery).
		Assess("dispatcher keeps serving while the operator is scaled to zero", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			if os.Getenv(codeModeE2EEnv) == "" {
				t.Skipf("set %s=1 (and %s to a Ready toolset's dispatcher Deployment) to run the SC-006 scale-to-zero scenario; "+
					"see the documented procedure on this test and the fake-provider unit test in cmd/execute-code",
					codeModeE2EEnv, codeModeDispatcherDeploymentEnv)
			}

			dispatcherName := os.Getenv(codeModeDispatcherDeploymentEnv)
			if dispatcherName == "" {
				t.Fatalf("%s must name the dispatcher Deployment to observe (e.g. ops-toolset-execute)", codeModeDispatcherDeploymentEnv)
			}
			dispatcherNs := os.Getenv(codeModeDispatcherNamespaceEnv)
			if dispatcherNs == "" {
				dispatcherNs = "default"
			}

			opRef := f.MustDiscoverOperatorOnce(ctx, cfg, t)
			r := cfg.Client().Resources()

			// The dispatcher must be serving before we take the operator down.
			dispatcher := &appsv1.Deployment{}
			if err := r.Get(ctx, dispatcherName, dispatcherNs, dispatcher); err != nil {
				t.Fatalf("dispatcher Deployment %s/%s not found (apply examples/mcptoolset-ops.yaml first): %v",
					dispatcherNs, dispatcherName, err)
			}
			if dispatcher.Status.AvailableReplicas < 1 {
				t.Fatalf("dispatcher %s/%s has no available replicas before scale-to-zero", dispatcherNs, dispatcherName)
			}

			// Scale the operator to zero, restoring the original replica count when done.
			operator := &appsv1.Deployment{}
			if err := r.Get(ctx, operatorDeploymentName, opRef.Namespace, operator); err != nil {
				t.Fatalf("operator Deployment %s/%s not found: %v", opRef.Namespace, operatorDeploymentName, err)
			}
			original := int32(1)
			if operator.Spec.Replicas != nil {
				original = *operator.Spec.Replicas
			}
			t.Cleanup(func() {
				restore := &appsv1.Deployment{}
				if err := r.Get(context.Background(), operatorDeploymentName, opRef.Namespace, restore); err != nil {
					t.Logf("could not reload operator Deployment to restore replicas: %v", err)
					return
				}
				restore.Spec.Replicas = &original
				if err := r.Update(context.Background(), restore); err != nil {
					t.Logf("could not restore operator replicas to %d: %v", original, err)
				}
			})

			zero := int32(0)
			operator.Spec.Replicas = &zero
			if err := r.Update(ctx, operator); err != nil {
				t.Fatalf("scaling operator to zero: %v", err)
			}
			t.Logf("scaled operator %s/%s to zero replicas", opRef.Namespace, operatorDeploymentName)

			// Give the operator time to fully stop, then confirm the dispatcher data
			// plane is unaffected: it keeps its available replicas with no reconcile
			// loop running. This is the observable core of SC-006.
			time.Sleep(30 * time.Second)

			after := &appsv1.Deployment{}
			if err := r.Get(ctx, dispatcherName, dispatcherNs, after); err != nil {
				t.Fatalf("dispatcher Deployment %s/%s disappeared while operator was down: %v", dispatcherNs, dispatcherName, err)
			}
			if after.Status.AvailableReplicas < 1 {
				t.Fatalf("dispatcher %s/%s lost its available replicas while the operator was scaled to zero", dispatcherNs, dispatcherName)
			}
			t.Logf("dispatcher %s/%s still has %d available replica(s) with the operator scaled to zero (SC-006 holds)",
				dispatcherNs, dispatcherName, after.Status.AvailableReplicas)

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}
