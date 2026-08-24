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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// reconcileReadyCondition determines the Ready condition for an MCPServer by
// inspecting deployment status. When the deployment is in a failure state, it
// fetches pods to surface specific error details (image pull failures, crash
// loops, OOM, etc.) rather than showing generic deployment messages.
func (r *MCPServerReconciler) reconcileReadyCondition(
	ctx context.Context,
	deployment *appsv1.Deployment,
	acceptedCondition metav1.Condition,
	generation int64,
	existingConditions []metav1.Condition,
) metav1.Condition {
	if acceptedCondition.Status == metav1.ConditionFalse {
		return newReadyCondition(metav1.ConditionFalse, ReasonConfigurationInvalid,
			"Configuration must be fixed before server can start", generation, existingConditions)
	}

	// Scaling to zero is an intentional, valid desired state (not a failure).
	if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 {
		return newReadyCondition(metav1.ConditionTrue, ReasonScaledToZero,
			"Server is ready (scaled to 0 replicas)", generation, existingConditions)
	}

	if len(deployment.Status.Conditions) == 0 && deployment.Status.ReadyReplicas == 0 {
		return newReadyCondition(metav1.ConditionUnknown, ReasonInitializing,
			"Waiting for Deployment to report status", generation, existingConditions)
	}

	state := extractDeploymentState(deployment)

	if deployment.Status.ObservedGeneration > 0 && deployment.Status.ObservedGeneration < deployment.Generation {
		return newReadyCondition(metav1.ConditionFalse, ReasonDeploymentUnavailable,
			"Deployment is processing spec update", generation, existingConditions)
	}

	if state.available && deployment.Status.ReadyReplicas > 0 {
		return newReadyCondition(metav1.ConditionTrue, ReasonAvailable,
			fmt.Sprintf("MCP server is ready (%d of %d instances healthy)",
				deployment.Status.ReadyReplicas, ptr.Deref(deployment.Spec.Replicas, 1)),
			generation, existingConditions)
	}

	// Failure path — fetch pods for detailed diagnostics.
	podFailureMessage := r.getPodFailureMessage(ctx, deployment)
	if state.replicaFailure ||
		(!state.progressing && !state.available) ||
		(state.progressing && deployment.Status.ReadyReplicas == 0 && podFailureMessage != "") {
		if podFailureMessage == "" {
			podFailureMessage = "No healthy instances (pod details unavailable)"
			if state.message != "" {
				podFailureMessage = podFailureMessage + ": " + state.message
			}
		}

		return newReadyCondition(metav1.ConditionFalse, ReasonDeploymentUnavailable,
			podFailureMessage, generation, existingConditions)
	}

	return newReadyCondition(metav1.ConditionFalse, ReasonDeploymentUnavailable,
		"Waiting for instances to become healthy", generation, existingConditions)
}

// getPodFailureMessage lists the pods for a deployment and returns a detailed
// message from their container statuses. Returns "" when pod details are
// unavailable or no known failure pattern is found.
func (r *MCPServerReconciler) getPodFailureMessage(
	ctx context.Context,
	deployment *appsv1.Deployment,
) string {
	logger := log.FromContext(ctx)

	if deployment.Spec.Selector == nil {
		return ""
	}

	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		logger.Error(err, "Failed to parse deployment selector")
		return ""
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(deployment.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		logger.Error(err, "Failed to list pods for deployment failure analysis")
		return ""
	}

	return analyzePodFailures(podList.Items)
}

// deploymentState summarises the health signals from a Deployment's status conditions.
type deploymentState struct {
	available      bool
	progressing    bool
	replicaFailure bool
	message        string // most relevant message from unhealthy conditions
}

// extractDeploymentState reads the standard Deployment condition types and
// returns a compact summary used by reconcileReadyCondition.
func extractDeploymentState(deployment *appsv1.Deployment) deploymentState {
	var state deploymentState
	var progressingMessage string
	var availableMessage string
	for _, cond := range deployment.Status.Conditions {
		switch cond.Type {
		case appsv1.DeploymentAvailable:
			state.available = cond.Status == corev1.ConditionTrue
			if cond.Status == corev1.ConditionFalse {
				availableMessage = cond.Message
			}
		case appsv1.DeploymentProgressing:
			state.progressing = cond.Status == corev1.ConditionTrue
			if cond.Status == corev1.ConditionFalse {
				progressingMessage = cond.Message
			}
		case appsv1.DeploymentReplicaFailure:
			if cond.Status == corev1.ConditionTrue {
				state.replicaFailure = true
				if cond.Message != "" {
					state.message = cond.Message
				}
			}
		}
	}

	// Prefer ReplicaFailure message; fall back to Progressing, then Available.
	if state.message == "" {
		state.message = progressingMessage
	}
	if state.message == "" {
		state.message = availableMessage
	}

	return state
}

// failureKind enumerates the container failure states this controller recognises.
type failureKind string

const (
	failureImagePull   failureKind = "ImagePull"
	failureCrashLoop   failureKind = "CrashLoop"
	failureConfigError failureKind = "ConfigError"
	failureOOMKilled   failureKind = "OOMKilled"
	failureNotReady    failureKind = "NotReady"
)

