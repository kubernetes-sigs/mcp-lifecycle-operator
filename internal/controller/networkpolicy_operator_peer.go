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
	"maps"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

// operatorIngressPeers returns the ingress peer that keeps the operator's own
// controller pod able to reach an operand under the restricted default ingress
// posture, which would otherwise deny all ingress when the server declares no
// source of its own.
//
// The operator connects to the operand pod to run its MCP verification handshake
// (the readiness path). Under deny-by-default that handshake is black-holed and
// the MCPServer can never become Ready, so every restricted-posture server
// without an explicit source needs this peer regardless of whether it is also
// gateway-routed. The caller composes it additively with any gateway peer -
// NetworkPolicy "from" peers are OR'ed.
//
// It returns a nil peer slice - leaving the deny-by-default behavior intact -
// whenever no operator peer applies: the posture is not restricted, the server
// already declares its own ingress sources (which are always honored and must
// then include the operator itself), or the operator cannot pin its own
// identity because either its namespace or its pod labels are unknown. The
// method is pure: it reads only the reconciler's own identity and the server
// spec, so it needs no API access and returns no error.
//
// The derived peer selects the operator controller pod by AND'ing a
// namespaceSelector on the operator namespace with a podSelector on the
// operator's Deployment labels. Both are required: a namespaceSelector alone
// would admit every pod in the operator namespace on the operand's port, which
// under the restricted posture is a wider grant than intended, so a missing
// namespace or missing pod labels fails closed (nil) rather than open.
func (r *MCPServerReconciler) operatorIngressPeers(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
) []networkingv1.NetworkPolicyPeer {
	if r.NetworkPolicyDefaultPosture != PostureRestricted {
		return nil
	}
	// An explicit ingress source is honored as-is and must already list every
	// peer that needs to reach the server, including the operator itself.
	if mcpServer.Spec.Network != nil && len(mcpServer.Spec.Network.IngressFrom) > 0 {
		return nil
	}

	// Without both a namespace and pod labels the operator cannot name a precise
	// source, so it stays deny-by-default rather than admitting a whole namespace
	// or fabricating a source.
	if r.OperatorNamespace == "" || len(r.OperatorPodLabels) == 0 {
		log.FromContext(ctx).V(1).Info(
			"Restricted posture: operator identity is unknown; operand ingress remains deny-by-default "+
				"and operator verification cannot reach it until an explicit ingress source is set",
			keyName, mcpServer.Name,
		)
		return nil
	}

	peer := namespaceNamePeer(r.OperatorNamespace)
	peer.PodSelector = &metav1.LabelSelector{
		MatchLabels: maps.Clone(r.OperatorPodLabels),
	}

	return []networkingv1.NetworkPolicyPeer{peer}
}
