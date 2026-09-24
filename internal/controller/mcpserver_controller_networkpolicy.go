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
	"slices"

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
	_, err := r.ensureNetworkPolicy(ctx, mcpServer)
	return err
}

// ensureNetworkPolicy reconciles the operand NetworkPolicy and returns the
// desired policy it built. Returning the policy lets the caller report the
// posture from the same object instead of rebuilding it via createNetworkPolicy.
func (r *MCPServerReconciler) ensureNetworkPolicy(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
) (*networkingv1.NetworkPolicy, error) {
	logger := log.FromContext(ctx)

	netpol := r.createNetworkPolicy(mcpServer)
	if err := controllerutil.SetControllerReference(mcpServer, netpol, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for NetworkPolicy")
		return nil, err
	}

	existingNetpol := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, client.ObjectKey{Name: netpol.Name, Namespace: netpol.Namespace}, existingNetpol)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating NetworkPolicy", keyName, netpol.Name)
		if err := applyCustomNetworkPolicyMetadata(mcpServer, netpol); err != nil {
			return nil, fmt.Errorf("applying custom metadata failed; %w", err)
		}
		if err := r.Create(ctx, netpol); err != nil {
			logger.Error(err, "Failed to create NetworkPolicy")
			return nil, err
		}
		if mcpServer.Spec.Network == nil || len(mcpServer.Spec.Network.IngressFrom) == 0 {
			logger.Info("NetworkPolicy created without ingress source restrictions", keyName, netpol.Name)
		}
		if mcpServer.Spec.Network == nil || (len(mcpServer.Spec.Network.EgressTo) == 0 && len(mcpServer.Spec.Network.EgressPorts) == 0) {
			logger.Info("NetworkPolicy created without egress destination restrictions", keyName, netpol.Name)
		}
		auditNetworkPolicyCreated(ctx, mcpServer, netpol.Name, hasIngressSourceRestriction(netpol), hasEgressDestinationRestriction(netpol))
		return netpol, nil
	} else if err != nil {
		logger.Error(err, "Failed to get NetworkPolicy")
		return nil, err
	}

	if err := r.validateOwnership(ctx, existingNetpol, mcpServer); err != nil {
		logger.Error(err, "NetworkPolicy ownership validation failed")
		return nil, err
	}

	oldOwnerUID := ""
	if oldOwner := metav1.GetControllerOf(existingNetpol); oldOwner != nil {
		oldOwnerUID = string(oldOwner.UID)
	}

	if err := controllerutil.SetControllerReference(mcpServer, existingNetpol, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for existing NetworkPolicy")
		return nil, err
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
			return nil, fmt.Errorf("applying custom networkpolicy metadata; %w", err)
		}
		existingNetpol.Spec = netpol.Spec
		if err := r.Update(ctx, existingNetpol); err != nil {
			logger.Error(err, "Failed to update NetworkPolicy")
			return nil, err
		}
		auditNetworkPolicyUpdated(ctx, mcpServer, existingNetpol.Name)
	} else {
		logger.Info("NetworkPolicy already exists and is up to date", keyName, netpol.Name)
	}

	return netpol, nil
}

func (r *MCPServerReconciler) createNetworkPolicy(mcpServer *mcpv1beta1.MCPServer) *networkingv1.NetworkPolicy {
	labels := managedWorkloadLabels(mcpServer.Name)

	ingressRules := defaultIngressRules(mcpServer, r.NetworkPolicyIngressPosture)

	egressRules, manageEgress := defaultEgressRules(mcpServer, r.NetworkPolicyEgressPosture)

	// Ingress is always managed (deny-by-default is expressed as an empty ingress
	// rule set with Ingress still in policyTypes). Egress is only listed when it is
	// managed; a restricted, unconfigured egress leaves the dimension unmanaged
	// rather than allow-all or deny-all.
	policyTypes := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	if manageEgress {
		policyTypes = append(policyTypes, networkingv1.PolicyTypeEgress)
	}

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
			PolicyTypes: policyTypes,
			Ingress:     ingressRules,
			Egress:      egressRules,
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
	return r.networkPolicyPostureConditionFor(
		mcpServer, generation, existingConditions, r.createNetworkPolicy(mcpServer))
}