// containerFailure is the structured description of why a container is unhealthy.
// Both the human-readable condition message and the pod-watch fingerprint are
// projections of this value, so the set of recognised failure states is defined
// in exactly one place: classifyContainer.
type containerFailure struct {
	Kind      failureKind
	Pod       string
	Container string
	Init      bool
	Image     string
	Detail    string // Waiting.Message, where it carries useful information
	ExitCode  *int32
	Restarts  int32 // snapshot for diagnostics; restart-only updates do not trigger reconciliation
}

// classifyContainer checks a single container status for known failure patterns.
// Returns false when the container is not in a recognised failure state.
func classifyContainer(cs corev1.ContainerStatus, podName string, init bool) (containerFailure, bool) {
	f := containerFailure{
		Pod:       podName,
		Container: cs.Name,
		Init:      init,
		Image:     cs.Image,
		Restarts:  cs.RestartCount,
	}

	if w := cs.State.Waiting; w != nil {
		switch w.Reason {
		case WaitingReasonImagePullBackOff, WaitingReasonErrImagePull:
			f.Kind, f.Detail = failureImagePull, w.Message
			return f, true
		case WaitingReasonCrashLoopBackOff:
			f.Kind = failureCrashLoop
			if t := cs.LastTerminationState.Terminated; t != nil {
				f.ExitCode = new(t.ExitCode)
			}
			// Waiting.Message is dropped here: it embeds a changing back-off
			// duration, which would churn the fingerprint below.
			return f, true
		case WaitingReasonCreateContainerConfigError:
			f.Kind, f.Detail = failureConfigError, w.Message
			return f, true
		}
	}

	if t := cs.State.Terminated; t != nil && t.Reason == TerminatedReasonOOMKilled {
		f.Kind, f.ExitCode = failureOOMKilled, new(t.ExitCode)
		return f, true
	}

	// Running but not ready with restarts indicates a probe failure.
	// We require RestartCount > 0 to avoid false positives during initial startup
	// when the readiness probe hasn't passed yet.
	if cs.State.Running != nil && !cs.Ready && cs.RestartCount > 0 {
		f.Kind = failureNotReady
		return f, true
	}

	return containerFailure{}, false
}

// Message renders the human-readable diagnostic surfaced on the Ready condition.
func (f containerFailure) Message() string {
	switch f.Kind {
	case failureImagePull:
		return fmt.Sprintf("Image pull failed for %q: %s (pod: %s)", f.Image, f.Detail, f.Pod)
	case failureCrashLoop:
		if f.ExitCode != nil {
			return fmt.Sprintf("Container crashing: exit code %d, restarts: %d (pod: %s)",
				*f.ExitCode, f.Restarts, f.Pod)
		}
		return fmt.Sprintf("Container crashing: restarts: %d (pod: %s)",
			f.Restarts, f.Pod)
	case failureConfigError:
		return fmt.Sprintf("Container config error: %s (pod: %s)", f.Detail, f.Pod)
	case failureOOMKilled:
		return fmt.Sprintf("Container OOMKilled: exit code %d, restarts: %d (pod: %s)",
			ptr.Deref(f.ExitCode, 0), f.Restarts, f.Pod)
	case failureNotReady:
		return fmt.Sprintf("Container not passing health checks: restarts: %d (pod: %s)",
			f.Restarts, f.Pod)
	}
	return ""
}

// Signature fingerprints everything that affects the reported diagnosis except
// Restarts, which ticks constantly and would cause reconcile churn in the pod
// watch predicate. Field-driven rather than branch-driven: a new failureKind is
// fingerprinted correctly without changing this method.
func (f containerFailure) Signature() string {
	exit := "none"
	if f.ExitCode != nil {
		exit = fmt.Sprintf("%d", *f.ExitCode)
	}
	return fmt.Sprintf("%s|%s|%t|%s|%s|%s",
		f.Kind, f.Container, f.Init, f.Image, f.Detail, exit)
}

// firstFailure returns the failure that would be reported for a pod, preferring
// init containers, matching the order used when rendering the condition message.
func firstFailure(pod *corev1.Pod) (containerFailure, bool) {
	for _, cs := range pod.Status.InitContainerStatuses {
		if f, ok := classifyContainer(cs, pod.Name, true); ok {
			return f, true
		}
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if f, ok := classifyContainer(cs, pod.Name, false); ok {
			return f, true
		}
	}

	return containerFailure{}, false
}

// analyzePodFailures inspects pod container statuses to build a human-readable
// message describing why pods are unhealthy. Returns "" if no specific failure
// can be identified. Only the first failure across all pods is returned to keep
// the status condition message concise.
func analyzePodFailures(pods []corev1.Pod) string {
	for i := range pods {
		if f, ok := firstFailure(&pods[i]); ok {
			return f.Message()
		}
	}

	return ""
}

