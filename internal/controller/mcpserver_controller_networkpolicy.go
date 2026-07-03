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
	"maps"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

func (r *MCPServerReconciler) reconcileNetworkPolicy(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) error {
	logger := log.FromContext(ctx)

	netpol := r.createNetworkPolicy(mcpServer)
	if err := controllerutil.SetControllerReference(mcpServer, netpol, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for NetworkPolicy")
		return err
	}

	existingNetpol := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, client.ObjectKey{Name: netpol.Name, Namespace: netpol.Namespace}, existingNetpol)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating NetworkPolicy", "name", netpol.Name)
		if err := applyCustomNetworkPolicyMetadata(mcpServer, netpol); err != nil {
			return fmt.Errorf("applying custom metadata failed; %w", err)
		}
		if err := r.Create(ctx, netpol); err != nil {
			logger.Error(err, "Failed to create NetworkPolicy")
			return err
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get NetworkPolicy")
		return err
	}

	if err := r.validateOwnership(existingNetpol, mcpServer); err != nil {
		logger.Error(err, "NetworkPolicy ownership validation failed")
		return err
	}

	oldOwnerUID := ""
	if oldOwner := metav1.GetControllerOf(existingNetpol); oldOwner != nil {
		oldOwnerUID = string(oldOwner.UID)
	}

	if err := controllerutil.SetControllerReference(mcpServer, existingNetpol, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for existing NetworkPolicy")
		return err
	}

	ownershipChanged := false
	if newOwner := metav1.GetControllerOf(existingNetpol); newOwner != nil {
		ownershipChanged = oldOwnerUID != string(newOwner.UID)
	}

	needsUpdate := !equality.Semantic.DeepEqual(netpol.Spec, existingNetpol.Spec) ||
		networkPolicyLabelsChanged(mcpServer, existingNetpol) ||
		networkPolicyAnnotationsChanged(mcpServer, existingNetpol) ||
		ownershipChanged
	if needsUpdate {
		logger.Info("Updating NetworkPolicy", "name", existingNetpol.Name)
		if existingNetpol.Labels == nil {
			existingNetpol.Labels = make(map[string]string)
		}
		maps.Copy(existingNetpol.Labels, netpol.Labels)
		if err := applyCustomNetworkPolicyMetadata(mcpServer, existingNetpol); err != nil {
			return fmt.Errorf("applying custom networkpolicy metadata; %w", err)
		}
		existingNetpol.Spec = netpol.Spec
		if err := r.Update(ctx, existingNetpol); err != nil {
			logger.Error(err, "Failed to update NetworkPolicy")
			return err
		}
	} else {
		logger.Info("NetworkPolicy already exists and is up to date", "name", netpol.Name)
	}

	return nil
}

func (r *MCPServerReconciler) createNetworkPolicy(mcpServer *mcpv1alpha1.MCPServer) *networkingv1.NetworkPolicy {
	labels := managedWorkloadLabels(mcpServer.Name)
	port := intstr.FromInt32(mcpServer.Spec.Config.Port)
	protocol := corev1.ProtocolTCP

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: managedWorkloadSelector(mcpServer.Name),
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Port:     &port,
							Protocol: &protocol,
						},
					},
				},
			},
		},
	}
}
