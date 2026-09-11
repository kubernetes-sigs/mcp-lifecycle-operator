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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

// postureTestServer builds a minimal MCPServer with the given network config.
// createNetworkPolicy only reads Name, Namespace, Config.Port and Network, so a
// full source/image spec is not needed here.
func postureTestServer(name string, network *mcpv1beta1.NetworkConfig) *mcpv1beta1.MCPServer {
	return &mcpv1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: mcpv1beta1.MCPServerSpec{
			Config:  mcpv1beta1.ServerConfig{Port: 8080},
			Network: network,
		},
	}
}

func TestNetworkPolicyPostureCondition(t *testing.T) {
	r := &MCPServerReconciler{}

	restrictedIngress := []networkingv1.NetworkPolicyPeer{
		{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"mcp-client": "true"}}},
	}
	restrictedEgress := []networkingv1.NetworkPolicyPeer{
		{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}},
	}

	tests := []struct {
		name           string
		network        *mcpv1beta1.NetworkConfig
		wantStatus     metav1.ConditionStatus
		wantReason     string
		wantMsgKeyword string // substring that must appear so an admin sees the open direction
		wantMsgAbsent  string // substring that must NOT appear (guards against overstating restriction)
	}{
		{
			name:           "both restricted",
			network:        &mcpv1beta1.NetworkConfig{IngressFrom: restrictedIngress, EgressTo: restrictedEgress},
			wantStatus:     metav1.ConditionTrue,
			wantReason:     ReasonNetworkPolicyRestricted,
			wantMsgKeyword: "restricts",
		},
		{
			name:           "ingress restricted, egress open",
			network:        &mcpv1beta1.NetworkConfig{IngressFrom: restrictedIngress},
			wantStatus:     metav1.ConditionFalse,
			wantReason:     ReasonNetworkPolicyEgressUnrestricted,
			wantMsgKeyword: "egress",
		},
		{
			name:           "egress restricted, ingress open",
			network:        &mcpv1beta1.NetworkConfig{EgressTo: restrictedEgress},
			wantStatus:     metav1.ConditionFalse,
			wantReason:     ReasonNetworkPolicyIngressUnrestricted,
			wantMsgKeyword: "ingress",
		},
		{
			name:           "both unrestricted (default, no Spec.Network)",
			network:        nil,
			wantStatus:     metav1.ConditionFalse,
			wantReason:     ReasonNetworkPolicyUnrestricted,
			wantMsgKeyword: "ingress",
		},
		{
			// DNS-only egress: user sets egress ports, which yields the operator's
			// DNS rule + a user rule constraining ports. hasEgressDestinationRestriction
			// treats this as restricted, so posture must agree (spec Edge Cases).
			name: "dns-only egress counts as restricted",
			network: &mcpv1beta1.NetworkConfig{
				IngressFrom: restrictedIngress,
				EgressPorts: []networkingv1.NetworkPolicyPort{
					{Port: ptrIntStr(443), Protocol: ptr.To(corev1.ProtocolTCP)},
				},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: ReasonNetworkPolicyRestricted,
			// Ports-only egress restricts the reachable ports but not the set of
			// destinations, so the message must not claim destinations are limited.
			wantMsgKeyword: "ports",
			wantMsgAbsent:  "destinations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mcpServer := postureTestServer("posture-"+strings.ReplaceAll(tt.name, " ", "-"), tt.network)
			mcpServer.Generation = 7

			c := r.networkPolicyPostureCondition(mcpServer, mcpServer.Generation, nil)

			if c.Type != ConditionTypeNetworkPolicyRestricted {
				t.Errorf("Type = %q, want %q", c.Type, ConditionTypeNetworkPolicyRestricted)
			}
			if c.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", c.Status, tt.wantStatus)
			}
			if c.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", c.Reason, tt.wantReason)
			}
			if c.ObservedGeneration != 7 {
				t.Errorf("ObservedGeneration = %d, want 7", c.ObservedGeneration)
			}
			if !strings.Contains(strings.ToLower(c.Message), tt.wantMsgKeyword) {
				t.Errorf("Message = %q, want it to contain %q", c.Message, tt.wantMsgKeyword)
			}
			if tt.wantMsgAbsent != "" && strings.Contains(strings.ToLower(c.Message), tt.wantMsgAbsent) {
				t.Errorf("Message = %q, want it to NOT contain %q", c.Message, tt.wantMsgAbsent)
			}
			// Status True only when Restricted (contract "Stable enum").
			if (c.Status == metav1.ConditionTrue) != (c.Reason == ReasonNetworkPolicyRestricted) {
				t.Errorf("status/reason invariant violated: status=%q reason=%q", c.Status, c.Reason)
			}
		})
	}
}

// TestNetworkPolicyPostureConditionPreservesTransitionTime verifies the
// LastTransitionTime is preserved when the status is unchanged and advances on
// a real transition (SC-003, mirrors newAvailableCondition behaviour).
func TestNetworkPolicyPostureConditionPreservesTransitionTime(t *testing.T) {
	r := &MCPServerReconciler{}
	earlier := metav1.NewTime(time.Now().Add(-time.Hour))

	// Existing condition: unrestricted, stamped an hour ago.
	existing := []metav1.Condition{{
		Type:               ConditionTypeNetworkPolicyRestricted,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonNetworkPolicyUnrestricted,
		LastTransitionTime: earlier,
	}}

	// Steady state: still unrestricted -> timestamp preserved.
	same := r.networkPolicyPostureCondition(postureTestServer("np-steady", nil), 1, existing)
	if !same.LastTransitionTime.Equal(&earlier) {
		t.Errorf("steady-state LastTransitionTime = %v, want preserved %v", same.LastTransitionTime, earlier)
	}

	// Transition to restricted -> timestamp advances.
	restricted := &mcpv1beta1.NetworkConfig{
		IngressFrom: []networkingv1.NetworkPolicyPeer{
			{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}},
		},
		EgressTo: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}}},
	}
	changed := r.networkPolicyPostureCondition(postureTestServer("np-changed", restricted), 2, existing)
	if changed.LastTransitionTime.Equal(&earlier) {
		t.Errorf("transition LastTransitionTime should advance, got preserved %v", earlier)
	}
}

func ptrIntStr(v int32) *intstr.IntOrString {
	p := intstr.FromInt32(v)
	return &p
}
