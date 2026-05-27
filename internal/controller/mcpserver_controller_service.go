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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// reconcileService creates or updates the Service for the MCPServer.
func (r *MCPServerReconciler) reconcileService(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) error {
	logger := log.FromContext(ctx)

	service := r.createService(mcpServer)
	if err := controllerutil.SetControllerReference(mcpServer, service, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for Service")
		return err
	}

	existingService := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, existingService)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating Service", "name", service.Name)
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
	if err := r.validateOwnership(existingService, mcpServer); err != nil {
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

	// Update if ports changed OR if we adopted an orphaned resource
	needsUpdate := !equality.Semantic.DeepEqual(service.Spec.Ports, existingService.Spec.Ports) ||
		existingService.Spec.SessionAffinity != service.Spec.SessionAffinity ||
		serviceLabelsChanged(mcpServer, existingService) ||
		serviceAnnotationsChanged(mcpServer, existingService) ||
		ownershipChanged
	if needsUpdate {
		logger.Info("Updating Service", "name", existingService.Name)
		if err := applyCustomServiceMetadata(mcpServer, existingService); err != nil {
			return fmt.Errorf("applying custom service metadata; %w", err)
		}
		existingService.Spec.Ports = service.Spec.Ports
		existingService.Spec.SessionAffinity = service.Spec.SessionAffinity
		if err := r.Update(ctx, existingService); err != nil {
			logger.Error(err, "Failed to update Service")
			return err
		}
	} else {
		logger.Info("Service already exists and is up to date", "name", service.Name)
	}

	return nil
}

// createService creates a Service for the MCPServer
func (r *MCPServerReconciler) createService(mcpServer *mcpv1alpha1.MCPServer) *corev1.Service {
	labels := managedWorkloadLabels(mcpServer.Name)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: managedWorkloadSelector(mcpServer.Name),
			Ports: []corev1.ServicePort{
				{
					Name:       "mcp",
					Port:       mcpServer.Spec.Config.Port,
					TargetPort: intstr.FromString("mcp"),
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
