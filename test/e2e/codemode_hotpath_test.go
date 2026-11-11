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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scenario"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

const (
	// dispatcherServicePort is the port the dispatcher Service exposes MCP on
	// (mirrors the controller's dispatcherPort; the two packages cannot share a
	// constant across the e2e build tag boundary).
	dispatcherServicePort = 8080
	// dispatcherMCPPath is the HTTP path the dispatcher serves MCP on.
	dispatcherMCPPath = "/mcp"
	// sandboxClaimGroup/Version/ListKind identify agent-sandbox SandboxClaims,
	// listed unstructured so the e2e client needs no agent-sandbox scheme.
	sandboxClaimGroup    = "extensions.agents.x-k8s.io"
	sandboxClaimVersion  = "v1beta1"
	sandboxClaimListKind = "SandboxClaimList"
)

// Opt-in and configuration for the code-mode SC-006 scenario. It is skipped by
// default because it requires:
//   - the agent-sandbox controller and its CRDs installed on the cluster
//     (https://github.com/kubernetes-sigs/agent-sandbox); the agentsandbox
//     provider claims agent-sandbox Sandboxes at execution time,
//   - the dispatcher and mcplo-agentsandbox images built and reachable
//     (the latter wired as the operator's --runner-image), and
//   - an MCPToolset already reconciled to Ready on the cluster.
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

			// Wait for the operator to actually terminate (no available replicas),
			// so the execution below genuinely runs with the reconcile loop stopped
			// rather than merely scaled on paper.
			waitForOperatorDown(ctx, t, r, operatorDeploymentName, opRef.Namespace)

			// The dispatcher data plane must be unaffected: it keeps its available
			// replicas with no reconcile loop running (the observable core of SC-006).
			after := &appsv1.Deployment{}
			if err := r.Get(ctx, dispatcherName, dispatcherNs, after); err != nil {
				t.Fatalf("dispatcher Deployment %s/%s disappeared while operator was down: %v", dispatcherNs, dispatcherName, err)
			}
			if after.Status.AvailableReplicas < 1 {
				t.Fatalf("dispatcher %s/%s lost its available replicas while the operator was scaled to zero", dispatcherNs, dispatcherName)
			}
			t.Logf("dispatcher %s/%s still has %d available replica(s) with the operator scaled to zero",
				dispatcherNs, dispatcherName, after.Status.AvailableReplicas)

			// Baseline the live SandboxClaims so the post-execution cleanup check is
			// robust even if other toolsets share the namespace.
			claimsBefore := countSandboxClaims(ctx, t, cfg, dispatcherNs)

			// Connect to the dispatcher Service over the API-server proxy (the same
			// node-agnostic transport the MCP handshake test uses) and drive a real
			// execute_code round-trip while the operator is down.
			httpClient, dispatcherURL := f.ServiceProxyHTTPClient(t, cfg,
				dispatcherNs, dispatcherName, dispatcherServicePort, dispatcherMCPPath)
			mcpClient := mcp.NewClient(&mcp.Implementation{Name: "e2e-codemode-client", Version: "v0.0.1"}, nil)
			connectCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
			defer cancel()
			session, err := mcpClient.Connect(connectCtx,
				&mcp.StreamableClientTransport{Endpoint: dispatcherURL, HTTPClient: httpClient}, nil)
			if err != nil {
				t.Fatalf("connecting to dispatcher %s/%s over the API-server proxy: %v", dispatcherNs, dispatcherName, err)
			}
			defer func() { _ = session.Close() }()

			// Discovery dials the allow-listed backends for their tools/list, so a
			// successful, non-empty catalog proves the dispatcher reaches a backend
			// with no operator involved.
			lt, err := session.CallTool(connectCtx, &mcp.CallToolParams{Name: "list_toolsets"})
			if err != nil {
				t.Fatalf("list_toolsets while operator down: %v", err)
			}
			if lt.IsError {
				t.Fatalf("list_toolsets reported an error: %s", mcpResultText(lt))
			}
			t.Logf("list_toolsets succeeded with the operator down: %s", mcpResultText(lt))

			// Execute a script in a freshly claimed sandbox and verify the single
			// returned result. This exercises the full claim -> ship code -> await ->
			// release path in the data plane with the reconcile loop stopped.
			exec, err := session.CallTool(connectCtx, &mcp.CallToolParams{
				Name:      "execute_code",
				Arguments: map[string]any{"code": "return 6 * 7;", "toolset": dispatcherToolsetName(dispatcherName)},
			})
			if err != nil {
				t.Fatalf("execute_code while operator down: %v", err)
			}
			if exec.IsError {
				t.Fatalf("execute_code transport error: %s", mcpResultText(exec))
			}
			out, ok := exec.StructuredContent.(map[string]any)
			if !ok {
				t.Fatalf("execute_code structured content = %T; want object (%s)", exec.StructuredContent, mcpResultText(exec))
			}
			if errObj, present := out["error"]; present && errObj != nil {
				t.Fatalf("execute_code returned an error with the operator down: %v", errObj)
			}
			if got := out["result"]; got != float64(42) {
				t.Fatalf("execute_code result = %v (%T); want 42", got, got)
			}
			t.Logf("execute_code returned 42 with the operator scaled to zero (data plane is self-sufficient)")

			// The sandbox must be cleaned up after the execution: the dispatcher
			// Releases the claim regardless of outcome, so the live claim count must
			// return to its baseline. Allow brief eventual-deletion slack.
			deadline := time.Now().Add(2 * time.Minute)
			var claimsAfter int
			for {
				claimsAfter = countSandboxClaims(ctx, t, cfg, dispatcherNs)
				if claimsAfter <= claimsBefore || time.Now().After(deadline) {
					break
				}
				time.Sleep(5 * time.Second)
			}
			if claimsAfter > claimsBefore {
				t.Fatalf("SandboxClaims not cleaned up: before=%d after=%d (the execution's claim/sandbox leaked)", claimsBefore, claimsAfter)
			}
			t.Logf("SandboxClaims returned to baseline (before=%d, after=%d): the execution's sandbox was cleaned up", claimsBefore, claimsAfter)

			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

