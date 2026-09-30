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
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

const (
	// gatewayProviderHTTPRoute is the only gateway provider whose ConfigMap
	// records the Gateway namespace (under gatewayConfigKeyNamespace), so it is
	// the only provider for which the operator can derive a gateway ingress peer
	// today. It mirrors providers/httproute.ProviderName; the string is
	// duplicated here rather than imported to keep the controller package from
	// depending on a concrete provider package.
	gatewayProviderHTTPRoute = "httproute"

	// gatewayConfigKeyNamespace is the ConfigMap data key under which the
	// httproute gateway provider records the namespace of the Gateway that
	// fronts an MCPServer. Resolving it lets the operator admit the gateway
	// under the restricted ingress posture. Other providers (e.g. kuadrant) do
	// not record a Gateway namespace and are handled separately - see
	// gatewayIngressPeers.
	gatewayConfigKeyNamespace = "gateway-namespace"

	// namespaceNameLabel is the immutable label Kubernetes sets on every
	// namespace, holding the namespace's own name. Selecting on it lets an
	// ingress peer target a namespace without the operator having to label it.
	namespaceNameLabel = "kubernetes.io/metadata.name"
)

// gatewayIngressPeers returns the ingress peers that keep a gateway-routed
// MCPServer reachable under the restricted default ingress posture, which would
// otherwise deny all ingress when the server declares no source of its own.
//
// It returns a nil peer slice - leaving the deny-by-default behavior intact -
// whenever no gateway peer applies: the posture is not restricted, the server is
// not gateway-routed, the provider is not one whose Gateway namespace the
// operator can resolve, the server already declares its own ingress sources
// (which are always honored and must include any gateway source), or the gateway
// namespace cannot be resolved because the ConfigMap or key is absent.
//
// A transient failure to read the gateway ConfigMap (any error other than
// NotFound) is returned instead: collapsing it to deny-by-default would overwrite
// an existing gateway-allowing policy on a read blip, so the caller propagates the
// error and leaves the current policy unchanged until the next reconcile.
//
// Only the httproute provider is supported today: its ConfigMap records the
// Gateway namespace. Other providers (e.g. kuadrant) do not record a Gateway
// namespace - the kuadrant ConfigMap carries only the extension namespace, which
// is not the data-plane source - so they stay deny-by-default until a
// provider-aware source is designed. A kuadrant-routed server must therefore set
// spec.network.ingressFrom explicitly under the restricted posture.
//
// The derived peer is a namespaceSelector matching the whole gateway namespace.
// That is the narrowest source the operator can determine portably: the gateway
// ConfigMap records the gateway namespace, but gateway data-plane pods carry
// implementation-specific labels (Envoy Gateway and Istio differ), so a
// podSelector cannot be built without per-implementation knowledge.
func (r *MCPServerReconciler) gatewayIngressPeers(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
) ([]networkingv1.NetworkPolicyPeer, error) {
	if r.NetworkPolicyDefaultPosture != PostureRestricted {
		return nil, nil
	}
	if mcpServer.Spec.Gateway == nil {
		return nil, nil
	}
	// Only the httproute provider records a resolvable Gateway namespace; other
	// providers stay deny-by-default until a provider-aware source is designed.
	if mcpServer.Spec.Gateway.Provider != gatewayProviderHTTPRoute {
		return nil, nil
	}
	// An explicit ingress source is honored as-is and must already list every
	// peer that needs to reach the server, including the gateway.
	if mcpServer.Spec.Network != nil && len(mcpServer.Spec.Network.IngressFrom) > 0 {
		return nil, nil
	}

	logger := log.FromContext(ctx)
	namespace, err := r.resolveGatewayNamespace(ctx, mcpServer)
	if err != nil {
		return nil, fmt.Errorf("resolving gateway namespace for MCPServer %q: %w", mcpServer.Name, err)
	}
	if namespace == "" {
		logger.Info(
			"Restricted posture: could not resolve gateway namespace; ingress remains deny-by-default",
			keyName, mcpServer.Name,
		)
		return nil, nil
	}

	return []networkingv1.NetworkPolicyPeer{
		{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{namespaceNameLabel: namespace},
			},
		},
	}, nil
}

// resolveGatewayNamespace reads the gateway namespace from the ConfigMap
// referenced by the MCPServer's gateway configuration. It returns an empty
// string and no error when the reference is unset, the ConfigMap does not exist,
// or the key is absent or blank - each of those legitimately leaves the caller
// on the deny-by-default path. Any other read failure is returned as an error so
// the caller can avoid overwriting an existing policy on a transient blip.
func (r *MCPServerReconciler) resolveGatewayNamespace(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
) (string, error) {
	if mcpServer.Spec.Gateway.ConfigRef == "" {
		return "", nil
	}

	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Name: mcpServer.Spec.Gateway.ConfigRef, Namespace: mcpServer.Namespace}
	if err := r.Get(ctx, key, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}

	return strings.TrimSpace(cm.Data[gatewayConfigKeyNamespace]), nil
}
