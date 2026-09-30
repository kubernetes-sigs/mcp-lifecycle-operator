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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// MCPToolset condition types.
const (
	// ConditionTypeServersResolved is True when every entry in spec.servers maps
	// to an existing, ready MCPServer endpoint.
	ConditionTypeServersResolved = "ServersResolved"
	// ConditionTypeReady is True when servers resolved, the provider satisfied the
	// isolation policy, and the dispatcher is deployed with an ExecuteEndpoint.
	ConditionTypeReady = "Ready"
)

// Reasons for the MCPToolset ServersResolved condition.
const (
	ReasonAllServersReady = "AllServersReady"
	ReasonServersMissing  = "ServersMissing"
)

// Reasons for the MCPToolset Ready condition.
const (
	ReasonUnknownProvider       = "UnknownProvider"
	ReasonInsufficientIsolation = "InsufficientIsolation"
	ReasonServersNotResolved    = "ServersNotResolved"
	ReasonDispatcherAvailable   = "DispatcherAvailable"
	ReasonSetupFailed           = "SetupFailed"
	ReasonDispatcherError       = "DispatcherError"
)

// MCPToolsetReconciler reconciles an MCPToolset: it resolves the referenced
// servers, verifies the selected provider satisfies the isolation policy, calls
// the provider's reconcile-time Setup, and deploys the long-lived execute_code
// dispatcher (Deployment + Service, plus the ServiceAccount/Role/RoleBinding the
// dispatcher needs to claim sandboxes at call time). It publishes
// status.executeEndpoint and conditions.
//
// The reconciler owns only long-lived infrastructure; it is never on the
// per-execution hot path (FR-009/SC-006). Executions reach the dispatcher
// directly at ExecuteEndpoint.
type MCPToolsetReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// DispatcherImage is the execute_code dispatcher image the controller deploys
	// (one Deployment per toolset).
	DispatcherImage string
	// RunnerImage is the portable runner image the dispatcher launches per
	// execution. It is injected into the dispatcher's environment.
	RunnerImage string
}

// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcptoolsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcptoolsets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcptoolsets/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get

// Reconcile drives an MCPToolset toward its desired state.
func (r *MCPToolsetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var toolset mcpv1alpha1.MCPToolset
	if err := r.Get(ctx, req.NamespacedName, &toolset); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// The dispatcher Deployment/Service and the provider's backend state are
	// owner-referenced (garbage collected on delete), and the pod provider's
	// Teardown is a no-op, so no finalizer is required for the MVP.
	if !toolset.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// 1. The provider must be registered.
	provider, ok := sandbox.Get(toolset.Spec.Provider)
	if !ok {
		return r.markNotReady(ctx, &toolset, ReasonUnknownProvider,
			fmt.Sprintf("provider %q is not registered (available: %s)",
				toolset.Spec.Provider, strings.Join(sandbox.Names(), ", ")))
	}

	// 2. The provider's declared guarantees must satisfy the isolation policy.
	minIsolation := string(toolset.Spec.Policy.MinIsolation)
	if !provider.IsolationGuarantees().Satisfies(minIsolation) {
		return r.markNotReady(ctx, &toolset, ReasonInsufficientIsolation,
			fmt.Sprintf("provider %q does not satisfy minIsolation %q",
				provider.Name(), effectiveIsolation(minIsolation)))
	}

	// 3. Resolve each referenced server to a reachable endpoint.
	endpoints, unresolved, err := ResolveServers(ctx, r.Client, toolset.Namespace, toolset.Spec.Servers)
	if err != nil {
		// Unexpected API error: retry.
		return ctrl.Result{}, err
	}

	resolvedNames := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		resolvedNames = append(resolvedNames, ep.Server)
	}
	toolset.Status.ResolvedServers = resolvedNames

	if len(unresolved) > 0 {
		meta.SetStatusCondition(&toolset.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeServersResolved,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonServersMissing,
			Message:            unresolvedMessage(unresolved),
			ObservedGeneration: toolset.Generation,
		})
		meta.SetStatusCondition(&toolset.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonServersNotResolved,
			Message:            "one or more referenced servers are missing or not ready",
			ObservedGeneration: toolset.Generation,
		})
		// A watch on MCPServer re-triggers reconcile once the server is ready; no
		// dispatcher is deployed against a not-ready toolset.
		return r.updateStatus(ctx, &toolset)
	}

	meta.SetStatusCondition(&toolset.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeServersResolved,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonAllServersReady,
		Message:            "all referenced servers resolved to a ready endpoint",
		ObservedGeneration: toolset.Generation,
	})

	// 4. Establish provider backend state (idempotent; near no-op for pod).
	timeout := effectiveTimeout(&toolset)
	if err := provider.Setup(ctx, sandbox.ToolsetContext{
		Namespace:       toolset.Namespace,
		Name:            toolset.Name,
		ServerEndpoints: endpoints,
		MinIsolation:    minIsolation,
		Timeout:         timeout,
	}); err != nil {
		logger.Error(err, "provider setup failed", "provider", provider.Name())
		return r.markNotReady(ctx, &toolset, ReasonSetupFailed,
			fmt.Sprintf("provider %q setup failed: %v", provider.Name(), err))
	}

	// 5. Reconcile the dispatcher infrastructure.
	if err := r.reconcileDispatcher(ctx, &toolset, provider.Name(), endpoints, timeout); err != nil {
		logger.Error(err, "dispatcher reconcile failed")
		return r.markNotReady(ctx, &toolset, ReasonDispatcherError,
			fmt.Sprintf("deploying dispatcher: %v", err))
	}

	// 6. Publish the endpoint and Ready=True.
	toolset.Status.ExecuteEndpoint = dispatcherEndpointURL(&toolset)
	meta.SetStatusCondition(&toolset.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonDispatcherAvailable,
		Message:            "dispatcher deployed and execute endpoint published",
		ObservedGeneration: toolset.Generation,
	})
	return r.updateStatus(ctx, &toolset)
}

