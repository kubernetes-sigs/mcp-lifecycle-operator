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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	acv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1/applyconfiguration/api/v1beta1"
)

// WorkloadStatus normalizes status from different workload kinds.
type WorkloadStatus struct {
	Ready           bool
	ReadyReplicas   int32
	TotalReplicas   int32
	DesiredReplicas *int32
	Message         string
}

// getWorkloadStatus fetches the referenced BYO workload and returns normalized status.
func getWorkloadStatus(ctx context.Context, r client.Reader, name string, kind mcpv1beta1.WorkloadKind, namespace string) (WorkloadStatus, error) {
	switch kind {
	case mcpv1beta1.WorkloadKindDeployment:
		dep := &appsv1.Deployment{}
		if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, dep); err != nil {
			if apierrors.IsNotFound(err) {
				return WorkloadStatus{Message: fmt.Sprintf("referenced Deployment %s not found", name)}, err
			}
			return WorkloadStatus{}, err
		}
		total := ptr.Deref(dep.Spec.Replicas, 1)
		return WorkloadStatus{
			Ready:           dep.Status.ReadyReplicas > 0 && dep.Status.ReadyReplicas >= total,
			ReadyReplicas:   dep.Status.ReadyReplicas,
			TotalReplicas:   total,
			DesiredReplicas: dep.Spec.Replicas,
		}, nil

	case mcpv1beta1.WorkloadKindDaemonSet:
		ds := &appsv1.DaemonSet{}
		if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, ds); err != nil {
			if apierrors.IsNotFound(err) {
				return WorkloadStatus{Message: fmt.Sprintf("referenced DaemonSet %s not found", name)}, err
			}
			return WorkloadStatus{}, err
		}
		return WorkloadStatus{
			Ready:         ds.Status.NumberReady > 0 && ds.Status.NumberReady >= ds.Status.DesiredNumberScheduled,
			ReadyReplicas: ds.Status.NumberReady,
			TotalReplicas: ds.Status.DesiredNumberScheduled,
		}, nil

	case mcpv1beta1.WorkloadKindStatefulSet:
		sts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, sts); err != nil {
			if apierrors.IsNotFound(err) {
				return WorkloadStatus{Message: fmt.Sprintf("referenced StatefulSet %s not found", name)}, err
			}
			return WorkloadStatus{}, err
		}
		total := ptr.Deref(sts.Spec.Replicas, 1)
		return WorkloadStatus{
			Ready:           sts.Status.ReadyReplicas > 0 && sts.Status.ReadyReplicas >= total,
			ReadyReplicas:   sts.Status.ReadyReplicas,
			TotalReplicas:   total,
			DesiredReplicas: sts.Spec.Replicas,
		}, nil

	default:
		return WorkloadStatus{}, fmt.Errorf("unsupported workload kind: %s", kind)
	}
}

// resolveServicePort determines the port to use for the MCP endpoint when
// serviceRef is set. If configPort > 0, it is used directly. Otherwise the
// referenced Service is fetched and a port named "mcp" is preferred, falling
// back to the first port.
func resolveServicePort(ctx context.Context, r client.Reader, serviceName, namespace string, configPort int32) (int32, error) {
	if configPort > 0 {
		return configPort, nil
	}

	svc := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Name: serviceName, Namespace: namespace}, svc); err != nil {
		if apierrors.IsNotFound(err) {
			return 0, fmt.Errorf("referenced Service %s not found", serviceName)
		}
		return 0, err
	}

	if len(svc.Spec.Ports) == 0 {
		return 0, fmt.Errorf("referenced Service %s has no ports", serviceName)
	}

	for _, p := range svc.Spec.Ports {
		if p.Name == mcpPortName {
			return p.Port, nil
		}
	}

	return svc.Spec.Ports[0].Port, nil
}

// reconcileServiceOrBYO either reconciles the operator-managed Service or
// resolves the BYO Service name and port. Returns serviceName, port, error.
func (r *MCPServerReconciler) reconcileServiceOrBYO(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
) (string, int32, error) {
	if mcpServer.Spec.ServiceRef != nil {
		serviceName := mcpServer.Spec.ServiceRef.Name
		port, err := resolveServicePort(ctx, r.APIReader, serviceName, mcpServer.Namespace, mcpServer.Spec.Config.Port)
		if err != nil {
			return "", 0, fmt.Errorf("resolving BYO service port: %w", err)
		}
		return serviceName, port, nil
	}

	serviceStart := time.Now()
	if err := r.reconcileService(ctx, mcpServer); err != nil {
		reconcileDuration.With(prometheus.Labels{keyPhase: ReconcilePhaseService}).Observe(time.Since(serviceStart).Seconds())
		return "", 0, err
	}
	reconcileDuration.With(prometheus.Labels{keyPhase: ReconcilePhaseService}).Observe(time.Since(serviceStart).Seconds())
	return mcpServer.Name, mcpServer.Spec.Config.Port, nil
}

