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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func TestParseDefaultPosture(t *testing.T) {
	tt := []struct {
		name    string
		in      string
		want    NetworkPolicyDefaultPosture
		wantErr bool
	}{
		{name: "empty resolves to open", in: "", want: PostureOpen},
		{name: "open", in: "open", want: PostureOpen},
		{name: "restricted", in: "restricted", want: PostureRestricted},
		{name: "mixed case open", in: "Open", want: PostureOpen},
		{name: "mixed case restricted", in: "RESTRICTED", want: PostureRestricted},
		{name: "surrounding whitespace", in: "  restricted  ", want: PostureRestricted},
		{name: "unknown value errors", in: "deny-all", wantErr: true},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDefaultPosture(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got none", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseDefaultPosture(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDefaultIngressRules(t *testing.T) {
	const port int32 = 8080

	portRule := func() networkingv1.NetworkPolicyIngressRule {
		p := intstr.FromInt32(port)
		proto := corev1.ProtocolTCP
		return networkingv1.NetworkPolicyIngressRule{
			Ports: []networkingv1.NetworkPolicyPort{{Port: &p, Protocol: &proto}},
		}
	}

	userPeer := networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "caller"}},
	}

	newServer := func(network *mcpv1beta1.NetworkConfig) *mcpv1beta1.MCPServer {
		s := &mcpv1beta1.MCPServer{}
		s.Spec.Config.Port = port
		s.Spec.Network = network
		return s
	}

	tt := []struct {
		name    string
		server  *mcpv1beta1.MCPServer
		posture NetworkPolicyDefaultPosture
		want    []networkingv1.NetworkPolicyIngressRule
	}{
		{
			name:    "open, no network -> port-only rule (historical)",
			server:  newServer(nil),
			posture: PostureOpen,
			want:    []networkingv1.NetworkPolicyIngressRule{portRule()},
		},
		{
			name:    "empty posture behaves as open",
			server:  newServer(nil),
			posture: "",
			want:    []networkingv1.NetworkPolicyIngressRule{portRule()},
		},
		{
			name:    "open, network without IngressFrom -> port-only rule",
			server:  newServer(&mcpv1beta1.NetworkConfig{}),
			posture: PostureOpen,
			want:    []networkingv1.NetworkPolicyIngressRule{portRule()},
		},
		{
			name:    "restricted, no network -> deny (empty rules)",
			server:  newServer(nil),
			posture: PostureRestricted,
			want:    []networkingv1.NetworkPolicyIngressRule{},
		},
		{
			name:    "restricted, network without IngressFrom -> deny (empty rules)",
			server:  newServer(&mcpv1beta1.NetworkConfig{}),
			posture: PostureRestricted,
			want:    []networkingv1.NetworkPolicyIngressRule{},
		},
		{
			name:    "restricted, IngressFrom set -> honored (port + source)",
			server:  newServer(&mcpv1beta1.NetworkConfig{IngressFrom: []networkingv1.NetworkPolicyPeer{userPeer}}),
			posture: PostureRestricted,
			want: []networkingv1.NetworkPolicyIngressRule{{
				Ports: portRule().Ports,
				From:  []networkingv1.NetworkPolicyPeer{userPeer},
			}},
		},
		{
			name:    "open, IngressFrom set -> honored (port + source)",
			server:  newServer(&mcpv1beta1.NetworkConfig{IngressFrom: []networkingv1.NetworkPolicyPeer{userPeer}}),
			posture: PostureOpen,
			want: []networkingv1.NetworkPolicyIngressRule{{
				Ports: portRule().Ports,
				From:  []networkingv1.NetworkPolicyPeer{userPeer},
			}},
		},
	}

	for _, tc := range tt {
		t.Run("ingress/"+tc.name, func(t *testing.T) {
			got := defaultIngressRules(tc.server, tc.posture)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("defaultIngressRules() = %#v, want %#v", got, tc.want)
			}
			// INV-2: never fabricate a source. Any rule with a From list must
			// carry only the user-supplied peers, and no rule may contain an
			// empty/wildcard peer or a universal CIDR.
			for _, rule := range got {
				for _, peer := range rule.From {
					if peer.PodSelector == nil && peer.NamespaceSelector == nil && peer.IPBlock == nil {
						t.Fatalf("fabricated empty ingress peer in rule %#v", rule)
					}
					if peer.IPBlock != nil && (peer.IPBlock.CIDR == "0.0.0.0/0" || peer.IPBlock.CIDR == "::/0") {
						t.Fatalf("universal CIDR peer in rule %#v", rule)
					}
				}
			}
		})
	}
}