// markNotReady sets Ready=False with the given reason and persists status. It is
// used for terminal-until-changed failures (unknown provider, insufficient
// isolation, setup/dispatcher errors) where re-reconcile is driven by a spec
// change or an owned-resource event, not a requeue.
func (r *MCPToolsetReconciler) markNotReady(
	ctx context.Context, toolset *mcpv1alpha1.MCPToolset, reason, message string,
) (ctrl.Result, error) {
	meta.SetStatusCondition(&toolset.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: toolset.Generation,
	})
	return r.updateStatus(ctx, toolset)
}

// updateStatus writes observedGeneration and persists the status subresource.
func (r *MCPToolsetReconciler) updateStatus(ctx context.Context, toolset *mcpv1alpha1.MCPToolset) (ctrl.Result, error) {
	toolset.Status.ObservedGeneration = toolset.Generation
	if err := r.Status().Update(ctx, toolset); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// effectiveIsolation renders an empty isolation level as its default for messages.
func effectiveIsolation(minIsolation string) string {
	if minIsolation == "" {
		return string(mcpv1alpha1.IsolationBaseline)
	}
	return minIsolation
}

// unresolvedMessage renders the unresolved servers into a single condition message.
func unresolvedMessage(unresolved []UnresolvedServer) string {
	parts := make([]string, 0, len(unresolved))
	for _, u := range unresolved {
		parts = append(parts, fmt.Sprintf("%s (%s)", u.Server, u.Reason))
	}
	return "servers not ready: " + strings.Join(parts, ", ")
}

// SetupWithManager wires the controller into the manager. It watches MCPToolset
// resources, the dispatcher resources it owns, and MCPServers so a toolset
// re-reconciles when a referenced server becomes ready.
func (r *MCPToolsetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPToolset{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
			predicate.LabelChangedPredicate{},
		))).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Watches(
			&mcpv1beta1.MCPServer{},
			handler.EnqueueRequestsFromMapFunc(r.toolsetsForServer),
		).
		Named("mcptoolset").
		Complete(r)
}

// toolsetsForServer maps an MCPServer to the toolsets in the same namespace that
// reference it, so a server becoming ready re-triggers those toolsets.
func (r *MCPToolsetReconciler) toolsetsForServer(ctx context.Context, obj client.Object) []reconcile.Request {
	var toolsets mcpv1alpha1.MCPToolsetList
	if err := r.List(ctx, &toolsets, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range toolsets.Items {
		ts := &toolsets.Items[i]
		for _, server := range ts.Spec.Servers {
			if server == obj.GetName() {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{Namespace: ts.Namespace, Name: ts.Name},
				})
				break
			}
		}
	}
	return requests
}
