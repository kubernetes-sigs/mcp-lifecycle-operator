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
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func auditLog(ctx context.Context) logr.Logger {
	return log.FromContext(ctx).WithName("audit")
}

func auditFields(mcpServer *mcpv1beta1.MCPServer) []any {
	return []any{
		"mcpserver", mcpServer.Name,
		keyNamespace, mcpServer.Namespace,
		"generation", mcpServer.Generation,
		"resourceVersion", mcpServer.ResourceVersion,
	}
}

func auditHandshakeAttempt(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, url string) {
	auditLog(ctx).Info("audit",
		append(auditFields(mcpServer),
			"operation", "HandshakeAttempt",
			"url", url,
		)...,
	)
}

func auditHandshakeSuccess(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, url string, info *mcpv1beta1.MCPServerInfo, duration time.Duration) {
	fields := append(auditFields(mcpServer),
		"operation", "HandshakeSuccess",
		"url", url,
		"durationMs", duration.Milliseconds(),
	)
	if info != nil {
		fields = append(fields,
			"protocolVersion", info.ProtocolVersion,
			"serverName", info.Name,
			"serverVersion", info.Version,
		)
	}
	auditLog(ctx).Info("audit", fields...)
}

func auditHandshakeFailed(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, url string, err error, duration time.Duration) {
	auditLog(ctx).Info("audit",
		append(auditFields(mcpServer),
			"operation", "HandshakeFailed",
			"url", url,
			"error", err.Error(),
			"durationMs", duration.Milliseconds(),
		)...,
	)
}

func auditHandshakeAuthSkip(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, url string, err error) {
	auditLog(ctx).Info("audit",
		append(auditFields(mcpServer),
			"operation", "HandshakeAuthSkip",
			"url", url,
			"error", err.Error(),
		)...,
	)
}

func auditHandshakeRetriesExhausted(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, retries int, max int) {
	auditLog(ctx).Info("audit",
		append(auditFields(mcpServer),
			"operation", "HandshakeRetriesExhausted",
			"retries", retries,
			"max", max,
		)...,
	)
}

func auditCapabilityChange(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, diff string) {
	auditLog(ctx).Info("audit",
		append(auditFields(mcpServer),
			"operation", "CapabilityChange",
			"diff", diff,
		)...,
	)
}

func auditNetworkPolicyCreated(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, netpolName string, hasIngressRestriction bool, hasEgressRestriction bool) {
	auditLog(ctx).Info("audit",
		append(auditFields(mcpServer),
			"operation", "NetworkPolicyCreated",
			"networkPolicy", netpolName,
			"hasIngressRestriction", hasIngressRestriction,
			"hasEgressRestriction", hasEgressRestriction,
		)...,
	)
}

func auditNetworkPolicyUpdated(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, netpolName string) {
	auditLog(ctx).Info("audit",
		append(auditFields(mcpServer),
			"operation", "NetworkPolicyUpdated",
			"networkPolicy", netpolName,
		)...,
	)
}

func auditConfigurationRejected(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, reason string, message string) {
	auditLog(ctx).Info("audit",
		append(auditFields(mcpServer),
			"operation", "ConfigurationRejected",
			keyReason, reason,
			"message", message,
		)...,
	)
}

func auditOwnershipViolation(ctx context.Context, mcpServer *mcpv1beta1.MCPServer, resource string, resourceKind string, existingOwner string) {
	auditLog(ctx).Info("audit",
		append(auditFields(mcpServer),
			"operation", "OwnershipViolation",
			"resource", resource,
			"resourceKind", resourceKind,
			"existingOwner", existingOwner,
		)...,
	)
}
