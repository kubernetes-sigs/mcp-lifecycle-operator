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
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

const (
	// dispatcherPort is the container/Service port the execute_code dispatcher
	// listens on (matches the dispatcher's default listen address).
	dispatcherPort = 8080
	// dispatcherPortName names the port so the Service can target it by name.
	dispatcherPortName = "mcp"
	// dispatcherMCPPath is the HTTP path the dispatcher serves MCP on.
	dispatcherMCPPath = "/mcp"
	// dispatcherComponent labels the dispatcher resources.
	dispatcherComponent = "code-mode-dispatcher"
	// verbGet and verbList are RBAC verbs used across the dispatcher Role rules.
	verbGet  = "get"
	verbList = "list"
)

// Dispatcher configuration wire contract. These env var names MUST match the
// ones the dispatcher reads in cmd/execute-code/main.go.
const (
	envDispToolset        = "MCPTOOLSET_NAME"
	envDispNamespace      = "MCPTOOLSET_NAMESPACE"
	envDispProvider       = "MCPTOOLSET_PROVIDER"
	envDispRunnerImage    = "MCPTOOLSET_RUNNER_IMAGE"
	envDispEndpoints      = "MCPTOOLSET_ENDPOINTS"
	envDispCredSecret     = "MCPTOOLSET_CREDENTIAL_SECRET"
	envDispCredKey        = "MCPTOOLSET_CREDENTIAL_KEY"
	envDispDefaultTimeout = "MCPTOOLSET_DEFAULT_TIMEOUT_SECONDS"
	envDispListenAddr     = "MCPTOOLSET_LISTEN_ADDR"
)

// dispatcherName is the name shared by the dispatcher's Deployment, Service,
// ServiceAccount, Role, and RoleBinding for a toolset.
func dispatcherName(toolset string) string {
	return toolset + "-execute"
}

// dispatcherEndpointURL is the in-cluster URL callers submit code to.
func dispatcherEndpointURL(toolset *mcpv1alpha1.MCPToolset) string {
	return fmt.Sprintf("http://%s.%s.svc:%d%s",
		dispatcherName(toolset.Name), toolset.Namespace, dispatcherPort, dispatcherMCPPath)
}

// effectiveTimeout resolves the default per-execution timeout for a toolset.
func effectiveTimeout(toolset *mcpv1alpha1.MCPToolset) time.Duration {
	seconds := int32(120)
	if toolset.Spec.TimeoutSeconds != nil {
		seconds = *toolset.Spec.TimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func dispatcherLabels(toolset string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "mcp-lifecycle-operator",
		"app.kubernetes.io/component":  dispatcherComponent,
		"mcp.x-k8s.io/toolset":         toolset,
	}
}

func dispatcherSelector(toolset string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/component": dispatcherComponent,
		"mcp.x-k8s.io/toolset":        toolset,
	}
}

// reconcileDispatcher creates or updates every resource the dispatcher needs:
// a ServiceAccount, a namespace-scoped Role + RoleBinding granting the call-time
// sandbox permissions (sandboxclaims create/delete, sandboxes + pods read, and the
// credential secret read), the Deployment, and the Service. All are
// owner-referenced to the toolset for garbage collection.
func (r *MCPToolsetReconciler) reconcileDispatcher(
	ctx context.Context,
	toolset *mcpv1alpha1.MCPToolset,
	providerName string,
	endpoints []sandbox.Endpoint,
	timeout time.Duration,
) (*appsv1.Deployment, error) {
	if r.DispatcherImage == "" {
		return nil, fmt.Errorf("dispatcher image is not configured")
	}

	name := dispatcherName(toolset.Name)

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: toolset.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		sa.Labels = dispatcherLabels(toolset.Name)
		return controllerutil.SetControllerReference(toolset, sa, r.Scheme)
	}); err != nil {
		return nil, fmt.Errorf("service account: %w", err)
	}

	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: toolset.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.Labels = dispatcherLabels(toolset.Name)
		role.Rules = []rbacv1.PolicyRule{
			{
				// The agent-sandbox SDK creates one SandboxClaim per execution and
				// deletes it on Release.
				APIGroups: []string{"extensions.agents.x-k8s.io"},
				Resources: []string{"sandboxclaims"},
				Verbs:     []string{verbGet, verbList, "create", "delete"},
			},
			{
				// The SDK reads the materialized Sandbox to learn the backing pod.
				APIGroups: []string{"agents.x-k8s.io"},
				Resources: []string{"sandboxes"},
				Verbs:     []string{verbGet, verbList},
			},
			{
				// InClusterPodIP connectivity reaches sandboxd on the sandbox pod's
				// IP, so the dispatcher must read the pod.
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{verbGet, verbList},
			},
			{
				// The static scoped credential (MVP) is read from its Secret at
				// call time and written into the sandbox.
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{verbGet},
			},
		}
		return controllerutil.SetControllerReference(toolset, role, r.Scheme)
	}); err != nil {
		return nil, fmt.Errorf("role: %w", err)
	}

	rb := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: toolset.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, rb, func() error {
		rb.Labels = dispatcherLabels(toolset.Name)
		rb.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     name,
		}
		rb.Subjects = []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      name,
			Namespace: toolset.Namespace,
		}}
		return controllerutil.SetControllerReference(toolset, rb, r.Scheme)
	}); err != nil {
		return nil, fmt.Errorf("role binding: %w", err)
	}

	endpointsJSON, err := sandbox.MarshalEndpoints(endpoints)
	if err != nil {
		return nil, fmt.Errorf("encoding endpoints: %w", err)
	}

	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: toolset.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		mutateDispatcherDeployment(deploy, toolset, name, r.DispatcherImage, r.RunnerImage, providerName, endpointsJSON, timeout)
		return controllerutil.SetControllerReference(toolset, deploy, r.Scheme)
	}); err != nil {
		return nil, fmt.Errorf("deployment: %w", err)
	}

	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: toolset.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = dispatcherLabels(toolset.Name)
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = dispatcherSelector(toolset.Name)
		svc.Spec.Ports = []corev1.ServicePort{{
			Name:       dispatcherPortName,
			Port:       dispatcherPort,
			TargetPort: intstr.FromString(dispatcherPortName),
			Protocol:   corev1.ProtocolTCP,
		}}
		return controllerutil.SetControllerReference(toolset, svc, r.Scheme)
	}); err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	return deploy, nil
}