// TestRestrictedPostureReportingComposition verifies that the restricted ingress
// end-state composes with the informational NetworkPolicyRestricted condition:
// a deny-by-default ingress (empty ingress rules) must be reported as ingress
// restricted, not as source-unrestricted. Egress under US1 is still allow-all,
// so with no egress config the overall condition stays False for egress.
func TestRestrictedPostureReportingComposition(t *testing.T) {
	r := &MCPServerReconciler{NetworkPolicyDefaultPosture: PostureRestricted}

	tt := []struct {
		name       string
		network    *mcpv1beta1.NetworkConfig
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{
			name:       "no config: ingress deny-by-default reported restricted, egress unrestricted",
			network:    nil,
			wantStatus: metav1.ConditionFalse,
			wantReason: ReasonNetworkPolicyEgressUnrestricted,
		},
		{
			name: "egress restricted too: both restricted",
			network: &mcpv1beta1.NetworkConfig{
				EgressTo: []networkingv1.NetworkPolicyPeer{
					{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}},
				},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: ReasonNetworkPolicyRestricted,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			mcpServer := postureTestServer("compose-"+tc.name, tc.network)
			c := r.networkPolicyPostureCondition(mcpServer, mcpServer.Generation, nil)

			if c.Type != ConditionTypeNetworkPolicyRestricted {
				t.Fatalf("Type = %q, want %q", c.Type, ConditionTypeNetworkPolicyRestricted)
			}
			if c.Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q (reason %q)", c.Status, tc.wantStatus, c.Reason)
			}
			if c.Reason != tc.wantReason {
				t.Fatalf("Reason = %q, want %q", c.Reason, tc.wantReason)
			}
			// The reason must never be Unrestricted: that would mean the deny-by-default
			// ingress was misclassified as source-open.
			if c.Reason == ReasonNetworkPolicyUnrestricted {
				t.Fatalf("deny-by-default ingress misreported as Unrestricted")
			}
		})
	}
}

// TestHasIngressSourceRestriction covers the source-restriction predicate,
// including the deny-by-default carve-out and the patterns that match every
// source and therefore must NOT read as a restriction (empty namespaceSelector,
// universal CIDR, a rule that names a source but restricts no port).
func TestHasIngressSourceRestriction(t *testing.T) {
	ingressType := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	port := intstr.FromInt32(8080)
	ports := []networkingv1.NetworkPolicyPort{{Port: &port}}

	npWith := func(rules []networkingv1.NetworkPolicyIngressRule) *networkingv1.NetworkPolicy {
		return &networkingv1.NetworkPolicy{
			Spec: networkingv1.NetworkPolicySpec{PolicyTypes: ingressType, Ingress: rules},
		}
	}

	tt := []struct {
		name string
		np   *networkingv1.NetworkPolicy
		want bool
	}{
		{
			name: "deny-by-default (empty ingress, Ingress in policyTypes)",
			np:   npWith([]networkingv1.NetworkPolicyIngressRule{}),
			want: true,
		},
		{
			name: "port-only rule, no source",
			np:   npWith([]networkingv1.NetworkPolicyIngressRule{{Ports: ports}}),
			want: false,
		},
		{
			name: "podSelector source with ports",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From:  []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}}},
			}}),
			want: true,
		},
		{
			// Finding 3: a named source without a port restriction admits that
			// source on every port, so it is not a genuine restriction.
			name: "source but no port restriction",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}}},
			}}),
			want: false,
		},
		{
			// Finding 4: an empty namespaceSelector matches all namespaces.
			name: "empty namespaceSelector matches all namespaces",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{}}},
			}}),
			want: false,
		},
		{
			name: "non-empty namespaceSelector restricts",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From:  []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "clients"}}}},
			}}),
			want: true,
		},
		{
			// Finding 4: a universal CIDR matches every address.
			name: "universal CIDR does not restrict",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From:  []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0"}}},
			}}),
			want: false,
		},
		{
			name: "universal CIDR with an exception restricts",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From:  []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0", Except: []string{"10.0.0.0/8"}}}},
			}}),
			want: true,
		},
		{
			name: "specific CIDR restricts",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From:  []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}}},
			}}),
			want: true,
		},
		{
			// Empty podSelector with no namespaceSelector restricts to the policy's
			// own namespace - a genuine (if broad) restriction.
			name: "empty podSelector restricts to same namespace",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From:  []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
			}}),
			want: true,
		},
		{
			// Empty podSelector AND empty namespaceSelector = all pods, all
			// namespaces = not a restriction.
			name: "empty podSelector with empty namespaceSelector matches everything",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From: []networkingv1.NetworkPolicyPeer{{
					PodSelector:       &metav1.LabelSelector{},
					NamespaceSelector: &metav1.LabelSelector{},
				}},
			}}),
			want: false,
		},
		{
			// Peers within a rule are OR'ed: a restrictive podSelector alongside a
			// universal CIDR still admits every source, so it must not read as a
			// restriction.
			name: "restrictive peer OR'ed with universal CIDR does not restrict",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From: []networkingv1.NetworkPolicyPeer{
					{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}},
					{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0"}},
				},
			}}),
			want: false,
		},
		{
			// Every peer in the rule restricts, so the rule as a whole restricts.
			name: "all peers restrict",
			np: npWith([]networkingv1.NetworkPolicyIngressRule{{
				Ports: ports,
				From: []networkingv1.NetworkPolicyPeer{
					{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}}},
					{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}},
				},
			}}),
			want: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasIngressSourceRestriction(tc.np); got != tc.want {
				t.Errorf("hasIngressSourceRestriction() = %v, want %v", got, tc.want)
			}
		})
	}
}
