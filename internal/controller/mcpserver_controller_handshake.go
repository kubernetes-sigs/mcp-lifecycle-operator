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
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// reconcileHandshake performs the MCP handshake when the deployment is available,
// skipping it when the endpoint was already verified for the current generation.
func (r *MCPServerReconciler) reconcileHandshake(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	mcpURL string,
	readyCondition metav1.Condition,
) (metav1.Condition, *mcpv1alpha1.MCPServerInfo) {
	logger := log.FromContext(ctx)

	metricLabels := prometheus.Labels{
		"name":      mcpServer.Name,
		"namespace": mcpServer.Namespace,
	}

	existingReady := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeReady)
	alreadyVerified := existingReady != nil &&
		existingReady.Status == metav1.ConditionTrue &&
		existingReady.Reason == ReasonAvailable &&
		mcpServer.Status.ObservedGeneration == mcpServer.Generation &&
		mcpServer.Status.ServerInfo != nil

	// If the handshake was already verified for this generation, preserve
	// Ready=True even if the Deployment has a transient status fluctuation
	// (e.g. during rollout cleanup).
	if alreadyVerified {
		handshakeTotal.With(withResult(metricLabels, "skip")).Inc()
		return *existingReady, mcpServer.Status.ServerInfo
	}

	if readyCondition.Status != metav1.ConditionTrue || readyCondition.Reason != ReasonAvailable {
		return readyCondition, nil
	}

	dialer := r.MCPDialer
	if dialer == nil {
		dialer = r.verifyMCPEndpoint
	}
	dialCtx, dialCancel := context.WithTimeout(ctx, mcpHandshakeTimeout)
	defer dialCancel()
	start := time.Now()
	info, err := dialer(dialCtx, mcpURL)
	handshakeDuration.With(metricLabels).Observe(time.Since(start).Seconds())
	if err != nil {
		if isHTTPAuthError(err) {
			handshakeTotal.With(withResult(metricLabels, "auth_skip")).Inc()
			logger.Info("MCP endpoint returned auth error, treating as reachable", "url", mcpURL, "error", err)
			return readyCondition, &mcpv1alpha1.MCPServerInfo{}
		}
		handshakeTotal.With(withResult(metricLabels, "failure")).Inc()
		logger.Info("MCP endpoint handshake failed", "url", mcpURL, "error", err)
		cond := newCondition(
			ConditionTypeReady,
			metav1.ConditionFalse,
			ReasonMCPEndpointUnavailable,
			fmt.Sprintf("MCP endpoint is not serving a valid MCP protocol: %v", err),
			mcpServer.Generation,
		)
		if existingReady == nil || existingReady.Status != metav1.ConditionTrue {
			preserveLastTransitionTime(&cond, mcpServer.Status.Conditions)
		}
		return cond, nil
	}

	handshakeTotal.With(withResult(metricLabels, "success")).Inc()
	protocolVersion := ""
	if info != nil {
		protocolVersion = info.ProtocolVersion
	}
	logger.Info("MCP endpoint verified successfully", "url", mcpURL, "protocolVersion", protocolVersion)
	return readyCondition, info
}

func withResult(labels prometheus.Labels, result string) prometheus.Labels {
	m := make(prometheus.Labels, len(labels)+1)
	maps.Copy(m, labels)
	m["result"] = result
	return m
}

// verifyMCPEndpoint performs an MCP protocol handshake (initialize or
// server/discover) against the given URL to verify the endpoint actually
// speaks the MCP protocol.
// On success it returns the server's self-reported identity and capabilities
// extracted from the InitializeResult.
// It uses a dedicated context for the connection so that cancelling it tears
// down the transport without sending an HTTP DELETE to the server (which some
// MCP servers do not handle gracefully).
func (r *MCPServerReconciler) verifyMCPEndpoint(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
	connCtx, connCancel := context.WithCancel(ctx)

	mcpClient := mcp.NewClient(
		&mcp.Implementation{
			Name:    mcpClientName,
			Version: MCPClientVersion,
		},
		nil,
	)

	transport := &mcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           &http.Client{Timeout: 10 * time.Second},
		DisableStandaloneSSE: true,
		MaxRetries:           -1, // disable retries; the controller handles requeue
	}

	session, err := mcpClient.Connect(connCtx, transport, nil)
	if err != nil {
		connCancel()
		return nil, err
	}
	// Cancel the connection context before closing the session. session.Close
	// blocks until the reader goroutine exits; cancelling the context causes
	// the reader to return immediately instead of waiting on the HTTP stream.
	defer func() {
		connCancel()
		_ = session.Close()
	}()

	return extractServerInfo(session.InitializeResult()), nil
}

