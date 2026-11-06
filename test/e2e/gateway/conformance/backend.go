//go:build e2e

/*
Copyright 2026 The Kubernetes Authors.

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

package conformance

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"

	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

// A successful initialize can terminate at a gateway's broker without reaching
// the backend. Calling a backend tool proves that the per-server route works.
func assertBackendPodListed(ctx context.Context, t *testing.T, gatewayAddress, hostname, path, ns string, r *resources.Resources, selector map[string]string) {
	t.Helper()
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gateway-backend-conformance", Version: "v0.0.1"}, nil)
	var lastError error
	err := wait.For(func(ctx context.Context) (bool, error) {
		// Broker discovery may finish after initialize. Open a fresh session on
		// each attempt rather than keeping a session with an earlier tool set.
		attemptCtx, attemptCancel := context.WithTimeout(ctx, 10*time.Second)
		defer attemptCancel()
		session, err := mcpClient.Connect(attemptCtx, &mcp.StreamableClientTransport{
			Endpoint:   fmt.Sprintf("http://%s%s", gatewayAddress, path),
			HTTPClient: f.WithHostOverride(&http.Client{}, hostname),
		}, nil)
		if err != nil {
			lastError = err
			return false, nil
		}
		defer func() { _ = session.Close() }()
		tools, err := session.ListTools(attemptCtx, &mcp.ListToolsParams{})
		if err != nil {
			lastError = err
			return false, nil
		}
		lastError = fmt.Errorf("backend pod-listing tool has not been discovered")
		for _, tool := range tools.Tools {
			// Providers may prefix backend tool names or expose them directly.
			if !strings.HasSuffix(tool.Name, "pods_list_in_namespace") {
				continue
			}
			result, err := session.CallTool(attemptCtx, &mcp.CallToolParams{
				Name: tool.Name, Arguments: map[string]any{"namespace": ns},
			})
			if err != nil {
				lastError = err
				continue
			}
			var text strings.Builder
			for _, content := range result.Content {
				if content, ok := content.(*mcp.TextContent); ok {
					text.WriteString(content.Text)
				}
			}
			if result.IsError {
				lastError = fmt.Errorf("tool %s failed: %s", tool.Name, text.String())
				continue
			}
			podName, err := listedBackendPod(ctx, r.GetControllerRuntimeClient(), ns, selector, text.String())
			if err != nil {
				lastError = fmt.Errorf("tool %s: %w", tool.Name, err)
				continue
			}
			t.Logf("backend tool %s returned pod %s through the gateway", tool.Name, podName)
			return true, nil
		}
		return false, nil
	}, wait.WithContext(callCtx), wait.WithTimeout(0), wait.WithInterval(2*time.Second)) // callCtx owns the overall deadline.
	if err != nil {
		t.Fatalf("backend tool call through gateway did not return a ready backend pod in %s: %v; last response: %v", ns, err, lastError)
	}
}

func createBackendPodReader(ctx context.Context, t *testing.T, r *resources.Resources, ns string) {
	t.Helper()
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "gateway-backend-pod-reader", Namespace: ns},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"list"}}},
	}
	if err := r.Create(ctx, role); err != nil {
		t.Fatalf("failed to create backend pod-reader Role: %v", err)
	}
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: role.Name, Namespace: ns},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default", Namespace: ns}},
	}
	if err := r.Create(ctx, binding); err != nil {
		t.Fatalf("failed to create backend pod-reader RoleBinding: %v", err)
	}
}
