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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	acv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1/applyconfiguration/api/v1beta1"
)

// workloadStatusConfiguration builds identity and replica status for either
// workload mode. Both successful and failed reconciliations use this base.
func workloadStatusConfiguration(server *mcpv1beta1.MCPServer, replicas, readyReplicas int32) *acv1beta1.MCPServerStatusApplyConfiguration {
	status := acv1beta1.MCPServerStatus().
		WithObservedGeneration(server.Generation).
		WithServiceName(serviceNameFor(server)).
		WithReplicas(replicas).
		WithReadyReplicas(readyReplicas)
	if ref := server.Spec.WorkloadRef; ref != nil {
		name := fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
		return status.WithWorkloadName(name).WithWorkloadSummary("BYO:" + name)
	}
	status.WithDeploymentName(server.Name)
	if server.Spec.Source.ContainerImage != nil {
		status.WithWorkloadSummary(server.Spec.Source.ContainerImage.Ref)
	}
	return status
}

// reconcileEndpoint completes networking, verification, gateway registration and
// status publication after the managed or referenced workload has been observed.
func (r *MCPServerReconciler) reconcileEndpoint(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
	acceptedCondition, availableCondition metav1.Condition,
	status *acv1beta1.MCPServerStatusApplyConfiguration,
	endpoint ServiceEndpoint,
	networkPolicyCondition *metav1.Condition,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	// Reconcile MCPGatewayBinding
	gatewayBindingStart := time.Now()
	if err := r.reconcileGatewayBinding(ctx, mcpServer); err != nil {
		reconcileDuration.With(prometheus.Labels{keyPhase: ReconcilePhaseGatewayBinding}).Observe(time.Since(gatewayBindingStart).Seconds())
		return r.handleResourceFailure(ctx, mcpServer, status, acceptedCondition, err, resourceFailureParams{
			counter:              gatewayBindingFailuresTotal,
			reason:               ReasonGatewayNotRegistered,
			resource:             "MCPGatewayBinding",
			isDuplicate:          duplicateGatewayBindingUnavailable,
			emitEvent:            r.emitGatewayBindingReconcileFailed,
			networkPolicyPosture: networkPolicyCondition,
		})
	}
	reconcileDuration.With(prometheus.Labels{keyPhase: ReconcilePhaseGatewayBinding}).Observe(time.Since(gatewayBindingStart).Seconds())

	// Build status
	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = DefaultMCPPath
	}

	mcpURL := fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d%s",
		urlScheme(mcpServer), endpoint.Name, mcpServer.Namespace, endpoint.Port, path)

	// Compute current TLS CA bundle hash so the handshake is re-verified
	// when the CA bundle Secret content changes (which does not bump generation).
	var tlsCABundleHash string
	if mcpServer.Spec.Transport != nil && mcpServer.Spec.Transport.TLS != nil {
		tlsCABundleHash = computeTLSCABundleHash(ctx, r.APIReader, mcpServer.Namespace, mcpServer.Spec.Transport.TLS)
	}

	// If the workload is available, verify the MCP endpoint.
	verifiedCondition, serverInfo := r.reconcileHandshake(ctx, mcpServer, mcpURL, availableCondition, tlsCABundleHash)

	// Record Verified condition metric
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		verifiedCondition.Type, string(verifiedCondition.Status), verifiedCondition.Reason)

	handshakeRetryCount := r.reconcileHandshakeEventsAndRetryCount(mcpServer, &verifiedCondition)

	// Resolve gateway status: condition + binding status + address override.
	// Must run before the ready-event check so availableCondition reflects
	// any GatewayNotRegistered override.
	gwStatus := r.reconcileGatewayCondition(ctx, mcpServer)
	var gwErr error
	if gwStatus != nil {
		availableCondition, mcpURL = r.applyGatewayStatus(mcpServer, gwStatus, availableCondition, mcpURL)
		gwErr = gwStatus.err
	}

	// Normal Event once per transition to fully ready (Available + Verified).
	if !serverIsFullyReady(mcpServer.Status.Conditions) &&
		availableCondition.Status == metav1.ConditionTrue &&
		verifiedCondition.Status == metav1.ConditionTrue {
		r.emitServerReady(mcpServer)
	}

	applyGatewayStatusToAC(status, gwStatus, acceptedCondition, availableCondition, verifiedCondition)

	// Surface the NetworkPolicy posture (computed right after the NetworkPolicy was
	// reconciled above) as an informational, administrator-visible condition,
	// appended to the same apply so it does not prune the other conditions.
	if networkPolicyCondition != nil {
		status.WithConditions(conditionToAC(*networkPolicyCondition))
	}

	// Surface whether the validating admission webhook is enforcing the configured
	// admission policies. Informational and administrator-visible; never gates
	// readiness. Appended to the same apply so it survives server-side apply.
	status.WithConditions(conditionToAC(
		r.policyEnforcedCondition(mcpServer.Generation, mcpServer.Status.Conditions)))

	status = withAddressWhenVerified(status, verifiedCondition, mcpURL)

	capDiff := capabilityChangeMessage(mcpServer, serverInfo)

	if serverInfo != nil {
		status = status.WithServerInfo(serverInfoToAC(serverInfo))
	}

	if err := r.applyStatus(ctx, mcpServer, status); err != nil {
		logger.Error(err, "Failed to apply MCPServer status")
		return ctrl.Result{}, err
	}

	r.updateTLSCABundleHash(mcpServer, tlsCABundleHash, verifiedCondition)

	if capDiff != "" {
		capabilityChangesTotal.WithLabelValues(mcpServer.Name, mcpServer.Namespace).Inc()
		r.emitCapabilityChangeDetected(mcpServer, capDiff)
		auditCapabilityChange(ctx, mcpServer, capDiff)
	}

	logger.Info("Successfully reconciled MCPServer",
		"accepted", acceptedCondition.Status,
		"available", availableCondition.Status,
		"verified", verifiedCondition.Status)

	// Deployment progress is driven by the Deployment and Pod watches rather than a
	// timed requeue; pod-level failures surface via podDiagnosticsChangedPredicate.

	// If MCP endpoint is not yet reachable, requeue with exponential backoff.
	// retryCount is 1-based (already incremented); handshakeRequeue adjusts internally.
	if result, done := handshakeRequeue(ctx, mcpServer, verifiedCondition, int(handshakeRetryCount)); done {
		return result, nil
	}

	return ctrl.Result{}, gwErr
}
