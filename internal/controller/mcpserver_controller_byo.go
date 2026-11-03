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

// workloadObservation contains only the information shared reconciliation needs.
// Native workload resources remain local to validation and availability handling.
type workloadObservation struct {
	selector      *metav1.LabelSelector
	replicas      int32
	readyReplicas int32
	availability  metav1.Condition
}

// getWorkload reads the native resource once. Fetch failures remain errors so
// validation can report them through Accepted rather than a partial observation.
func getWorkload(ctx context.Context, reader client.Reader, name string, kind mcpv1beta1.WorkloadKind, namespace string) (client.Object, error) {
	var obj client.Object
	switch kind {
	case mcpv1beta1.WorkloadKindDeployment:
		obj = &appsv1.Deployment{}
	case mcpv1beta1.WorkloadKindDaemonSet:
		obj = &appsv1.DaemonSet{}
	case mcpv1beta1.WorkloadKindStatefulSet:
		obj = &appsv1.StatefulSet{}
	default:
		return nil, &ValidationError{Reason: ReasonInvalid, Message: fmt.Sprintf("unsupported workloadRef kind: %s", kind)}
	}
	if err := reader.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, obj); err != nil {
		return nil, fmt.Errorf("getting referenced %s %q: %w", kind, name, err)
	}
	return obj, nil
}

// workloadSelector returns the native pod selector without discarding expressions.
func workloadSelector(obj client.Object) *metav1.LabelSelector {
	switch w := obj.(type) {
	case *appsv1.Deployment:
		return w.Spec.Selector
	case *appsv1.DaemonSet:
		return w.Spec.Selector
	case *appsv1.StatefulSet:
		return w.Spec.Selector
	default:
		return nil
	}
}

// observeWorkload derives a small common result directly from native status.
// Each kind owns its availability policy and message; Deployment reuses the
// same condition handling as operator-managed workloads.
func (r *MCPServerReconciler) observeWorkload(ctx context.Context, server *mcpv1beta1.MCPServer, obj client.Object, acceptedCondition metav1.Condition) (workloadObservation, error) {
	observation := workloadObservation{selector: workloadSelector(obj)}
	switch w := obj.(type) {
	case *appsv1.Deployment:
		observation.replicas, observation.readyReplicas = ptr.Deref(w.Spec.Replicas, 1), w.Status.ReadyReplicas
		observation.availability = r.reconcileAvailableCondition(ctx, w, acceptedCondition, server.Generation, server.Status.Conditions)
	case *appsv1.DaemonSet:
		observation.replicas, observation.readyReplicas = w.Status.DesiredNumberScheduled, w.Status.NumberReady
		observation.availability = daemonSetAvailability(server, w)
	case *appsv1.StatefulSet:
		observation.replicas, observation.readyReplicas = ptr.Deref(w.Spec.Replicas, 1), w.Status.ReadyReplicas
		observation.availability = statefulSetAvailability(server, w)
	default:
		return workloadObservation{}, fmt.Errorf("unsupported workload type: %T", obj)
	}
	return observation, nil
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

// workloadProgressCondition distinguishes unobserved and stale status from a
// current workload snapshot. Callers handle intentional scale-to-zero first.
func workloadProgressCondition(server *mcpv1beta1.MCPServer, kind string, generation, observedGeneration int64) (metav1.Condition, bool) {
	if observedGeneration == 0 {
		return newAvailableCondition(metav1.ConditionUnknown, ReasonInitializing,
			fmt.Sprintf("Waiting for BYO %s to report status", kind), server.Generation, server.Status.Conditions), true
	}
	if observedGeneration < generation {
		return newAvailableCondition(metav1.ConditionFalse, ReasonWorkloadUnavailable,
			fmt.Sprintf("BYO %s is processing spec update", kind), server.Generation, server.Status.Conditions), true
	}
	return metav1.Condition{}, false
}

// daemonSetAvailability uses numberAvailable to respect minReadySeconds. Partial
// availability permits verification while unavailable and unscheduled pods remain
// visible in the message. DaemonSets cannot be intentionally scaled to zero.
func daemonSetAvailability(server *mcpv1beta1.MCPServer, ds *appsv1.DaemonSet) metav1.Condition {
	if condition, pending := workloadProgressCondition(server, "DaemonSet", ds.Generation, ds.Status.ObservedGeneration); pending {
		return condition
	}
	status := ds.Status
	if status.DesiredNumberScheduled == 0 {
		return newAvailableCondition(metav1.ConditionFalse, ReasonWorkloadUnavailable,
			fmt.Sprintf("DaemonSet %s has no nodes matching its scheduling constraints", ds.Name), server.Generation, server.Status.Conditions)
	}
	message := fmt.Sprintf("BYO DaemonSet: %d of %d instances available (%d ready); %d unavailable; %d of %d instances scheduled",
		status.NumberAvailable, status.DesiredNumberScheduled, status.NumberReady, status.NumberUnavailable, status.CurrentNumberScheduled, status.DesiredNumberScheduled)
	if status.NumberAvailable > 0 {
		return newAvailableCondition(metav1.ConditionTrue, ReasonAvailable, message, server.Generation, server.Status.Conditions)
	}
	return newAvailableCondition(metav1.ConditionFalse, ReasonWorkloadUnavailable, message, server.Generation, server.Status.Conditions)
}

// statefulSetAvailability uses availableReplicas to respect minReadySeconds and
// reports rollout progress separately from the presence of an available instance.
func statefulSetAvailability(server *mcpv1beta1.MCPServer, sts *appsv1.StatefulSet) metav1.Condition {
	desired := ptr.Deref(sts.Spec.Replicas, 1)
	if desired == 0 {
		return newAvailableCondition(metav1.ConditionTrue, ReasonScaledToZero,
			"BYO StatefulSet is scaled to zero", server.Generation, server.Status.Conditions)
	}
	if condition, pending := workloadProgressCondition(server, "StatefulSet", sts.Generation, sts.Status.ObservedGeneration); pending {
		return condition
	}
	status := sts.Status
	message := fmt.Sprintf("BYO StatefulSet: %d of %d instances available (%d ready)", status.AvailableReplicas, desired, status.ReadyReplicas)
	if status.UpdatedReplicas < desired {
		message += "; rolling update in progress"
	}
	if status.AvailableReplicas > 0 {
		return newAvailableCondition(metav1.ConditionTrue, ReasonAvailable, message, server.Generation, server.Status.Conditions)
	}
	return newAvailableCondition(metav1.ConditionFalse, ReasonWorkloadUnavailable, message, server.Generation, server.Status.Conditions)
}

// reconcileBYO handles the reconciliation path for BYO workloads. It skips
// Deployment/NetworkPolicy creation and reads status from the referenced
// workload, then performs the MCP handshake against the resolved endpoint.
func (r *MCPServerReconciler) reconcileBYO(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
	acceptedCondition metav1.Condition,
	workload client.Object,
) (ctrl.Result, error) {
	observation, err := r.observeWorkload(ctx, mcpServer, workload, acceptedCondition)
	if err != nil {
		return ctrl.Result{}, err
	}
	availableCondition := observation.availability
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		availableCondition.Type, string(availableCondition.Status), availableCondition.Reason)
	r.maybeEmitWorkloadUnavailableEvent(mcpServer, availableCondition)

	status := workloadStatusConfiguration(mcpServer, observation.replicas, observation.readyReplicas)
	var selector map[string]string
	if observation.selector != nil {
		selector = observation.selector.MatchLabels
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
