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

package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTestMCPServerForAudit(name string) *mcpv1alpha1.MCPServer {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       "test-ns",
			Generation:      3,
			ResourceVersion: "12345",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Config: mcpv1alpha1.ServerConfig{
				Port: 8080,
			},
		},
	}
}

type logEntry struct {
	output string
}

func captureAuditLogs(fn func(ctx context.Context)) []logEntry {
	var entries []logEntry
	logger := funcr.New(func(prefix, args string) {
		entries = append(entries, logEntry{output: prefix + " " + args})
	}, funcr.Options{})

	ctx := log.IntoContext(context.Background(), logger)
	fn(ctx)
	return entries
}

func findAuditEntry(entries []logEntry, operation string) (logEntry, bool) {
	for _, e := range entries {
		if strings.Contains(e.output, "audit") && strings.Contains(e.output, fmt.Sprintf(`"operation"=%q`, operation)) {
			return e, true
		}
	}
	return logEntry{}, false
}

func TestAuditHandshakeSuccess(t *testing.T) {
	mcpServer := newTestMCPServerForAudit("test-server")
	info := &mcpv1alpha1.MCPServerInfo{
		Name:            "echo-server",
		Version:         "1.2.0",
		ProtocolVersion: "2026-07-28",
	}
	entries := captureAuditLogs(func(ctx context.Context) {
		auditHandshakeSuccess(ctx, mcpServer, "http://test.svc:8080/mcp", info, 142*time.Millisecond)
	})

	entry, found := findAuditEntry(entries, "HandshakeSuccess")
	if !found {
		t.Fatalf("expected HandshakeSuccess audit entry, got %v", entries)
	}
	for _, expected := range []string{"audit", "mcpserver", "test-server", "test-ns", "2026-07-28", "echo-server"} {
		if !strings.Contains(entry.output, expected) {
			t.Errorf("expected audit entry to contain %q, got: %s", expected, entry.output)
		}
	}
}

func TestAuditHandshakeFailed(t *testing.T) {
	mcpServer := newTestMCPServerForAudit("fail-server")
	entries := captureAuditLogs(func(ctx context.Context) {
		auditHandshakeFailed(ctx, mcpServer, "http://fail.svc:8080/mcp", fmt.Errorf("connection refused"), 50*time.Millisecond)
	})

	entry, found := findAuditEntry(entries, "HandshakeFailed")
	if !found {
		t.Fatalf("expected HandshakeFailed audit entry, got %v", entries)
	}
	if !strings.Contains(entry.output, "connection refused") {
		t.Errorf("expected error in audit entry, got: %s", entry.output)
	}
}

func TestAuditNetworkPolicyCreated(t *testing.T) {
	mcpServer := newTestMCPServerForAudit("np-server")
	entries := captureAuditLogs(func(ctx context.Context) {
		auditNetworkPolicyCreated(ctx, mcpServer, "np-server", true)
	})

	entry, found := findAuditEntry(entries, "NetworkPolicyCreated")
	if !found {
		t.Fatalf("expected NetworkPolicyCreated audit entry, got %v", entries)
	}
	if !strings.Contains(entry.output, "hasIngressRestriction") {
		t.Errorf("expected hasIngressRestriction field, got: %s", entry.output)
	}
}

func TestAuditConfigurationRejected(t *testing.T) {
	mcpServer := newTestMCPServerForAudit("invalid-server")
	entries := captureAuditLogs(func(ctx context.Context) {
		auditConfigurationRejected(ctx, mcpServer, "Invalid", "port must be > 0")
	})

	entry, found := findAuditEntry(entries, "ConfigurationRejected")
	if !found {
		t.Fatalf("expected ConfigurationRejected audit entry, got %v", entries)
	}
	for _, expected := range []string{"Invalid", "port must be > 0"} {
		if !strings.Contains(entry.output, expected) {
			t.Errorf("expected %q in audit entry, got: %s", expected, entry.output)
		}
	}
}

func TestAuditOwnershipViolation(t *testing.T) {
	mcpServer := newTestMCPServerForAudit("owned-server")
	entries := captureAuditLogs(func(ctx context.Context) {
		auditOwnershipViolation(ctx, mcpServer, "my-deployment", "Deployment", "other-controller")
	})

	entry, found := findAuditEntry(entries, "OwnershipViolation")
	if !found {
		t.Fatalf("expected OwnershipViolation audit entry, got %v", entries)
	}
	for _, expected := range []string{"my-deployment", "Deployment", "other-controller"} {
		if !strings.Contains(entry.output, expected) {
			t.Errorf("expected %q in audit entry, got: %s", expected, entry.output)
		}
	}
}

func TestAuditCommonFields(t *testing.T) {
	mcpServer := newTestMCPServerForAudit("common-fields-server")
	entries := captureAuditLogs(func(ctx context.Context) {
		auditHandshakeAttempt(ctx, mcpServer, "http://test.svc:8080/mcp")
	})

	entry, found := findAuditEntry(entries, "HandshakeAttempt")
	if !found {
		t.Fatalf("expected HandshakeAttempt audit entry, got %v", entries)
	}
	for _, expected := range []string{
		"common-fields-server",
		"test-ns",
		`"generation"=3`,
		`"resourceVersion"="12345"`,
	} {
		if !strings.Contains(entry.output, expected) {
			t.Errorf("expected common field %q in audit entry, got: %s", expected, entry.output)
		}
	}
}

func TestAuditLoggerName(t *testing.T) {
	mcpServer := newTestMCPServerForAudit("logger-name-server")
	entries := captureAuditLogs(func(ctx context.Context) {
		auditCapabilityChange(ctx, mcpServer, "tools: true->false")
	})

	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}
	if !strings.Contains(entries[0].output, "audit") {
		t.Errorf("expected logger name containing 'audit', got: %s", entries[0].output)
	}
}
