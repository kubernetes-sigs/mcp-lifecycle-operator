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
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

	if readyCondition.Status != metav1.ConditionTrue || readyCondition.Reason != ReasonAvailable {
		return readyCondition, nil
	}

	existingReady := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeReady)
	alreadyVerified := existingReady != nil &&
		existingReady.Status == metav1.ConditionTrue &&
		existingReady.Reason == ReasonAvailable &&
		mcpServer.Status.ObservedGeneration == mcpServer.Generation &&
		mcpServer.Status.ServerInfo != nil

	if alreadyVerified {
		return readyCondition, mcpServer.Status.ServerInfo
	}

	dialer := r.MCPDialer
	if dialer == nil {
		dialer = r.verifyMCPEndpoint
	}
	dialCtx, dialCancel := context.WithTimeout(ctx, mcpHandshakeTimeout)
	defer dialCancel()
	info, err := dialer(dialCtx, mcpURL)
	if err != nil {
		if isHTTPAuthError(err) {
			logger.Info("MCP endpoint returned auth error, treating as reachable", "url", mcpURL, "error", err)
			return readyCondition, &mcpv1alpha1.MCPServerInfo{}
		}
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

	logger.Info("MCP endpoint verified successfully", "url", mcpURL)
	return readyCondition, info
}

// verifyMCPEndpoint performs an MCP initialize handshake against the given URL
// to verify the endpoint actually speaks the MCP protocol.
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
			Logging:     res.Capabilities.Logging != nil,
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
