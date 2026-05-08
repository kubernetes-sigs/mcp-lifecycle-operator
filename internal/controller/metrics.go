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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const metricsNamespace = "mcpserver"

var (
	// conditionInfo tracks MCPServer resources by condition state.
	// Labels: name, namespace, type (Accepted/Ready), status (True/False/Unknown), reason.
	conditionInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "condition_info",
			Help:      "Current condition state of MCPServer resources. Value is always 1; use labels to filter.",
		},
		[]string{"name", "namespace", "type", "status", "reason"},
	)

	// validationFailuresTotal counts configuration validation failures.
	// Labels: name, namespace, reason.
	validationFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "validation_failures_total",
			Help:      "Total number of configuration validation failures.",
		},
		[]string{"name", "namespace", "reason"},
	)

	// deploymentFailuresTotal counts deployment reconciliation failures.
	// Labels: name, namespace, reason.
	deploymentFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "deployment_failures_total",
			Help:      "Total number of deployment reconciliation failures.",
		},
		[]string{"name", "namespace", "reason"},
	)

	// serviceFailuresTotal counts service reconciliation failures.
	// Labels: name, namespace, reason.
	serviceFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "service_failures_total",
			Help:      "Total number of service reconciliation failures.",
		},
		[]string{"name", "namespace", "reason"},
	)

	// reconcileDuration tracks the duration of reconciliation phases.
	// Labels: phase (ReconcilePhaseValidation / ReconcilePhaseDeployment / ReconcilePhaseService).
	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "reconcile_phase_duration_seconds",
			Help:      "Duration of reconciliation phases in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"phase"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		conditionInfo,
		validationFailuresTotal,
		deploymentFailuresTotal,
		serviceFailuresTotal,
		reconcileDuration,
	)
}

// recordCondition updates the conditionInfo gauge for a given MCPServer condition.
// It clears previous values for the same (name, namespace, type) before setting the new one.
func recordCondition(name, namespace, condType, status, reason string) {
	// Delete all status/reason variants for this condition type to ensure only one is active
	conditionInfo.DeletePartialMatch(prometheus.Labels{
		"name":      name,
		"namespace": namespace,
		"type":      condType,
	})
	conditionInfo.With(prometheus.Labels{
		"name":      name,
		"namespace": namespace,
		"type":      condType,
		"status":    status,
		"reason":    reason,
	}).Set(1)
}

// cleanupMetrics removes all metrics for a deleted MCPServer.
func cleanupMetrics(name, namespace string) {
	conditionInfo.DeletePartialMatch(prometheus.Labels{
		"name":      name,
		"namespace": namespace,
	})
}
