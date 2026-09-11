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

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func (r *MCPServerReconciler) reconcileNetworkPolicy(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
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
		logger.Info("Creating NetworkPolicy", keyName, netpol.Name)
		if err := applyCustomNetworkPolicyMetadata(mcpServer, netpol); err != nil {
			return fmt.Errorf("applying custom metadata failed; %w", err)
		}
		if err := r.Create(ctx, netpol); err != nil {
			logger.Error(err, "Failed to create NetworkPolicy")
			return err
		}
		if mcpServer.Spec.Network == nil || len(mcpServer.Spec.Network.IngressFrom) == 0 {
			logger.Info("NetworkPolicy created without ingress source restrictions", keyName, netpol.Name)
		}
		if mcpServer.Spec.Network == nil || (len(mcpServer.Spec.Network.EgressTo) == 0 && len(mcpServer.Spec.Network.EgressPorts) == 0) {
			logger.Info("NetworkPolicy created without egress destination restrictions", keyName, netpol.Name)
		}
		auditNetworkPolicyCreated(ctx, mcpServer, netpol.Name, hasIngressSourceRestriction(netpol), hasEgressDestinationRestriction(netpol))
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to get NetworkPolicy")
		return err
	}

	if err := r.validateOwnership(ctx, existingNetpol, mcpServer); err != nil {
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
		logger.Info("Updating NetworkPolicy", keyName, existingNetpol.Name)
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
		auditNetworkPolicyUpdated(ctx, mcpServer, existingNetpol.Name)
	} else {
		logger.Info("NetworkPolicy already exists and is up to date", keyName, netpol.Name)
	}

	return nil
}

func (r *MCPServerReconciler) createNetworkPolicy(mcpServer *mcpv1beta1.MCPServer) *networkingv1.NetworkPolicy {
	labels := managedWorkloadLabels(mcpServer.Name)
	port := intstr.FromInt32(mcpServer.Spec.Config.Port)
	protocol := corev1.ProtocolTCP

	ingressRule := networkingv1.NetworkPolicyIngressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{
				Port:     &port,
				Protocol: &protocol,
			},
		},
	}
	if mcpServer.Spec.Network != nil && len(mcpServer.Spec.Network.IngressFrom) > 0 {
		ingressRule.From = mcpServer.Spec.Network.DeepCopy().IngressFrom
	}

	egressRules := buildEgressRules(mcpServer)

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
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				ingressRule,
			},
			Egress: egressRules,
		},
	}
}

func buildEgressRules(mcpServer *mcpv1beta1.MCPServer) []networkingv1.NetworkPolicyEgressRule {
	hasEgressTo := mcpServer.Spec.Network != nil && len(mcpServer.Spec.Network.EgressTo) > 0
	hasEgressPorts := mcpServer.Spec.Network != nil && len(mcpServer.Spec.Network.EgressPorts) > 0

	if !hasEgressTo && !hasEgressPorts {
		return []networkingv1.NetworkPolicyEgressRule{{}}
	}

	dnsPort := intstr.FromInt32(53)
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	dnsRule := networkingv1.NetworkPolicyEgressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Port: &dnsPort, Protocol: &udp},
			{Port: &dnsPort, Protocol: &tcp},
		},
	}
	if mcpServer.Spec.Network.DNSEgressPeer != nil {
		dnsRule.To = []networkingv1.NetworkPolicyPeer{*mcpServer.Spec.Network.DNSEgressPeer.DeepCopy()}
	}

	userRule := networkingv1.NetworkPolicyEgressRule{}
	if hasEgressTo {
		userRule.To = mcpServer.Spec.Network.DeepCopy().EgressTo
	}
	if hasEgressPorts {
		userRule.Ports = mcpServer.Spec.Network.DeepCopy().EgressPorts
	}

	return []networkingv1.NetworkPolicyEgressRule{dnsRule, userRule}
}

// networkPolicyPostureCondition reports whether the operator-managed
// NetworkPolicy restricts ingress sources and egress destinations. It derives
// the posture from the desired policy built by createNetworkPolicy, using the
// same restriction-detection helpers as the audit signal so the two never
// disagree. The condition is informational and never gates readiness.
func (r *MCPServerReconciler) networkPolicyPostureCondition(
	mcpServer *mcpv1beta1.MCPServer,
	generation int64,
	existingConditions []metav1.Condition,
) metav1.Condition {
	netpol := r.createNetworkPolicy(mcpServer)
	ingressRestricted := hasIngressSourceRestriction(netpol)
	egressRestricted := hasEgressDestinationRestriction(netpol)

	var (
		status  metav1.ConditionStatus
		reason  string
		message string
	)
	switch {
	case ingressRestricted && egressRestricted:
		status = metav1.ConditionTrue
		reason = ReasonNetworkPolicyRestricted
		// Egress counts as restricted when a rule constrains either destinations
		// or ports. Phrase the message for what is actually constrained so a
		// ports-only egress (any destination reachable on those ports) is not
		// reported as restricting destinations.
		if egressDestinationsRestricted(netpol) {
			message = "NetworkPolicy restricts both ingress sources and egress destinations"
		} else {
			message = "NetworkPolicy restricts ingress sources and limits egress to specific ports"
		}
	case ingressRestricted && !egressRestricted:
		status = metav1.ConditionFalse
		reason = ReasonNetworkPolicyEgressUnrestricted
		message = "NetworkPolicy allows egress to any destination"
	case !ingressRestricted && egressRestricted:
		status = metav1.ConditionFalse
		reason = ReasonNetworkPolicyIngressUnrestricted
		message = "NetworkPolicy allows ingress from any source"
	default:
		status = metav1.ConditionFalse
		reason = ReasonNetworkPolicyUnrestricted
		message = "NetworkPolicy allows ingress from any source and egress to any destination"
	}

	c := newCondition(ConditionTypeNetworkPolicyRestricted, status, reason, message, generation)
	preserveLastTransitionTime(&c, existingConditions)
	return c
}

func hasIngressSourceRestriction(netpol *networkingv1.NetworkPolicy) bool {
	for _, rule := range netpol.Spec.Ingress {
		for _, peer := range rule.From {
			if peer.PodSelector != nil || peer.NamespaceSelector != nil || peer.IPBlock != nil {
				return true
			}
		}
	}
	return false
}

func hasEgressDestinationRestriction(netpol *networkingv1.NetworkPolicy) bool {
	for _, rule := range netpol.Spec.Egress {
		if len(rule.To) > 0 || len(rule.Ports) > 0 {
			return true
		}
	}
	return false
}

// egressDestinationsRestricted reports whether any egress rule limits the set of
// destination peers (a non-empty To). This is narrower than
// hasEgressDestinationRestriction, which also treats a ports-only rule as
// restricted; it is used only to phrase the posture message accurately.
func egressDestinationsRestricted(netpol *networkingv1.NetworkPolicy) bool {
	for _, rule := range netpol.Spec.Egress {
		if len(rule.To) > 0 {
			return true
		}
	}
	return false
}
