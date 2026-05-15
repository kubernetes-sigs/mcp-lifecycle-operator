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
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// ContextKey is used to store values in context.
type ContextKey string

const (
	NsKey     ContextKey = "namespace"
	ServerKey ContextKey = "mcpserver"
)

// ServerFromContext extracts the MCPServer stored by SetupMCPServer.
// Returns nil if the server was never stored in context.
func ServerFromContext(ctx context.Context) *mcpv1alpha1.MCPServer {
	s, _ := ctx.Value(ServerKey).(*mcpv1alpha1.MCPServer)
	return s
}

// SetupMCPServer creates an MCPServer, optionally waits for Ready, and stores it in context.
// Pass waitForReady=true to block until the server reaches Ready=True before returning.
func SetupMCPServer(ctx context.Context, t *testing.T, cfg *envconf.Config, name string, waitForReady bool, opts ...MCPServerOption) context.Context {
	t.Helper()
	ns, ok := ctx.Value(NsKey).(string)
	if !ok || ns == "" {
		t.Fatal("namespace not found in context; ensure BeforeEachTest has run")
	}
	r := cfg.Client().Resources()

	server := NewMCPServer(name, ns, opts...)
	if err := r.Create(ctx, server); err != nil {
		t.Fatalf("failed to create MCPServer: %v", err)
	}
	t.Logf("created MCPServer %s/%s", ns, server.Name)

	if waitForReady {
		WaitForMCPServerCondition(ctx, t, r, server, "Ready", metav1.ConditionTrue)
		t.Log("MCPServer is Ready")
	}

	return context.WithValue(ctx, ServerKey, server)
}

// WaitForMCPServerCondition polls until the named condition reaches the desired status.
// An optional timeout can be provided; defaults to 3 minutes.
func WaitForMCPServerCondition(ctx context.Context, t *testing.T, r *resources.Resources,
	server *mcpv1alpha1.MCPServer, condType string, status metav1.ConditionStatus, timeout ...time.Duration) {
	t.Helper()
	d := 3 * time.Minute
	if len(timeout) > 0 {
		d = timeout[0]
	}
	err := wait.For(
		conditions.New(r).ResourceMatch(server, func(obj k8s.Object) bool {
			s := obj.(*mcpv1alpha1.MCPServer)
			for _, c := range s.Status.Conditions {
				if c.Type == condType && c.Status == status {
					return true
				}
			}
			return false
		}),
		wait.WithTimeout(d),
		wait.WithInterval(2*time.Second),
	)
	if err != nil {
		t.Fatalf("MCPServer %s/%s: timed out waiting for %s=%s: %v",
			server.Namespace, server.Name, condType, status, err)
	}
}

// WaitForMCPServerReconciledAndReady polls until the controller has reconciled the
// current generation (observedGeneration >= generation) and Ready=True.
// Use this after mutating the MCPServer spec to avoid seeing stale Ready from before the update.
func WaitForMCPServerReconciledAndReady(ctx context.Context, t *testing.T, r *resources.Resources,
	server *mcpv1alpha1.MCPServer, timeout ...time.Duration) {
	t.Helper()
	d := 3 * time.Minute
	if len(timeout) > 0 {
		d = timeout[0]
	}
	err := wait.For(
		conditions.New(r).ResourceMatch(server, func(obj k8s.Object) bool {
			s := obj.(*mcpv1alpha1.MCPServer)
			if s.Status.ObservedGeneration < s.Generation {
				return false
			}
			for _, c := range s.Status.Conditions {
				if c.Type == "Ready" && c.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}),
		wait.WithTimeout(d),
		wait.WithInterval(2*time.Second),
	)
	if err != nil {
		t.Fatalf("MCPServer %s/%s: timed out waiting for reconciled Ready: %v",
			server.Namespace, server.Name, err)
	}
}

// WaitForMCPServerConditionReason polls until the named condition reaches the desired status and reason.
// An optional timeout can be provided; defaults to 3 minutes.
func WaitForMCPServerConditionReason(ctx context.Context, t *testing.T, r *resources.Resources,
	server *mcpv1alpha1.MCPServer, condType string, status metav1.ConditionStatus, reason string, timeout ...time.Duration) {
	t.Helper()
	d := 3 * time.Minute
	if len(timeout) > 0 {
		d = timeout[0]
	}
	err := wait.For(
		conditions.New(r).ResourceMatch(server, func(obj k8s.Object) bool {
			s := obj.(*mcpv1alpha1.MCPServer)
			for _, c := range s.Status.Conditions {
				if c.Type == condType && c.Status == status && c.Reason == reason {
					return true
				}
			}
			return false
		}),
		wait.WithTimeout(d),
		wait.WithInterval(2*time.Second),
	)
	if err != nil {
		t.Fatalf("MCPServer %s/%s: timed out waiting for %s=%s reason=%s: %v",
			server.Namespace, server.Name, condType, status, reason, err)
	}
}

// WaitForMCPServerConditionMessageContains polls until the named condition reaches the desired
// status and reason, and its message contains the given substring.
// An optional timeout can be provided; defaults to 3 minutes.
func WaitForMCPServerConditionMessageContains(ctx context.Context, t *testing.T, r *resources.Resources,
	server *mcpv1alpha1.MCPServer, condType string, status metav1.ConditionStatus, reason string,
	messageSubstring string, timeout ...time.Duration) {
	t.Helper()
	d := 3 * time.Minute
	if len(timeout) > 0 {
		d = timeout[0]
	}
	err := wait.For(
		conditions.New(r).ResourceMatch(server, func(obj k8s.Object) bool {
			s := obj.(*mcpv1alpha1.MCPServer)
			for _, c := range s.Status.Conditions {
				if c.Type == condType && c.Status == status && c.Reason == reason &&
					strings.Contains(c.Message, messageSubstring) {
					return true
				}
			}
			return false
		}),
		wait.WithTimeout(d),
		wait.WithInterval(2*time.Second),
	)
	if err != nil {
		t.Fatalf("MCPServer %s/%s: timed out waiting for %s=%s reason=%s message containing %q: %v",
			server.Namespace, server.Name, condType, status, reason, messageSubstring, err)
	}
}

// TeardownMCPServer deletes the MCPServer from context and waits for owned
// Deployment and Service to be garbage collected.
func TeardownMCPServer(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	t.Helper()
	server := ServerFromContext(ctx)
	if server == nil {
		t.Log("no MCPServer found in context, skipping teardown")
		return ctx
	}
	r := cfg.Client().Resources()

	if err := r.Delete(ctx, server); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("failed to delete MCPServer: %v", err)
	}
	t.Logf("deleted MCPServer %s/%s", server.Namespace, server.Name)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
	}
	if err := wait.For(
		conditions.New(r).ResourceDeleted(dep),
		wait.WithTimeout(1*time.Minute),
		wait.WithInterval(2*time.Second),
	); err != nil {
		t.Fatalf("Deployment was not garbage collected: %v", err)
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
	}
	if err := wait.For(
		conditions.New(r).ResourceDeleted(svc),
		wait.WithTimeout(1*time.Minute),
		wait.WithInterval(2*time.Second),
	); err != nil {
		t.Fatalf("Service was not garbage collected: %v", err)
	}

	t.Log("Deployment and Service were garbage collected")
	return ctx
}
