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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

// WorkloadStatus normalizes status from different workload kinds.
type WorkloadStatus struct {
	Ready              bool
	ReadyReplicas      int32
	TotalReplicas      int32
	DesiredReplicas    *int32
	NoSchedulableNodes bool
	Selector           *metav1.LabelSelector
	Message            string
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
		ws := WorkloadStatus{
			Ready:           dep.Status.ReadyReplicas > 0 && dep.Status.ReadyReplicas >= total,
			ReadyReplicas:   dep.Status.ReadyReplicas,
			TotalReplicas:   total,
			DesiredReplicas: dep.Spec.Replicas,
		}
		ws.Selector = dep.Spec.Selector
		return ws, nil

	case mcpv1beta1.WorkloadKindDaemonSet:
		ds := &appsv1.DaemonSet{}
		if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, ds); err != nil {
			if apierrors.IsNotFound(err) {
				return WorkloadStatus{Message: fmt.Sprintf("referenced DaemonSet %s not found", name)}, err
			}
			return WorkloadStatus{}, err
		}
		ws := WorkloadStatus{
			Ready:         ds.Status.NumberReady > 0 && ds.Status.NumberReady >= ds.Status.DesiredNumberScheduled,
			ReadyReplicas: ds.Status.NumberReady,
			TotalReplicas: ds.Status.DesiredNumberScheduled,
		}
		ws.Selector = ds.Spec.Selector
		if ds.Status.DesiredNumberScheduled == 0 {
			ws.NoSchedulableNodes = true
			ws.Message = fmt.Sprintf("DaemonSet %s has no nodes matching its scheduling constraints", name)
		}
		return ws, nil

	case mcpv1beta1.WorkloadKindStatefulSet:
		sts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, sts); err != nil {
			if apierrors.IsNotFound(err) {
				return WorkloadStatus{Message: fmt.Sprintf("referenced StatefulSet %s not found", name)}, err
			}
			return WorkloadStatus{}, err
		}
		total := ptr.Deref(sts.Spec.Replicas, 1)
		ws := WorkloadStatus{
			Ready:           sts.Status.ReadyReplicas > 0 && sts.Status.ReadyReplicas >= total,
			ReadyReplicas:   sts.Status.ReadyReplicas,
			TotalReplicas:   total,
			DesiredReplicas: sts.Spec.Replicas,
		}
		ws.Selector = sts.Spec.Selector
		return ws, nil

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

// serviceNameFor returns the Service identity even when endpoint resolution or
// configuration validation fails.
func serviceNameFor(server *mcpv1beta1.MCPServer) string {
	if server.Spec.ServiceRef != nil {
		return server.Spec.ServiceRef.Name
	}
	return server.Name
}

// ResolveServiceEndpoint returns the Service name and port used for MCP traffic.
// Gateway providers use the same resolution as the MCP handshake, including
// inferred ports on externally managed Services.
func ResolveServiceEndpoint(ctx context.Context, reader client.Reader, server *mcpv1beta1.MCPServer) (string, int32, error) {
	if server.Spec.ServiceRef == nil {
		return serviceNameFor(server), server.Spec.Config.Port, nil
	}
	name := serviceNameFor(server)
	port, err := resolveServicePort(ctx, reader, name, server.Namespace, server.Spec.Config.Port)
	return name, port, err
}

// reconcileServiceOrBYO either reconciles the operator-managed Service or
// resolves the BYO Service port. Service identity comes from serviceNameFor.
func (r *MCPServerReconciler) reconcileServiceOrBYO(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
	selector map[string]string,
) (int32, error) {
	if mcpServer.Spec.ServiceRef != nil {
		_, port, err := ResolveServiceEndpoint(ctx, r.APIReader, mcpServer)
		return port, err
	}

	serviceStart := time.Now()
	if err := r.reconcileService(ctx, mcpServer, selector); err != nil {
		reconcileDuration.With(prometheus.Labels{keyPhase: ReconcilePhaseService}).Observe(time.Since(serviceStart).Seconds())
		return 0, err
	}
	reconcileDuration.With(prometheus.Labels{keyPhase: ReconcilePhaseService}).Observe(time.Since(serviceStart).Seconds())
	return mcpServer.Spec.Config.Port, nil
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
	case ws.NoSchedulableNodes:
		return newAvailableCondition(metav1.ConditionFalse, ReasonWorkloadUnavailable,
			ws.Message, generation, existingConditions)
	case ws.TotalReplicas == 0 && ws.ReadyReplicas == 0:
		return newAvailableCondition(metav1.ConditionUnknown, ReasonInitializing,
			fmt.Sprintf("Waiting for BYO %s to report status", kind), generation, existingConditions)
	default:
		msg := fmt.Sprintf("BYO %s is not ready (%d of %d instances healthy)", kind, ws.ReadyReplicas, ws.TotalReplicas)
		if ws.Message != "" {
			msg = ws.Message
		}
		return newAvailableCondition(metav1.ConditionFalse, ReasonWorkloadUnavailable,
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
	ws WorkloadStatus,
) (ctrl.Result, error) {
	ref := mcpServer.Spec.WorkloadRef

	availableCondition := byoAvailableCondition(ws, ref.Kind, mcpServer.Generation, mcpServer.Status.Conditions)
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		availableCondition.Type, string(availableCondition.Status), availableCondition.Reason)
	r.maybeEmitWorkloadUnavailableEvent(mcpServer, availableCondition)

	status := workloadStatusConfiguration(mcpServer, ws.TotalReplicas, ws.ReadyReplicas)
	var selector map[string]string
	if ws.Selector != nil {
		selector = ws.Selector.MatchLabels
	}
	port, err := r.reconcileServiceOrBYO(ctx, mcpServer, selector)
	if err != nil {
		return r.handleResourceFailure(ctx, mcpServer, status, acceptedCondition, err, resourceFailureParams{
			counter:     serviceFailuresTotal,
			reason:      ReasonServiceUnavailable,
			resource:    "Service",
			isDuplicate: duplicateServiceUnavailable,
			emitEvent:   r.emitServiceReconcileFailed,
		})
	}
	return r.reconcileEndpoint(ctx, mcpServer, acceptedCondition, availableCondition, status, port, nil)
}
