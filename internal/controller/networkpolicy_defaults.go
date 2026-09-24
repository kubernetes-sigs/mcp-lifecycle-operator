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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

// NetworkPolicyPosture selects the operand NetworkPolicy content emitted for an
// MCPServer that leaves a network dimension (ingress or egress) unconfigured. It
// never affects user-supplied Spec.Network values, which are always honored. The
// empty value is treated as PostureOpen, so a reconciler that does not set it
// behaves exactly as it did before this option existed.
type NetworkPolicyPosture string

const (
	// PostureOpen preserves the operator's historical default: an ingress rule
	// scoped to the server port but open to any source, and an allow-all egress
	// rule, when the respective dimension is left unconfigured.
	PostureOpen NetworkPolicyPosture = "open"
	// PostureRestricted applies a least-privilege default: deny-by-default ingress
	// when no source is configured (without fabricating a source), and unmanaged
	// egress (Egress dropped from the policy) when no egress is configured.
	PostureRestricted NetworkPolicyPosture = "restricted"
)

// ParsePosture parses a posture value case-insensitively. An empty value
// resolves to PostureOpen. An unrecognized value returns an error so callers can
// fail fast instead of silently applying an unexpected default.
func ParsePosture(value string) (NetworkPolicyPosture, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(PostureOpen):
		return PostureOpen, nil
	case string(PostureRestricted):
		return PostureRestricted, nil
	default:
		return "", fmt.Errorf("invalid network policy default posture %q: must be %q or %q",
			value, PostureOpen, PostureRestricted)
	}
}

// defaultIngressRules returns the ingress rules for an operand NetworkPolicy.
//
// When the MCPServer declares an ingress source (Spec.Network.IngressFrom), that
// source is honored regardless of posture. When no source is declared:
//   - PostureOpen emits a single rule scoped to the server port with no source
//     (historical behavior; any source may reach the port).
//   - PostureRestricted emits no rule (deny-by-default). It never fabricates a
//     placeholder source such as an empty peer or a universal CIDR.
func defaultIngressRules(
	mcpServer *mcpv1beta1.MCPServer,
	posture NetworkPolicyPosture,
) []networkingv1.NetworkPolicyIngressRule {
	if mcpServer.Spec.Network != nil && len(mcpServer.Spec.Network.IngressFrom) > 0 {
		return []networkingv1.NetworkPolicyIngressRule{
			{
				Ports: ingressPorts(mcpServer),
				From:  mcpServer.Spec.Network.DeepCopy().IngressFrom,
			},
		}
	}

	if posture == PostureRestricted {
		return []networkingv1.NetworkPolicyIngressRule{}
	}

	return []networkingv1.NetworkPolicyIngressRule{
		{
			Ports: ingressPorts(mcpServer),
		},
	}
}

// ingressPorts returns the single-port allow-list (server port, TCP) shared by
// every non-deny ingress rule.
func ingressPorts(mcpServer *mcpv1beta1.MCPServer) []networkingv1.NetworkPolicyPort {
	port := intstr.FromInt32(mcpServer.Spec.Config.Port)
	protocol := corev1.ProtocolTCP
	return []networkingv1.NetworkPolicyPort{
		{
			Port:     &port,
			Protocol: &protocol,
		},
	}
}