// byoAvailableCondition derives the Available condition for a BYO workload from
// its normalized status, mirroring reconcileAvailableCondition's semantics for
// operator-managed Deployments.
func byoAvailableCondition(
	ws WorkloadStatus,
	kind mcpv1beta1.WorkloadKind,
	generation int64,
	existingConditions []metav1.Condition,
) metav1.Condition {
	switch {
	case ws.Ready:
		return newAvailableCondition(metav1.ConditionTrue, ReasonAvailable,
			fmt.Sprintf("BYO %s is ready (%d of %d instances healthy)", kind, ws.ReadyReplicas, ws.TotalReplicas),
			generation, existingConditions)
	case ws.DesiredReplicas != nil && *ws.DesiredReplicas == 0:
		return newAvailableCondition(metav1.ConditionTrue, ReasonScaledToZero,
			fmt.Sprintf("BYO %s is scaled to zero", kind), generation, existingConditions)
	case ws.TotalReplicas == 0 && ws.ReadyReplicas == 0:
		return newAvailableCondition(metav1.ConditionUnknown, ReasonInitializing,
			fmt.Sprintf("Waiting for BYO %s to report status", kind), generation, existingConditions)
	default:
		msg := fmt.Sprintf("BYO %s is not ready (%d of %d instances healthy)", kind, ws.ReadyReplicas, ws.TotalReplicas)
		if ws.Message != "" {
			msg = ws.Message
		}
		return newAvailableCondition(metav1.ConditionFalse, ReasonDeploymentUnavailable,
			msg, generation, existingConditions)
	}
}

// reconcileBYO handles the reconciliation path for BYO workloads. It skips
// Deployment/NetworkPolicy creation and reads status from the referenced
// workload, then performs the MCP handshake against the resolved endpoint.
func (r *MCPServerReconciler) reconcileBYO(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
	acceptedCondition metav1.Condition,
	pendingServerReadyEvent bool,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	ref := mcpServer.Spec.WorkloadRef

	ws, err := getWorkloadStatus(ctx, r.APIReader, ref.Name, ref.Kind, mcpServer.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("BYO workload not found, should have been caught by validation")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	availableCondition := byoAvailableCondition(ws, ref.Kind, mcpServer.Generation, mcpServer.Status.Conditions)
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		availableCondition.Type, string(availableCondition.Status), availableCondition.Reason)
	r.maybeEmitDeploymentUnavailableEvent(mcpServer, availableCondition)

	// Resolve service name and port for the MCP endpoint URL.
	serviceName := mcpServer.Name
	port := mcpServer.Spec.Config.Port
	if mcpServer.Spec.ServiceRef != nil {
		serviceName = mcpServer.Spec.ServiceRef.Name
		resolvedPort, resolveErr := resolveServicePort(ctx, r.APIReader, serviceName, mcpServer.Namespace, port)
		if resolveErr != nil {
			logger.Error(resolveErr, "Failed to resolve BYO Service port")
			return ctrl.Result{}, resolveErr
		}
		port = resolvedPort
	}

	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = DefaultMCPPath
	}

	mcpURL := fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d%s",
		urlScheme(mcpServer), serviceName, mcpServer.Namespace, port, path)

	// Compute current TLS CA bundle hash so the handshake is re-verified when the
	// CA bundle Secret content changes (which does not bump generation).
	var tlsCABundleHash string
	if mcpServer.Spec.Transport != nil && mcpServer.Spec.Transport.TLS != nil {
		tlsCABundleHash = computeTLSCABundleHash(ctx, r.APIReader, mcpServer.Namespace, mcpServer.Spec.Transport.TLS)
	}

	// If the workload is available, verify the MCP endpoint.
	verifiedCondition, serverInfo := r.reconcileHandshake(ctx, mcpServer, mcpURL, availableCondition, tlsCABundleHash)
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		verifiedCondition.Type, string(verifiedCondition.Status), verifiedCondition.Reason)

	handshakeRetryCount := r.reconcileHandshakeEventsAndRetryCount(mcpServer, &verifiedCondition)

	// Normal Event once per transition to fully ready (Available + Verified).
	if pendingServerReadyEvent &&
		availableCondition.Status == metav1.ConditionTrue &&
		verifiedCondition.Status == metav1.ConditionTrue {
		r.emitServerReady(mcpServer)
	}

	workloadName := fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
	workloadSummary := fmt.Sprintf("BYO:%s/%s", ref.Kind, ref.Name)

	conditions := r.appendPersistentConditions(mcpServer, []*v1ac.ConditionApplyConfiguration{
		conditionToAC(acceptedCondition),
		conditionToAC(availableCondition),
		conditionToAC(verifiedCondition),
	}, nil)

	status := acv1beta1.MCPServerStatus().
		WithObservedGeneration(mcpServer.Generation).
		WithServiceName(serviceName).
		WithWorkloadName(workloadName).
		WithWorkloadSummary(workloadSummary).
		WithReplicas(ws.TotalReplicas).
		WithReadyReplicas(ws.ReadyReplicas).
		WithConditions(conditions...)

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

	logger.Info("Successfully reconciled BYO MCPServer",
		"workload", workloadName,
		"available", availableCondition.Status,
		"verified", verifiedCondition.Status)

	// If the MCP endpoint is not yet reachable, requeue with exponential backoff.
	if result, done := handshakeRequeue(ctx, mcpServer, verifiedCondition, int(handshakeRetryCount)); done {
		return result, nil
	}

	return ctrl.Result{}, nil
}
