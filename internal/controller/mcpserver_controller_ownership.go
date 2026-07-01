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
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

type OwnershipConflictError struct {
	Message string
}

func (e *OwnershipConflictError) Error() string {
	return e.Message
}

func IsOwnershipConflict(err error) bool {
	var target *OwnershipConflictError
	return errors.As(err, &target)
}

// validateOwnership checks if a resource is owned by a different controller.
// Returns an error if the resource has a controller owner that is not the given MCPServer,
// or if the resource has no controller owner (preventing silent adoption of unowned resources).
func (r *MCPServerReconciler) validateOwnership(
	obj client.Object,
	mcpServer *mcpv1alpha1.MCPServer,
) error {
	// Get the controller owner reference from the existing resource
	controllerOwner := metav1.GetControllerOf(obj)
	if controllerOwner == nil {
		// No controller owner - reject to prevent silent adoption
		// User must delete the existing resource or choose a different name for their MCPServer
		return &OwnershipConflictError{Message: fmt.Sprintf("resource %s/%s exists but has no controller owner; "+
			"delete the resource first or choose a different name for the MCPServer",
			obj.GetNamespace(), obj.GetName())}
	}

	// Check if the controller owner is this MCPServer by UID
	if controllerOwner.UID == mcpServer.UID {
		// Owned by this exact MCPServer instance - safe to update
		return nil
	}

	// Check if the owner is an MCPServer with the same name/namespace/group
	// This handles the case where the MCPServer was deleted and recreated
	// with the same name, and we want to adopt the orphaned resources.
	// We validate the API group but allow different versions to support upgrades.
	if isSameGroupKind(controllerOwner, mcpv1alpha1.GroupVersion.Group, mcpv1alpha1.MCPServerKind) &&
		controllerOwner.Name == mcpServer.Name &&
		obj.GetNamespace() == mcpServer.Namespace {
		// Owner is an MCPServer with same group/name/namespace but different UID
		// This means the old MCPServer was deleted and this is a new one
		// Safe to adopt the resources (version may differ during upgrades)
		return nil
	}

	// Resource is owned by a different controller
	return &OwnershipConflictError{Message: fmt.Sprintf("resource %s/%s is owned by %s/%s (UID: %s), cannot be managed by MCPServer %s/%s (UID: %s)",
		obj.GetNamespace(), obj.GetName(),
		controllerOwner.Kind, controllerOwner.Name, controllerOwner.UID,
		mcpServer.Namespace, mcpServer.Name, mcpServer.UID)}
}

// isSameGroupKind checks if an owner reference matches the expected API group and kind,
// ignoring the API version to support cross-version adoption scenarios.
func isSameGroupKind(ownerRef *metav1.OwnerReference, expectedGroup, expectedKind string) bool {
	if ownerRef.Kind != expectedKind {
		return false
	}

	ownerGV, err := schema.ParseGroupVersion(ownerRef.APIVersion)
	if err != nil {
		return false
	}

	return ownerGV.Group == expectedGroup
}
