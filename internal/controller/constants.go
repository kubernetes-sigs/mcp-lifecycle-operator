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

// Kubernetes label keys reserved and applied by the operator on managed workloads.
const (
	LabelKeyApp       = "app"
	LabelKeyMCPServer = "mcp-server"
)

// ManagedWorkloadName is the standard app label value and name of the main container
// in operator-created Deployments.
const ManagedWorkloadName = "mcp-server"

// ReconcilePhase is the value for the "phase" label on mcpserver_reconcile_phase_duration_seconds.
const (
	ReconcilePhaseValidation = "validation"
	ReconcilePhaseDeployment = "deployment"
	ReconcilePhaseService    = "service"
)

// MetricReasonReconcileError is the `reason` label on deployment/service failure counters
// when the corresponding reconcile step returns an error.
const MetricReasonReconcileError = "ReconcileError"