// mcpResultText concatenates the text content of an MCP tool result, for logging
// and error messages.
func mcpResultText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b []byte
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b = append(b, tc.Text...)
		}
	}
	return string(b)
}

// dispatcherToolsetName recovers the toolset name from its dispatcher resource
// name. reconcileDispatcher names every dispatcher resource "<toolset>-execute".
func dispatcherToolsetName(dispatcherName string) string {
	const suffix = "-execute"
	if len(dispatcherName) > len(suffix) && dispatcherName[len(dispatcherName)-len(suffix):] == suffix {
		return dispatcherName[:len(dispatcherName)-len(suffix)]
	}
	return dispatcherName
}

// waitForOperatorDown blocks until the operator Deployment reports no available
// replicas, so a subsequent execution genuinely runs with the reconcile loop
// stopped. It fails the test if the operator does not terminate in time.
func waitForOperatorDown(ctx context.Context, t *testing.T, r *resources.Resources, name, namespace string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		dep := &appsv1.Deployment{}
		if err := r.Get(ctx, name, namespace, dep); err != nil {
			t.Fatalf("getting operator Deployment %s/%s while waiting for it to stop: %v", namespace, name, err)
		}
		if dep.Status.AvailableReplicas == 0 {
			t.Logf("operator %s/%s reports 0 available replicas", namespace, name)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("operator %s/%s still has %d available replica(s) after the scale-to-zero timeout",
				namespace, name, dep.Status.AvailableReplicas)
		}
		time.Sleep(5 * time.Second)
	}
}

// countSandboxClaims lists agent-sandbox SandboxClaims in a namespace via an
// unstructured list so the e2e client needs no agent-sandbox scheme registration.
func countSandboxClaims(ctx context.Context, t *testing.T, cfg *envconf.Config, namespace string) int {
	t.Helper()
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   sandboxClaimGroup,
		Version: sandboxClaimVersion,
		Kind:    sandboxClaimListKind,
	})
	if err := cfg.Client().Resources(namespace).List(ctx, list); err != nil {
		t.Fatalf("listing SandboxClaims in %s: %v", namespace, err)
	}
	n := 0
	for _, item := range list.Items {
		if item.GetNamespace() == namespace {
			n++
		}
	}
	return n
}
