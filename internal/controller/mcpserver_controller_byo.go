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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	acv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1/applyconfiguration/api/v1alpha1"
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
func getWorkloadStatus(ctx context.Context, r client.Reader, name string, kind mcpv1alpha1.WorkloadKind, namespace string) (WorkloadStatus, error) {
	switch kind {
	case mcpv1alpha1.WorkloadKindDeployment:
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

	case mcpv1alpha1.WorkloadKindDaemonSet:
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

	case mcpv1alpha1.WorkloadKindStatefulSet:
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
		if p.Name == "mcp" {
			return p.Port, nil
		}
	}

	return svc.Spec.Ports[0].Port, nil
}

// reconcileBYO handles the reconciliation path for BYO workloads.
// It skips Deployment/NetworkPolicy creation and reads status from the referenced workload.
func (r *MCPServerReconciler) reconcileBYO(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
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

	var readyCondition metav1.Condition
	if ws.Ready {
		readyCondition = newReadyCondition(metav1.ConditionTrue, ReasonAvailable,
			fmt.Sprintf("BYO %s is ready (%d of %d instances healthy)",
				ref.Kind, ws.ReadyReplicas, ws.TotalReplicas),
			mcpServer.Generation, mcpServer.Status.Conditions)
	} else if ws.TotalReplicas == 0 && ws.ReadyReplicas == 0 {
		reason := ReasonInitializing
		msg := fmt.Sprintf("Waiting for BYO %s to report status", ref.Kind)
		if ws.DesiredReplicas != nil && *ws.DesiredReplicas == 0 {
			reason = ReasonScaledToZero
			msg = fmt.Sprintf("BYO %s is scaled to zero", ref.Kind)
		}
		readyCondition = newReadyCondition(metav1.ConditionUnknown, reason,
			msg, mcpServer.Generation, mcpServer.Status.Conditions)
	} else {
		msg := fmt.Sprintf("BYO %s is not ready (%d of %d instances healthy)",
			ref.Kind, ws.ReadyReplicas, ws.TotalReplicas)
		if ws.Message != "" {
			msg = ws.Message
		}
		readyCondition = newReadyCondition(metav1.ConditionFalse, ReasonDeploymentUnavailable,
			msg, mcpServer.Generation, mcpServer.Status.Conditions)
	}

	recordCondition(mcpServer.Name, mcpServer.Namespace,
		readyCondition.Type, string(readyCondition.Status), readyCondition.Reason)

	// Resolve service name and port
	serviceName := mcpServer.Name
	port := mcpServer.Spec.Config.Port
	if mcpServer.Spec.ServiceRef != nil {
		serviceName = mcpServer.Spec.ServiceRef.Name
		if port == 0 {
			resolvedPort, resolveErr := resolveServicePort(ctx, r.APIReader, serviceName, mcpServer.Namespace, port)
			if resolveErr != nil {
				logger.Error(resolveErr, "Failed to resolve BYO Service port")
				return ctrl.Result{}, resolveErr
			}
			port = resolvedPort
		}
	}

	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = defaultMCPPath
	}

	mcpURL := fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d%s",
		urlScheme(mcpServer), serviceName, mcpServer.Namespace, port, path)

	// MCP handshake for BYO workloads
	var tlsCABundleHash string
	if mcpServer.Spec.Transport != nil && mcpServer.Spec.Transport.TLS != nil {
		tlsCABundleHash = computeTLSCABundleHash(ctx, r.APIReader, mcpServer.Namespace, mcpServer.Spec.Transport.TLS)
	}

	var serverInfo *mcpv1alpha1.MCPServerInfo
	readyCondition, serverInfo = r.reconcileHandshake(ctx, mcpServer, mcpURL, readyCondition, tlsCABundleHash)

	if pendingServerReadyEvent &&
		readyCondition.Status == metav1.ConditionTrue &&
		readyCondition.Reason == ReasonAvailable {
		r.emitServerReady(mcpServer)
	}

	handshakeRetryCount := r.reconcileHandshakeEventsAndRetryCount(mcpServer, readyCondition)

	workloadName := fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
	workloadSummary := fmt.Sprintf("BYO:%s/%s", ref.Kind, ref.Name)

	status := acv1alpha1.MCPServerStatus().
		WithObservedGeneration(mcpServer.Generation).
		WithServiceName(serviceName).
		WithWorkloadName(workloadName).
		WithWorkloadSummary(workloadSummary).
		WithHandshakeRetryCount(handshakeRetryCount).
		WithReplicas(ws.TotalReplicas).
		WithReadyReplicas(ws.ReadyReplicas).
		WithConditions(
			conditionToAC(acceptedCondition),
			conditionToAC(readyCondition),
		)

	status = withAddressWhenAvailable(status, readyCondition, mcpURL)

	capDiff := capabilityChangeMessage(mcpServer, serverInfo)
	if serverInfo != nil {
		status = status.WithServerInfo(serverInfoToAC(serverInfo))
	}

	if err := r.applyStatus(ctx, mcpServer, status); err != nil {
		logger.Error(err, "Failed to apply MCPServer status")
		return ctrl.Result{}, err
	}

	r.updateTLSCABundleHash(mcpServer, tlsCABundleHash, readyCondition)

	if capDiff != "" {
		capabilityChangesTotal.WithLabelValues(mcpServer.Name, mcpServer.Namespace).Inc()
		r.emitCapabilityChangeDetected(mcpServer, capDiff)
		auditCapabilityChange(ctx, mcpServer, capDiff)
	}

	logger.Info("Successfully reconciled BYO MCPServer",
		"workload", workloadName,
		"ready", readyCondition.Status)

	if readyCondition.Status == metav1.ConditionFalse && readyCondition.Reason == ReasonMCPEndpointUnavailable {
		retryCount := int(handshakeRetryCount)
		if retryCount >= maxMCPHandshakeRetries {
			return ctrl.Result{}, nil
		}
		delay := mcpHandshakeBackoff(retryCount - 1)
		return ctrl.Result{RequeueAfter: delay}, nil
	}

	return ctrl.Result{}, nil
}