// networkPolicyPostureConditionFor derives the posture condition from an
// already-built NetworkPolicy, so a caller that just reconciled the policy can
// reuse that object instead of rebuilding it via createNetworkPolicy.
func (r *MCPServerReconciler) networkPolicyPostureConditionFor(
	mcpServer *mcpv1beta1.MCPServer,
	generation int64,
	existingConditions []metav1.Condition,
	netpol *networkingv1.NetworkPolicy,
) metav1.Condition {
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
		if egressDestinationsRestricted(mcpServer) {
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
	// An empty ingress rule set with Ingress declared in policyTypes denies all
	// ingress - the most restrictive posture - so it counts as restricted. Without
	// this the deny-by-default policy would be misreported as source-unrestricted.
	if len(netpol.Spec.Ingress) == 0 && slices.Contains(netpol.Spec.PolicyTypes, networkingv1.PolicyTypeIngress) {
		return true
	}
	for _, rule := range netpol.Spec.Ingress {
		// A rule restricts ingress only when it constrains both the source and the
		// destination port. A rule that names a source but leaves ports empty still
		// admits that source on every port, so it is not a genuine restriction.
		if len(rule.Ports) == 0 {
			continue
		}
		// Peers within a rule are OR'ed, so the rule restricts sources only when it
		// names at least one peer and every peer narrows the source set. A single
		// admit-all peer (e.g. 0.0.0.0/0 listed alongside a podSelector) opens the
		// rule to every source, so it must not be reported as restricted.
		if len(rule.From) > 0 && !slices.ContainsFunc(rule.From, peerAdmitsAllSources) {
			return true
		}
	}
	return false
}

// peerRestrictsSource reports whether an ingress peer actually narrows the set of
// allowed sources. Patterns that match every source - an empty namespaceSelector
// (all namespaces), or a universal CIDR with no exceptions - do not count, so
// they are not misreported as a restriction.
func peerRestrictsSource(peer networkingv1.NetworkPolicyPeer) bool {
	if peer.IPBlock != nil {
		return len(peer.IPBlock.Except) > 0 || !isUniversalCIDR(peer.IPBlock.CIDR)
	}
	// A podSelector with match criteria narrows sources regardless of namespace.
	if peer.PodSelector != nil && !isEmptyLabelSelector(peer.PodSelector) {
		return true
	}
	// A namespaceSelector narrows sources only when it selects a subset of
	// namespaces; an empty selector matches all namespaces.
	if peer.NamespaceSelector != nil && !isEmptyLabelSelector(peer.NamespaceSelector) {
		return true
	}
	// An empty podSelector with no namespaceSelector restricts to the policy's own
	// namespace, which is a genuine (if broad) restriction.
	return peer.PodSelector != nil && peer.NamespaceSelector == nil
}

// peerAdmitsAllSources reports whether an ingress peer admits every source. It is
// the inverse of peerRestrictsSource and is used to detect an admit-all peer OR'ed
// into an otherwise restrictive rule, which opens the rule to all sources.
func peerAdmitsAllSources(peer networkingv1.NetworkPolicyPeer) bool {
	return !peerRestrictsSource(peer)
}

func isEmptyLabelSelector(selector *metav1.LabelSelector) bool {
	return selector != nil && len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0
}

func isUniversalCIDR(cidr string) bool {
	return cidr == "0.0.0.0/0" || cidr == "::/0"
}

func hasEgressDestinationRestriction(netpol *networkingv1.NetworkPolicy) bool {
	for _, rule := range netpol.Spec.Egress {
		if len(rule.To) > 0 || len(rule.Ports) > 0 {
			return true
		}
	}
	return false
}

// egressDestinationsRestricted reports whether the user restricted the set of
// application egress destinations, i.e. Spec.Network.EgressTo is non-empty. It is
// deliberately narrower than hasEgressDestinationRestriction (which also treats a
// ports-only rule as restricted) and it intentionally ignores the operator's own
// DNS egress carve-out: DNSEgressPeer gives the DNS rule a To, which must not make
// a ports-only application egress read as restricting destinations. It is used
// only to phrase the posture message accurately.
func egressDestinationsRestricted(mcpServer *mcpv1beta1.MCPServer) bool {
	return mcpServer.Spec.Network != nil && len(mcpServer.Spec.Network.EgressTo) > 0
}
