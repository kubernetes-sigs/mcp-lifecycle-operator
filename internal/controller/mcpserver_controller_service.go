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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

// reconcileService creates or updates the Service for the MCPServer.
// selector is the label selector the Service uses to target pods.
func (r *MCPServerReconciler) reconcileService(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
	selector map[string]string,
) error {
	logger := log.FromContext(ctx)

	service := r.createService(mcpServer, selector)
	if err := controllerutil.SetControllerReference(mcpServer, service, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for Service")
		return err
	}

	existingService := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, existingService)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating Service", keyName, service.Name)
		if err := applyCustomServiceMetadata(mcpServer, service); err != nil {
			return fmt.Errorf("applying custom metadata failed; %w", err)
		}
		if err := r.Create(ctx, service); err != nil {
			logger.Error(err, "Failed to create Service")
			return err
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get Service")
		return err
	}

	// Validate ownership before updating
	if err := r.validateOwnership(ctx, existingService, mcpServer); err != nil {
		logger.Error(err, "Service ownership validation failed")
		return err
	}

	// Check if we need to adopt an orphaned resource by comparing owner UIDs before updating
	oldOwnerUID := ""
	if oldOwner := metav1.GetControllerOf(existingService); oldOwner != nil {
		oldOwnerUID = string(oldOwner.UID)
	}

	// Update ownerReferences to establish/refresh controller ownership.
	// This is safe because validateOwnership has confirmed we can manage this resource.
	// For orphaned resources, this adopts them by updating the stale UID.
	if err := controllerutil.SetControllerReference(mcpServer, existingService, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for existing Service")
		return err
	}

	// Check if we actually adopted an orphaned resource (owner UID changed)
	ownershipChanged := false
	if newOwner := metav1.GetControllerOf(existingService); newOwner != nil {
		ownershipChanged = oldOwnerUID != string(newOwner.UID)
	}

	// Update if the desired Service fields changed OR if we adopted an orphaned resource.
	needsUpdate := !equality.Semantic.DeepEqual(service.Spec.Ports, existingService.Spec.Ports) ||
		!equality.Semantic.DeepEqual(service.Spec.Selector, existingService.Spec.Selector) ||
		existingService.Spec.SessionAffinity != service.Spec.SessionAffinity ||
		serviceLabelsChanged(mcpServer, existingService) ||
		serviceAnnotationsChanged(mcpServer, existingService) ||
		ownershipChanged
	if needsUpdate {
		logger.Info("Updating Service", keyName, existingService.Name)
		if err := applyCustomServiceMetadata(mcpServer, existingService); err != nil {
			return fmt.Errorf("applying custom service metadata; %w", err)
		}
		existingService.Spec.Ports = service.Spec.Ports
		existingService.Spec.Selector = service.Spec.Selector
		existingService.Spec.SessionAffinity = service.Spec.SessionAffinity
		if err := r.Update(ctx, existingService); err != nil {
			logger.Error(err, "Failed to update Service")
			return err
		}
	} else {
		logger.Info("Service already exists and is up to date", keyName, service.Name)
	}

	return nil
}