// dispatcherPropagation tears the dispatcher workload down with foreground
// propagation (ReplicaSet and pods included) when execution must be disabled.
var dispatcherPropagation = metav1.DeletePropagationForeground

// deleteDispatcherWorkload stops the dispatcher from serving by deleting its
// Deployment and Service. It tolerates already-absent resources so it is
// idempotent. The ServiceAccount/Role/RoleBinding are left in place: they grant
// no serving capability on their own and are garbage-collected with the toolset.
func (r *MCPToolsetReconciler) deleteDispatcherWorkload(ctx context.Context, toolset *mcpv1alpha1.MCPToolset) error {
	name := dispatcherName(toolset.Name)
	opts := []client.DeleteOption{client.PropagationPolicy(dispatcherPropagation)}

	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: toolset.Namespace}}
	if err := r.Delete(ctx, deploy, opts...); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting dispatcher Deployment: %w", err)
	}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: toolset.Namespace}}
	if err := r.Delete(ctx, svc, opts...); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting dispatcher Service: %w", err)
	}
	return nil
}

// dispatcherAvailable reports whether the dispatcher Deployment is Available for
// its current generation: the Deployment controller has observed the latest spec,
// every desired replica is available, and the Available condition is True. This
// is what lets Ready mean "executions can actually run" rather than merely
// "resources were created".
func dispatcherAvailable(deploy *appsv1.Deployment) bool {
	if deploy == nil {
		return false
	}
	if deploy.Status.ObservedGeneration < deploy.Generation {
		return false
	}
	desired := int32(1)
	if deploy.Spec.Replicas != nil {
		desired = *deploy.Spec.Replicas
	}
	if deploy.Status.AvailableReplicas < desired {
		return false
	}
	for _, c := range deploy.Status.Conditions {
		if c.Type == appsv1.DeploymentAvailable {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// mutateDispatcherDeployment sets the desired dispatcher Deployment spec in place.
func mutateDispatcherDeployment(
	deploy *appsv1.Deployment,
	toolset *mcpv1alpha1.MCPToolset,
	name, dispatcherImage, runnerImage, providerName, endpointsJSON string,
	timeout time.Duration,
) {
	labels := dispatcherLabels(toolset.Name)
	selector := dispatcherSelector(toolset.Name)
	replicas := int32(1)

	deploy.Labels = labels
	deploy.Spec.Replicas = &replicas
	deploy.Spec.Selector = &metav1.LabelSelector{MatchLabels: selector}
	deploy.Spec.Template.Labels = labels
	deploy.Spec.Template.Spec = corev1.PodSpec{
		ServiceAccountName: name,
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   new(true),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		Containers: []corev1.Container{{
			Name:  "dispatcher",
			Image: dispatcherImage,
			Ports: []corev1.ContainerPort{{
				Name:          dispatcherPortName,
				ContainerPort: dispatcherPort,
				Protocol:      corev1.ProtocolTCP,
			}},
			// A readiness probe so the Deployment reports Available only once the
			// dispatcher is actually listening; a crash-looping or not-yet-serving
			// pod then keeps the toolset out of Ready (gated on availability).
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString(dispatcherPortName)},
				},
				InitialDelaySeconds: 2,
				PeriodSeconds:       10,
			},
			Env: []corev1.EnvVar{
				{Name: envDispToolset, Value: toolset.Name},
				{Name: envDispNamespace, Value: toolset.Namespace},
				{Name: envDispProvider, Value: providerName},
				{Name: envDispRunnerImage, Value: runnerImage},
				{Name: envDispEndpoints, Value: endpointsJSON},
				{Name: envDispDefaultTimeout, Value: strconv.FormatInt(int64(timeout.Seconds()), 10)},
				{Name: envDispListenAddr, Value: fmt.Sprintf(":%d", dispatcherPort)},
			},
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: new(false),
				ReadOnlyRootFilesystem:   new(true),
				RunAsNonRoot:             new(true),
				Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
		}},
	}
}