// extractServerInfo converts an MCP InitializeResult into our CRD type.
func extractServerInfo(res *mcp.InitializeResult) *mcpv1alpha1.MCPServerInfo {
	if res == nil {
		return nil
	}
	info := &mcpv1alpha1.MCPServerInfo{
		ProtocolVersion: res.ProtocolVersion,
		Instructions:    res.Instructions,
	}
	if res.ServerInfo != nil {
		info.Name = res.ServerInfo.Name
		info.Version = res.ServerInfo.Version
	}
	if res.Capabilities != nil {
		info.Capabilities = &mcpv1alpha1.MCPServerCapabilities{
			Tools:       res.Capabilities.Tools != nil,
			Resources:   res.Capabilities.Resources != nil,
			Prompts:     res.Capabilities.Prompts != nil,
			Logging:     res.Capabilities.Logging != nil, //nolint:staticcheck // TODO: remove after SEP-2577 deprecation window (mid-2027)
			Completions: res.Capabilities.Completions != nil,
		}
	}
	return info
}

// mcpHandshakeBackoff computes an exponential backoff delay for MCP handshake
// retries: 10s, 20s, 40s, 80s, capped at maxRequeueDelayMCPHandshake.
func mcpHandshakeBackoff(retryCount int) time.Duration {
	delay := requeueDelayMCPHandshake
	for range retryCount {
		delay *= 2
		if delay > maxRequeueDelayMCPHandshake {
			return maxRequeueDelayMCPHandshake
		}
	}
	return delay
}

// capabilityDiffMessage compares two MCPServerCapabilities and returns a
// human-readable message describing the differences. Nil is treated as
// all-false. Returns an empty string when nothing changed.
func capabilityDiffMessage(old, new *mcpv1alpha1.MCPServerCapabilities) string {
	var oldCaps, newCaps mcpv1alpha1.MCPServerCapabilities
	if old != nil {
		oldCaps = *old
	}
	if new != nil {
		newCaps = *new
	}

	var diffs []string
	if oldCaps.Tools != newCaps.Tools {
		diffs = append(diffs, fmt.Sprintf("tools: %v->%v", oldCaps.Tools, newCaps.Tools))
	}
	if oldCaps.Resources != newCaps.Resources {
		diffs = append(diffs, fmt.Sprintf("resources: %v->%v", oldCaps.Resources, newCaps.Resources))
	}
	if oldCaps.Prompts != newCaps.Prompts {
		diffs = append(diffs, fmt.Sprintf("prompts: %v->%v", oldCaps.Prompts, newCaps.Prompts))
	}
	if oldCaps.Logging != newCaps.Logging { //nolint:staticcheck // TODO: remove after SEP-2577 deprecation window (mid-2027)
		diffs = append(diffs, fmt.Sprintf("logging: %v->%v", oldCaps.Logging, newCaps.Logging)) //nolint:staticcheck // TODO: remove after SEP-2577 deprecation window (mid-2027)
	}
	if oldCaps.Completions != newCaps.Completions {
		diffs = append(diffs, fmt.Sprintf("completions: %v->%v", oldCaps.Completions, newCaps.Completions))
	}
	return strings.Join(diffs, ", ")
}

// isHTTPAuthError checks whether the error from the MCP SDK indicates an HTTP
// 401 Unauthorized or 403 Forbidden response. The SDK does not wrap these with
// a sentinel error type; it returns a plain error whose message ends with the
// status text from net/http (e.g. "POST http://...: Unauthorized").
func isHTTPAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.HasSuffix(msg, ": "+http.StatusText(http.StatusUnauthorized)) ||
		strings.HasSuffix(msg, ": "+http.StatusText(http.StatusForbidden))
}

// reconcileHandshakeEventsAndRetryCount emits handshake-related events and returns the updated retry count.
func (r *MCPServerReconciler) reconcileHandshakeEventsAndRetryCount(
	mcpServer *mcpv1alpha1.MCPServer,
	readyCondition metav1.Condition,
) int32 {
	if readyCondition.Reason != ReasonMCPEndpointUnavailable {
		return 0
	}

	prevHandshakeRetryCount := mcpServer.Status.HandshakeRetryCount
	if mcpServer.Status.ObservedGeneration != mcpServer.Generation {
		prevHandshakeRetryCount = 0
	}

	var handshakeRetryCount int32
	if mcpServer.Status.ObservedGeneration == mcpServer.Generation {
		handshakeRetryCount = mcpServer.Status.HandshakeRetryCount + 1
	} else {
		handshakeRetryCount = 1
	}

	if !duplicateHandshakeUnavailable(mcpServer.Status.Conditions, readyCondition.Message) {
		r.emitMCPHandshakeFailed(mcpServer, readyCondition.Message)
	}
	if int(handshakeRetryCount) >= maxMCPHandshakeRetries && int(prevHandshakeRetryCount) < maxMCPHandshakeRetries {
		r.emitMCPHandshakeRetriesExhausted(mcpServer, handshakeRetryCount)
	}

	return handshakeRetryCount
}
