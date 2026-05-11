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

package framework

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/klient/k8s/resources"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// AssertAddressURL verifies the MCPServer's status address URL matches the expected port and path.
func AssertAddressURL(t *testing.T, server *mcpv1alpha1.MCPServer, port int32) {
	t.Helper()
	path := server.Spec.Config.Path
	if path == "" {
		path = "/mcp"
	}
	expectedURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", server.Name, server.Namespace, port, path)
	if server.Status.Address == nil || server.Status.Address.URL != expectedURL {
		actual := ""
		if server.Status.Address != nil {
			actual = server.Status.Address.URL
		}
		t.Fatalf("expected address URL %q, got %q", expectedURL, actual)
	}
}

// AssertConditionStable re-fetches the MCPServer repeatedly for the given duration
// and fails if the condition ever deviates from the expected status.
func AssertConditionStable(ctx context.Context, t *testing.T, r *resources.Resources,
	server *mcpv1alpha1.MCPServer, condType string, status metav1.ConditionStatus, duration ...time.Duration) {
	t.Helper()
	d := 10 * time.Second
	if len(duration) > 0 {
		d = duration[0]
	}
	deadline := time.Now().Add(d)
	polls := 0
	for time.Now().Before(deadline) {
		if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
			t.Fatalf("failed to get MCPServer during stability check: %v", err)
		}
		cond := GetMCPServerCondition(server, condType)
		if cond == nil || cond.Status != status {
			actual := "<nil>"
			if cond != nil {
				actual = string(cond.Status)
			}
			t.Fatalf("condition %s flipped from %s to %s after %d polls",
				condType, status, actual, polls)
		}
		polls++
		time.Sleep(2 * time.Second)
	}
	t.Logf("condition %s=%s stable for %s (%d polls)", condType, status, d, polls)
}

// GetMCPServerCondition returns a pointer to the named condition, or nil.
func GetMCPServerCondition(server *mcpv1alpha1.MCPServer, condType string) *metav1.Condition {
	for i := range server.Status.Conditions {
		if server.Status.Conditions[i].Type == condType {
			return &server.Status.Conditions[i]
		}
	}
	return nil
}