// createService creates a Service for the MCPServer using the given selector.
func (r *MCPServerReconciler) createService(mcpServer *mcpv1beta1.MCPServer, selector map[string]string) *corev1.Service {
	labels := managedWorkloadLabels(mcpServer.Name)
	targetPort := intstr.FromString(mcpPortName)
	if mcpServer.Spec.WorkloadRef != nil {
		// External containers need not declare an operator-specific named port.
		targetPort = intstr.FromInt32(mcpServer.Spec.Config.Port)
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selector,
			Ports: []corev1.ServicePort{
				{
					Name:       mcpPortName,
					Port:       mcpServer.Spec.Config.Port,
					TargetPort: targetPort,
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if mcpServer.Spec.MCP.Stateless != nil && *mcpServer.Spec.MCP.Stateless {
		service.Spec.SessionAffinity = corev1.ServiceAffinityNone
	} else {
		service.Spec.SessionAffinity = corev1.ServiceAffinityClientIP
	}

	return service
}

// ServiceEndpoint identifies the Service and exposed port used for MCP traffic.
// Resolution preserves Name on error so callers can record the dependency.
type ServiceEndpoint struct {
	Name string
	Port int32
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
func ResolveServiceEndpoint(ctx context.Context, reader client.Reader, server *mcpv1beta1.MCPServer) (ServiceEndpoint, error) {
	if server.Spec.ServiceRef == nil {
		return ServiceEndpoint{Name: serviceNameFor(server), Port: server.Spec.Config.Port}, nil
	}
	name := serviceNameFor(server)
	configPort := server.Spec.Config.Port
	if server.Spec.WorkloadRef == nil {
		// For managed workloads config.port describes the container, which may
		// differ from the referenced Service's exposed port.
		configPort = 0
	}
	port, err := resolveServicePort(ctx, reader, name, server.Namespace, configPort)
	return ServiceEndpoint{Name: name, Port: port}, err
}

// reconcileServiceOrBYO either reconciles the operator-managed Service or
// resolves the BYO Service port. Service identity comes from serviceNameFor.
func (r *MCPServerReconciler) reconcileServiceOrBYO(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
	selector map[string]string,
) (ServiceEndpoint, error) {
	if mcpServer.Spec.ServiceRef != nil {
		endpoint, err := ResolveServiceEndpoint(ctx, r.APIReader, mcpServer)
		if err != nil {
			return endpoint, err
		}
		// A serviceRef may be added to an MCPServer that already has an
		// operator-managed Service. Endpoint resolution above has switched to the
		// external Service, so the previously managed Service must be cleaned up or
		// it lingers unreconciled and goes stale. Cleanup is guarded so the
		// operator never deletes a Service it does not own or still relies on.
		if err := r.cleanupManagedServiceForBYO(ctx, mcpServer); err != nil {
			return endpoint, err
		}
		return endpoint, nil
	}

	serviceStart := time.Now()
	if err := r.reconcileService(ctx, mcpServer, selector); err != nil {
		reconcileDuration.With(prometheus.Labels{keyPhase: ReconcilePhaseService}).Observe(time.Since(serviceStart).Seconds())
		return ServiceEndpoint{}, err
	}
	reconcileDuration.With(prometheus.Labels{keyPhase: ReconcilePhaseService}).Observe(time.Since(serviceStart).Seconds())
	return ServiceEndpoint{Name: serviceNameFor(mcpServer), Port: mcpServer.Spec.Config.Port}, nil
}

// cleanupManagedServiceForBYO deletes a previously operator-managed Service after
// an external serviceRef is added to the MCPServer. serviceRef is not immutable, so
// a user may point an MCPServer at an external Service while an operator-managed
// Service (named after the MCPServer) still exists. Once endpoint resolution
// switches to the external Service the managed Service is no longer reconciled and
// goes stale (a later container port change leaves it pointing at an obsolete
// port). This performs a safe cleanup guarded so the operator never removes a
// Service it does not own or still relies on:
//
//  1. The external serviceRef Service is resolved and must exist before anything is
//     deleted. A missing reference fails safe and leaves the managed Service in
//     place, surfaced through the normal Service-failure status/event path.
//  2. The managed Service is deleted only when its controller owner reference UID
//     matches this MCPServer's UID. Services owned by anything else, or with no
//     controller owner (user-owned), are preserved.
//  3. A self-referencing serviceRef (serviceRef.name == MCPServer name) points at
//     the very Service the operator would manage, so it is preserved.
//  4. Deletion uses UID + resourceVersion preconditions so a Service replaced
//     between the read and the delete is not removed.
//
// The function is idempotent: when no managed Service exists there is nothing to do.
func (r *MCPServerReconciler) cleanupManagedServiceForBYO(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
) error {
	logger := log.FromContext(ctx)

	// Guard 3: a self-referencing serviceRef names the operator-managed Service
	// itself; the external and managed Service are the same object, never delete it.
	if mcpServer.Spec.ServiceRef != nil && mcpServer.Spec.ServiceRef.Name == mcpServer.Name {
		return nil
	}

	// The operator-managed Service is always named after the MCPServer. Look it up
	// first so the steady state (serviceRef already set, nothing to clean up) and a
	// not-yet-created external Service never hit the external resolution below.
	managedSvc := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, managedSvc); err != nil {
		if apierrors.IsNotFound(err) {
			// Nothing to clean up: the managed Service never existed or is already gone.
			return nil
		}
		return err
	}

	// Guard 2: only delete a Service this exact MCPServer controls. Compare the
	// controller owner reference UID; preserve Services owned by anything else or
	// with no controller owner (user-owned).
	owner := metav1.GetControllerOf(managedSvc)
	if owner == nil || owner.UID != mcpServer.UID {
		logger.Info("Preserving Service not owned by this MCPServer after serviceRef was added",
			keyName, managedSvc.Name, keyNamespace, managedSvc.Namespace)
		return nil
	}

	// Guard 1: resolve and validate the external Service before deleting the owned
	// managed Service. A missing reference fails safe and leaves the managed Service
	// in place, surfaced through the normal Service-failure status/event path.
	externalName := serviceNameFor(mcpServer)
	externalSvc := &corev1.Service{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: externalName, Namespace: mcpServer.Namespace}, externalSvc); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("referenced Service %s not found", externalName)
		}
		return err
	}

	// Guard 4: UID + resourceVersion deletion preconditions so a Service that was
	// replaced between the read above and the delete is not removed.
	preconditions := client.Preconditions{
		UID:             &managedSvc.UID,
		ResourceVersion: &managedSvc.ResourceVersion,
	}
	if err := r.Delete(ctx, managedSvc, preconditions); err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
			// Already deleted or replaced concurrently; nothing further to do.
			return nil
		}
		logger.Error(err, "Failed to delete stale managed Service after serviceRef was added",
			keyName, managedSvc.Name)
		return err
	}

	logger.Info("Deleted stale operator-managed Service after serviceRef was added",
		keyName, managedSvc.Name, keyNamespace, managedSvc.Namespace)
	return nil
}
