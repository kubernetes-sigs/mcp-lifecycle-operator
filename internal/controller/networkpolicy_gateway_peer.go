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

	mcpv1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1"
)

const (
	// gatewayProviderHTTPRoute mirrors providers/httproute.ProviderName; the
	// string is duplicated here rather than imported to keep the controller
	// package from depending on a concrete provider package.
	gatewayProviderHTTPRoute = "httproute"

	// gatewayProviderKuadrant mirrors providers/kuadrant.ProviderName.
	gatewayProviderKuadrant = "kuadrant"

	// gatewayConfigKeyNamespace is the ConfigMap data key under which the
	// httproute gateway provider records the namespace of the Gateway that
	// fronts an MCPServer.
	gatewayConfigKeyNamespace = "gateway-namespace"

	// kuadrantConfigKeyExtensionNamespace mirrors
	// providers/kuadrant.configKeyExtensionNamespace — the ConfigMap data key
	// under which the kuadrant provider records the namespace of the
	// MCPGatewayExtension (and its broker) that fronts an MCPServer.
	kuadrantConfigKeyExtensionNamespace = "extension-namespace"

	// namespaceNameLabel is the immutable label Kubernetes sets on every
	// namespace, holding the namespace's own name. Selecting on it lets an
	// ingress peer target a namespace without the operator having to label it.
	namespaceNameLabel = "kubernetes.io/metadata.name"
)

// namespaceNamePeer builds an ingress peer that selects an entire namespace by
// its immutable kubernetes.io/metadata.name label, so the operator can target a
// namespace without having to label it first. Callers may narrow the result
// further by setting PodSelector on the returned peer.
func namespaceNamePeer(namespace string) networkingv1.NetworkPolicyPeer {
	return networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{namespaceNameLabel: namespace},
		},
	}
}

// gatewayIngressPeers returns the ingress peers that keep a gateway-routed
// MCPServer reachable under the restricted default ingress posture, which would
// otherwise deny all ingress when the server declares no source of its own.
//
// It returns a nil peer slice - leaving the deny-by-default behavior intact -
// whenever no gateway peer applies: the posture is not restricted, the server is
// not gateway-routed, the provider is not one whose source namespace the
// operator can resolve, the server already declares its own ingress sources
// (which are always honored and must include any gateway source), or the source
// namespace cannot be resolved because the ConfigMap or key is absent.
//
// A transient failure to read the gateway ConfigMap (any error other than
// NotFound) is returned instead: collapsing it to deny-by-default would overwrite
// an existing gateway-allowing policy on a read blip, so the caller propagates the
// error and leaves the current policy unchanged until the next reconcile.
//
// Supported providers:
//   - httproute: the ConfigMap key "gateway-namespace" identifies the namespace
//     of the Gateway whose data-plane pods need to reach the MCPServer.
//   - kuadrant: the ConfigMap key "extension-namespace" identifies the namespace
//     of the MCPGatewayExtension broker that connects upstream to the MCPServer.
//
// The derived peer is a namespaceSelector matching the resolved namespace.
// That is the narrowest source the operator can determine portably: gateway
// data-plane pods and broker pods carry implementation-specific labels (Envoy
// Gateway, Istio, and the kuadrant broker differ), so a podSelector cannot be
// built without per-implementation knowledge.
func (r *MCPServerReconciler) gatewayIngressPeers(
	ctx context.Context,
	mcpServer *mcpv1.MCPServer,
) ([]networkingv1.NetworkPolicyPeer, error) {
	if r.NetworkPolicyDefaultPosture != PostureRestricted {
		return nil, nil
	}
	if mcpServer.Spec.Gateway == nil {
		return nil, nil
	}
	// An explicit ingress source is honored as-is and must already list every
	// peer that needs to reach the server, including the gateway.
	if mcpServer.Spec.Network != nil && len(mcpServer.Spec.Network.IngressFrom) > 0 {
		return nil, nil
	}

	var configKey string
	switch mcpServer.Spec.Gateway.Provider {
	case gatewayProviderHTTPRoute:
		configKey = gatewayConfigKeyNamespace
	case gatewayProviderKuadrant:
		configKey = kuadrantConfigKeyExtensionNamespace
	default:
		return nil, nil
	}

	logger := log.FromContext(ctx)
	namespace, err := r.resolveGatewaySourceNamespace(ctx, mcpServer, configKey)
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

	return []networkingv1.NetworkPolicyPeer{namespaceNamePeer(namespace)}, nil
}

// resolveGatewaySourceNamespace reads a namespace value from the ConfigMap
// referenced by the MCPServer's gateway configuration, using the given data
// key. It returns an empty string and no error when the reference is unset, the
// ConfigMap does not exist, or the key is absent or blank - each of those
// legitimately leaves the caller on the deny-by-default path. Any other read
// failure is returned as an error so the caller can avoid overwriting an
// existing policy on a transient blip.
func (r *MCPServerReconciler) resolveGatewaySourceNamespace(
	ctx context.Context,
	mcpServer *mcpv1.MCPServer,
	dataKey string,
) (string, error) {
	if mcpServer.Spec.Gateway.ConfigRef == "" {
		return "", nil
	}

	cm := &corev1.ConfigMap{}
	key := client.ObjectKey{Name: mcpServer.Spec.Gateway.ConfigRef, Namespace: mcpServer.Namespace}
	// APIReader bypasses the cache to avoid starting a full structured informer
	// for ConfigMaps, which would negate the metadata-only watch.
	if err := r.APIReader.Get(ctx, key, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}

	return strings.TrimSpace(cm.Data[dataKey]), nil
}
