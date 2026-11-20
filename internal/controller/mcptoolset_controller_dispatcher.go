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
	"errors"
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
// sandbox permissions, the Deployment, and the Service. All are
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

	sa := &corev1.ServiceAccount{Name: name, Namespace: toolset.Namespace}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		sa.Labels = dispatcherLabels(toolset.Name)
		return r.setDispatcherOwner(toolset, sa)
	}); err != nil {
		return nil, fmt.Errorf("service account: %w", err)
	}

	role := &rbacv1.Role{Name: name, Namespace: toolset.Namespace}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.Labels = dispatcherLabels(toolset.Name)
		role.Rules = []rbacv1.PolicyRule{
			{
				// The agent-sandbox SDK creates one SandboxClaim per execution and
				// deletes it on Release.
				APIGroups: []string{"extensions.agents.x-k8s.io"},
				Resources: []string{"sandboxclaims"},
				Verbs:     []string{verbGet, verbList, "watch", "create", "delete"},
			},
			{
				// The SDK reads the materialized Sandbox to learn the backing pod.
				APIGroups: []string{"agents.x-k8s.io"},
				Resources: []string{"sandboxes"},
				Verbs:     []string{verbGet, verbList, "watch"},
			},
			{
				// InClusterPodIP connectivity reaches sandboxd on the sandbox pod's
				// IP, so the dispatcher must read the pod.
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{verbGet, verbList},
			},
		}
		return r.setDispatcherOwner(toolset, role)
	}); err != nil {
		return nil, fmt.Errorf("role: %w", err)
	}

	rb := &rbacv1.RoleBinding{Name: name, Namespace: toolset.Namespace}
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
		return r.setDispatcherOwner(toolset, rb)
	}); err != nil {
		return nil, fmt.Errorf("role binding: %w", err)
	}

	endpointsJSON, err := sandbox.MarshalEndpoints(endpoints)
	if err != nil {
		return nil, fmt.Errorf("encoding endpoints: %w", err)
	}

	deploy := &appsv1.Deployment{Name: name, Namespace: toolset.Namespace}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		mutateDispatcherDeployment(deploy, toolset, name, r.DispatcherImage, r.RunnerImage, providerName, endpointsJSON, timeout)
		return r.setDispatcherOwner(toolset, deploy)
	}); err != nil {
		return nil, fmt.Errorf("deployment: %w", err)
	}

	svc := &corev1.Service{Name: name, Namespace: toolset.Namespace}
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
		return r.setDispatcherOwner(toolset, svc)
	}); err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	return deploy, nil
}

// dispatcherPropagation tears the dispatcher workload down with foreground
// propagation (ReplicaSet and pods included) when execution must be disabled.
const dispatcherPropagation = metav1.DeletePropagationForeground

func (r *MCPToolsetReconciler) setDispatcherOwner(toolset *mcpv1alpha1.MCPToolset, obj client.Object) error {
	if obj.GetResourceVersion() != "" && !metav1.IsControlledBy(obj, toolset) {
		return fmt.Errorf("resource %s is not controlled by MCPToolset %s", client.ObjectKeyFromObject(obj), toolset.Name)
	}
	return controllerutil.SetControllerReference(toolset, obj, r.Scheme)
}

// dispatcherHasCurrentEndpoints permits the existing dispatcher to survive a
// temporary outage when the configured names and all resolved addresses still
// match the allow-list it is serving.
func (r *MCPToolsetReconciler) dispatcherHasCurrentEndpoints(ctx context.Context, toolset *mcpv1alpha1.MCPToolset, resolved []sandbox.Endpoint, unresolved []UnresolvedServer) (bool, error) {
	key := client.ObjectKey{Namespace: toolset.Namespace, Name: dispatcherName(toolset.Name)}
	var deploy appsv1.Deployment
	if err := r.Get(ctx, key, &deploy); err != nil {
		return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(&deploy, toolset) {
		return true, nil
	}
	if !deploy.DeletionTimestamp.IsZero() {
		return false, nil
	}
	currentURLs := make(map[string]string, len(toolset.Spec.Servers))
	for _, server := range unresolved {
		if server.Reason == ResolveReasonNotReady {
			currentURLs[server.Server] = ""
		}
	}
	for _, endpoint := range resolved {
		currentURLs[endpoint.Server] = endpoint.URL
	}
	for _, container := range deploy.Spec.Template.Spec.Containers {
		if container.Name != "dispatcher" {
			continue
		}
		for _, env := range container.Env {
			if env.Name != envDispEndpoints {
				continue
			}
			endpoints, err := sandbox.UnmarshalEndpoints(env.Value)
			if err != nil || len(endpoints) != len(currentURLs) {
				return false, nil
			}
			for _, endpoint := range endpoints {
				url, ok := currentURLs[endpoint.Server]
				if !ok || (url != "" && url != endpoint.URL) {
					return false, nil
				}
				delete(currentURLs, endpoint.Server)
			}
			return len(currentURLs) == 0, nil
		}
	}
	return false, nil
}

// deleteDispatcherWorkload stops the dispatcher from serving by deleting its
// Deployment and Service. It tolerates already-absent resources so it is
// idempotent. The ServiceAccount/Role/RoleBinding are left in place: they grant
// no serving capability on their own and are garbage-collected with the toolset.
func (r *MCPToolsetReconciler) deleteDispatcherWorkload(ctx context.Context, toolset *mcpv1alpha1.MCPToolset) error {
	key := client.ObjectKey{Namespace: toolset.Namespace, Name: dispatcherName(toolset.Name)}
	var errs []error
	for _, obj := range []client.Object{&corev1.Service{}, &appsv1.Deployment{}} {
		if err := r.Get(ctx, key, obj); err != nil {
			if !apierrors.IsNotFound(err) {
				errs = append(errs, fmt.Errorf("getting dispatcher %T: %w", obj, err))
			}
			continue
		}
		if !metav1.IsControlledBy(obj, toolset) || !obj.GetDeletionTimestamp().IsZero() {
			continue
		}
		opts := []client.DeleteOption{client.Preconditions{UID: new(obj.GetUID()), ResourceVersion: new(obj.GetResourceVersion())}}
		if _, ok := obj.(*appsv1.Deployment); ok {
			opts = append(opts, client.PropagationPolicy(dispatcherPropagation))
		}
		if err := r.Delete(ctx, obj, opts...); client.IgnoreNotFound(err) != nil {
			errs = append(errs, fmt.Errorf("deleting dispatcher %T: %w", obj, err))
		}
	}
	return errors.Join(errs...)
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
	if deploy.Status.UpdatedReplicas != desired || deploy.Status.Replicas != desired || deploy.Status.AvailableReplicas != desired {
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
				TCPSocket:           &corev1.TCPSocketAction{Port: intstr.FromString(dispatcherPortName)},
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