// podFailureSignature returns a stable fingerprint of the failure a pod would
// report, or "" when it is healthy. Used by the pod watch predicate to trigger a
// reconcile only when the reported diagnosis would actually change.
func podFailureSignature(pod *corev1.Pod) string {
	f, ok := firstFailure(pod)
	if !ok {
		return ""
	}
	return f.Signature()
}

// podDiagnosticsChangedPredicate filters pod events down to those that would
// change the diagnostic message on the Ready condition.
//
// Pods report status frequently (restart counters, probe results), and the
// Deployment they belong to stops reporting changes once it settles into a failed
// state. Without this filter the watch would either reconcile far more often than
// the timed requeue it replaces, or miss the pod-level transitions entirely.
func podDiagnosticsChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		// Informer sync emits Create for pods that already exist, which is how an
		// operator restart picks up an in-progress failure. Healthy pods fingerprint
		// to "" and are ignored.
		CreateFunc: func(e event.CreateEvent) bool {
			pod, ok := e.Object.(*corev1.Pod)
			return ok && podFailureSignature(pod) != ""
		},

		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPod, okOld := e.ObjectOld.(*corev1.Pod)
			newPod, okNew := e.ObjectNew.(*corev1.Pod)
			if !okOld || !okNew {
				return false
			}
			return podFailureSignature(oldPod) != podFailureSignature(newPod)
		},

		// A failing pod going away invalidates the message derived from it. A
		// healthy pod going away changes ReadyReplicas, which the Deployment watch
		// already covers.
		DeleteFunc: func(e event.DeleteEvent) bool {
			if e.DeleteStateUnknown {
				// Final state was missed; reconcile rather than risk a stale message.
				return true
			}
			pod, ok := e.Object.(*corev1.Pod)
			return ok && podFailureSignature(pod) != ""
		},

		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// newCondition creates a new metav1.Condition with the current timestamp.
func newCondition(
	condType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
	observedGeneration int64,
) metav1.Condition {
	return metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: observedGeneration,
		LastTransitionTime: metav1.Now(),
	}
}

// newReadyCondition creates a Ready condition and preserves the LastTransitionTime
// from existingConditions when the status has not changed.
func newReadyCondition(
	status metav1.ConditionStatus,
	reason string,
	message string,
	generation int64,
	existingConditions []metav1.Condition,
) metav1.Condition {
	c := newCondition(ConditionTypeReady, status, reason, message, generation)
	preserveLastTransitionTime(&c, existingConditions)
	return c
}

func conditionToAC(condition metav1.Condition) *v1ac.ConditionApplyConfiguration {
	return v1ac.Condition().
		WithType(condition.Type).
		WithStatus(condition.Status).
		WithReason(condition.Reason).
		WithMessage(condition.Message).
		WithObservedGeneration(condition.ObservedGeneration).
		WithLastTransitionTime(condition.LastTransitionTime)
}

// preserveLastTransitionTime keeps the existing LastTransitionTime when the
// condition status has not changed, so that timestamps reflect actual transitions.
func preserveLastTransitionTime(condition *metav1.Condition, existingConditions []metav1.Condition) {
	if existing := meta.FindStatusCondition(existingConditions, condition.Type); existing != nil && existing.Status == condition.Status {
		condition.LastTransitionTime = existing.LastTransitionTime
	}
}

func acceptedConditionIsTrue(conditions []metav1.Condition) bool {
	c := meta.FindStatusCondition(conditions, ConditionTypeAccepted)
	return c != nil && c.Status == metav1.ConditionTrue
}

func readyConditionIsAvailable(conditions []metav1.Condition) bool {
	c := meta.FindStatusCondition(conditions, ConditionTypeReady)
	return c != nil && c.Status == metav1.ConditionTrue && c.Reason == ReasonAvailable
}

func duplicateHandshakeUnavailable(conditions []metav1.Condition, message string) bool {
	prevReady := meta.FindStatusCondition(conditions, ConditionTypeReady)
	return prevReady != nil && prevReady.Status == metav1.ConditionFalse &&
		prevReady.Reason == ReasonMCPEndpointUnavailable && prevReady.Message == message
}

func duplicateDeploymentUnavailable(conditions []metav1.Condition, message string) bool {
	prevReady := meta.FindStatusCondition(conditions, ConditionTypeReady)
	return prevReady != nil && prevReady.Status == metav1.ConditionFalse &&
		prevReady.Reason == ReasonDeploymentUnavailable && prevReady.Message == message
}

func duplicateServiceUnavailable(conditions []metav1.Condition, message string) bool {
	prevReady := meta.FindStatusCondition(conditions, ConditionTypeReady)
	return prevReady != nil && prevReady.Status == metav1.ConditionFalse &&
		prevReady.Reason == ReasonServiceUnavailable && prevReady.Message == message
}

func duplicateNetworkPolicyUnavailable(conditions []metav1.Condition, message string) bool {
	prevReady := meta.FindStatusCondition(conditions, ConditionTypeReady)
	return prevReady != nil && prevReady.Status == metav1.ConditionFalse &&
		prevReady.Reason == ReasonNetworkPolicyUnavailable && prevReady.Message == message
}
