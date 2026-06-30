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
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	if err := r.APIReader.List(ctx, podList,
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

// analyzePodFailures inspects pod container statuses to build a human-readable
// message describing why pods are unhealthy. Returns "" if no specific failure
// can be identified. Only the first failure across all pods is returned to keep
// the status condition message concise.
func analyzePodFailures(pods []corev1.Pod) string {
	for _, pod := range pods {
		for _, cs := range pod.Status.InitContainerStatuses {
			if msg := analyzeContainerStatus(cs, pod.Name); msg != "" {
				return msg
			}
		}

		for _, cs := range pod.Status.ContainerStatuses {
			if msg := analyzeContainerStatus(cs, pod.Name); msg != "" {
				return msg
			}
		}
	}

	return ""
}

// analyzeContainerStatus checks a single container status for known failure
// patterns and returns a human-readable message, or "" if none is found.
func analyzeContainerStatus(cs corev1.ContainerStatus, podName string) string {
	if w := cs.State.Waiting; w != nil {
		switch w.Reason {
		case WaitingReasonImagePullBackOff, WaitingReasonErrImagePull:
			return fmt.Sprintf("Image pull failed for %q: %s (pod: %s)", cs.Image, w.Message, podName)
		case WaitingReasonCrashLoopBackOff:
			if t := cs.LastTerminationState.Terminated; t != nil {
				return fmt.Sprintf("Container crashing: exit code %d, restarts: %d (pod: %s)",
					t.ExitCode, cs.RestartCount, podName)
			}
			return fmt.Sprintf("Container crashing: restarts: %d (pod: %s)",
				cs.RestartCount, podName)
		case WaitingReasonCreateContainerConfigError:
			return fmt.Sprintf("Container config error: %s (pod: %s)", w.Message, podName)
		}
	}
	if t := cs.State.Terminated; t != nil {
		if t.Reason == TerminatedReasonOOMKilled {
			return fmt.Sprintf("Container OOMKilled: exit code %d, restarts: %d (pod: %s)",
				t.ExitCode, cs.RestartCount, podName)
		}
	}
	// Running but not ready with restarts indicates a probe failure.
	// We require RestartCount > 0 to avoid false positives during initial startup
	// when the readiness probe hasn't passed yet.
	if cs.State.Running != nil && !cs.Ready && cs.RestartCount > 0 {
		return fmt.Sprintf("Container not passing health checks: restarts: %d (pod: %s)",
			cs.RestartCount, podName)
	}
	return ""
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
