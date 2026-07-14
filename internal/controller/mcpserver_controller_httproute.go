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

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// reconcileGateway gates HTTPRoute reconciliation on Gateway API availability.
func (r *MCPServerReconciler) reconcileGateway(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) error {
	if r.GatewayAPIAvailable {
		return r.reconcileHTTPRoute(ctx, mcpServer)
	}
	if mcpServer.Spec.Gateway != nil {
		return fmt.Errorf("spec.gateway is set but Gateway API CRDs are not installed on this cluster")
	}
	return nil
}

// reconcileHTTPRoute creates, updates, or deletes the HTTPRoute for the MCPServer.
// When spec.gateway is set, an HTTPRoute is created/updated pointing to the
// MCPServer's Service. When spec.gateway is nil, any existing HTTPRoute owned
// by this MCPServer is deleted.
func (r *MCPServerReconciler) reconcileHTTPRoute(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) error {
	logger := log.FromContext(ctx)

	existing := &gatewayv1.HTTPRoute{}
	err := r.Get(ctx, client.ObjectKey{Name: mcpServer.Name, Namespace: mcpServer.Namespace}, existing)

	if mcpServer.Spec.Gateway == nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			logger.Error(err, "Failed to get HTTPRoute")
			return err
		}
		if err := r.validateOwnership(existing, mcpServer); err != nil {
			return nil
		}
		logger.Info("Deleting HTTPRoute (gateway config removed)", "name", existing.Name)
		if err := r.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "Failed to delete HTTPRoute")
			return err
		}
		return nil
	}

	route := r.buildHTTPRoute(mcpServer)
	if err := controllerutil.SetControllerReference(mcpServer, route, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for HTTPRoute")
		return err
	}

	if apierrors.IsNotFound(err) {
		logger.Info("Creating HTTPRoute", "name", route.Name)
		if err := applyCustomHTTPRouteMetadata(mcpServer, route); err != nil {
			return fmt.Errorf("applying custom metadata failed; %w", err)
		}
		if err := r.Create(ctx, route); err != nil {
			logger.Error(err, "Failed to create HTTPRoute")
			return err
		}
		return nil
	}
	if err != nil {
		logger.Error(err, "Failed to get HTTPRoute")
		return err
	}

	// Validate ownership before updating
	if err := r.validateOwnership(existing, mcpServer); err != nil {
		logger.Error(err, "HTTPRoute ownership validation failed")
		return err
	}

	oldOwnerUID := ""
	if oldOwner := metav1.GetControllerOf(existing); oldOwner != nil {
		oldOwnerUID = string(oldOwner.UID)
	}

	if err := controllerutil.SetControllerReference(mcpServer, existing, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for existing HTTPRoute")
		return err
	}

	ownershipChanged := false
	if newOwner := metav1.GetControllerOf(existing); newOwner != nil {
		ownershipChanged = oldOwnerUID != string(newOwner.UID)
	}

	needsUpdate := !equality.Semantic.DeepEqual(route.Spec.ParentRefs, existing.Spec.ParentRefs) ||
		!equality.Semantic.DeepEqual(route.Spec.Hostnames, existing.Spec.Hostnames) ||
		!equality.Semantic.DeepEqual(route.Spec.Rules, existing.Spec.Rules) ||
		httpRouteLabelsChanged(mcpServer, existing) ||
		httpRouteAnnotationsChanged(mcpServer, existing) ||
		ownershipChanged
	if needsUpdate {
		logger.Info("Updating HTTPRoute", "name", existing.Name)
		if err := applyCustomHTTPRouteMetadata(mcpServer, existing); err != nil {
			return fmt.Errorf("applying custom httproute metadata; %w", err)
		}
		existing.Spec.ParentRefs = route.Spec.ParentRefs
		existing.Spec.Hostnames = route.Spec.Hostnames
		existing.Spec.Rules = route.Spec.Rules
		if err := r.Update(ctx, existing); err != nil {
			logger.Error(err, "Failed to update HTTPRoute")
			return err
		}
	} else {
		logger.Info("HTTPRoute already exists and is up to date", "name", route.Name)
	}

	return nil
}

// buildHTTPRoute creates the desired HTTPRoute for the MCPServer.
func (r *MCPServerReconciler) buildHTTPRoute(mcpServer *mcpv1alpha1.MCPServer) *gatewayv1.HTTPRoute {
	labels := managedWorkloadLabels(mcpServer.Name)

	gwNamespace := mcpServer.Spec.Gateway.ParentRef.Namespace
	var parentNs *gatewayv1.Namespace
	if gwNamespace != "" {
		ns := gatewayv1.Namespace(gwNamespace)
		parentNs = &ns
	}

	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = defaultMCPPath
	}

	pathType := gatewayv1.PathMatchPathPrefix
	port := mcpServer.Spec.Config.Port

	gwGroup := gatewayv1.Group(gatewayv1.GroupName)
	gwKind := gatewayv1.Kind("Gateway")
	svcKind := gatewayv1.Kind("Service")
	backendWeight := int32(1)

	var hostnames []gatewayv1.Hostname
	if mcpServer.Spec.Gateway.Hostname != "" {
		hostnames = []gatewayv1.Hostname{gatewayv1.Hostname(mcpServer.Spec.Gateway.Hostname)}
	}

	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
			Labels:    labels,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: hostnames,
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Group:     &gwGroup,
						Kind:      &gwKind,
						Name:      gatewayv1.ObjectName(mcpServer.Spec.Gateway.ParentRef.Name),
						Namespace: parentNs,
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  &pathType,
								Value: &path,
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Kind: &svcKind,
									Name: gatewayv1.ObjectName(mcpServer.Name),
									Port: &port,
								},
								Weight: &backendWeight,
							},
						},
					},
				},
			},
		},
	}
}
