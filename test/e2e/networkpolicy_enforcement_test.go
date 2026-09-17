//go:build e2e

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

package e2e

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

// TestNetworkPolicyIngressRestrictionEnforced verifies that an ingress
// restriction is actually enforced by the cluster CNI (not merely reflected in
// the generated NetworkPolicy object). The operator's own MCP handshake probe
// runs from the operator namespace, so an ingress restriction that does not
// admit that namespace blocks the probe and drives Verified=False, even though
// the workload itself is healthy (Available=True; kubelet readiness probes are
// not subject to NetworkPolicy).
//
// It then confirms the recovery path: widening the ingress rule to admit the
// operator namespace, which also bumps the generation and re-triggers
// verification, flips Verified back to True. This documents that Verified does
// not auto-recover once handshake retries are exhausted - a spec change (or
// operator restart) is required.
//
// The structural NetworkPolicy e2e tests only cover the default allow-all
// policy; this is the only e2e coverage of a *restricted* ingress policy and
// its effect on the Verified condition.
func TestNetworkPolicyIngressRestrictionEnforced(t *testing.T) {
	t.Parallel()

	// A namespace label that no namespace in the cluster carries, so the
	// generated ingress rule admits nothing - in particular not the operator
	// namespace from which the verify probe originates.
	const clientNamespaceLabel = "mcp-client"

	feature := features.New("Ingress restriction blocks operator verification and recovers when widened").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "netpol-ingress-restricted", true,
				f.WithNetwork(&mcpv1beta1.NetworkConfig{
					IngressFrom: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{clientNamespaceLabel: "true"},
							},
						},
					},
				}),
			)
		}).
		Assess("generated NetworkPolicy carries the restricted ingress selector", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			netpol := &networkingv1.NetworkPolicy{}
			if err := r.Get(ctx, server.Name, server.Namespace, netpol); err != nil {
				t.Fatalf("NetworkPolicy not found: %v", err)
			}
			if len(netpol.Spec.Ingress) != 1 {
				t.Fatalf("expected 1 ingress rule, got %d", len(netpol.Spec.Ingress))
			}
			from := netpol.Spec.Ingress[0].From
			if len(from) != 1 || from[0].NamespaceSelector == nil {
				t.Fatalf("expected a single namespaceSelector ingress peer, got %#v", from)
			}
			if got := from[0].NamespaceSelector.MatchLabels[clientNamespaceLabel]; got != "true" {
				t.Fatalf("expected ingress namespaceSelector {%s: true}, got %v",
					clientNamespaceLabel, from[0].NamespaceSelector.MatchLabels)
			}
			t.Logf("NetworkPolicy %s restricts ingress to namespaces labeled %s=true", netpol.Name, clientNamespaceLabel)
			return ctx
		}).
		Assess("workload is Available but Verified is False because the probe is blocked", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			// The operator's verify handshake, originating from the operator
			// namespace, is dropped by the ingress rule.
			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				mcpcontroller.ConditionTypeVerified, metav1.ConditionFalse, mcpcontroller.ReasonEndpointUnavailable,
				2*time.Minute)

			// The workload itself stays healthy: kubelet readiness probes bypass
			// NetworkPolicy, so Available remains True even while the operator's
			// cross-namespace probe is blocked. Setup already waited for
			// Available=True; here we assert the two conditions co-occur rather
			// than waiting again.
			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			if c := f.GetMCPServerCondition(server, mcpcontroller.ConditionTypeAvailable); c == nil || c.Status != metav1.ConditionTrue {
				t.Fatalf("expected Available=True while Verified=False, got %#v", c)
			}
			t.Log("ingress restriction enforced: Available=True, Verified=False (EndpointUnavailable)")
			return ctx
		}).
		Assess("widening ingress to admit the operator namespace restores Verified=True", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			opRef := f.MustDiscoverOperatorOnce(ctx, cfg, t)

			// Every namespace carries the immutable kubernetes.io/metadata.name
			// label, so selecting the operator namespace by name admits its
			// pods without mutating any shared cluster state. This spec change
			// also bumps the generation, which resets the handshake retry budget
			// and re-triggers verification - Verified does not self-heal once
			// retries are exhausted.
			//
			// This relies on the CNI preserving the operator pod's namespace
			// identity for east-west traffic (no SNAT or proxy rewrite of the
			// source), which holds for kindnet. On a CNI that SNATs pod-to-pod
			// traffic the widened selector would not match the probe source and
			// this step would time out.
			f.UpdateWithRetry(ctx, t, r, server, func(s *mcpv1beta1.MCPServer) {
				s.Spec.Network.IngressFrom = []networkingv1.NetworkPolicyPeer{
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{corev1.LabelMetadataName: opRef.Namespace},
						},
					},
				}
			})

			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server, 3*time.Minute)
			t.Logf("verification recovered after admitting operator namespace %q: Verified=True", opRef.Namespace)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// TestNetworkPolicyEgressRestrictionApplied verifies that configuring an egress
// restriction produces the expected NetworkPolicy in a live cluster - the
// automatic DNS rule plus the user-configured destination rule (CIDR and port)
// - and that adding an egress restriction does not break inbound verification:
// the operator's probe reaches the pod on the ingress path, so Verified stays
// True.
//
// This test deliberately does NOT assert that the CNI actually drops disallowed
// egress. Inbound verification rides the ingress path and established-connection
// replies, so it cannot observe egress enforcement; proving egress is blocked
// would require an in-pod probe to a disallowed destination, which is out of
// scope here. TestNetworkPolicyIngressRestrictionEnforced above is the
// enforcement proof (a blocked probe drives Verified=False). The existing
// structural e2e tests only cover the default allow-all egress rule.
func TestNetworkPolicyEgressRestrictionApplied(t *testing.T) {
	t.Parallel()

	// The user rule restricts egress to 10.0.0.0/8:443, mirroring the
	// with_egress_restriction sample. These values do not gate the result:
	// verification is inbound and the automatic DNS rule is added regardless, so
	// the pod reaches Available/Verified independent of this destination.
	tcp := corev1.ProtocolTCP
	port443 := intstr.FromInt32(443)

	feature := features.New("Egress restriction is applied without breaking verification").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "netpol-egress-restricted", true,
				f.WithNetwork(&mcpv1beta1.NetworkConfig{
					EgressTo: []networkingv1.NetworkPolicyPeer{
						{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}},
					},
					EgressPorts: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &port443},
					},
				}),
			)
		}).
		Assess("generated NetworkPolicy restricts egress and adds the DNS rule", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			netpol := &networkingv1.NetworkPolicy{}
			if err := r.Get(ctx, server.Name, server.Namespace, netpol); err != nil {
				t.Fatalf("NetworkPolicy not found: %v", err)
			}

			hasEgress := false
			for _, pt := range netpol.Spec.PolicyTypes {
				if pt == networkingv1.PolicyTypeEgress {
					hasEgress = true
				}
			}
			if !hasEgress {
				t.Fatalf("expected Egress policyType, got %v", netpol.Spec.PolicyTypes)
			}

			// Exactly two rules are expected, matching the controller's
			// buildEgressRules output: an unrestricted DNS rule (UDP/53 + TCP/53,
			// no destinations) and the user rule (only the configured CIDR and
			// TCP/443). Classify by content rather than index, and compare each
			// rule exactly - not just presence - so a regression that adds an
			// extra destination or port is caught.
			if len(netpol.Spec.Egress) != 2 {
				t.Fatalf("expected 2 egress rules (DNS + configured), got %d: %#v",
					len(netpol.Spec.Egress), netpol.Spec.Egress)
			}

			portKey := func(p networkingv1.NetworkPolicyPort) string {
				proto := ""
				if p.Protocol != nil {
					proto = string(*p.Protocol)
				}
				port := ""
				if p.Port != nil {
					port = p.Port.String()
				}
				return proto + "/" + port
			}
			portSet := func(ports []networkingv1.NetworkPolicyPort) map[string]bool {
				s := make(map[string]bool, len(ports))
				for _, p := range ports {
					s[portKey(p)] = true
				}
				return s
			}

			var sawDNS, sawConfigured bool
			for _, rule := range netpol.Spec.Egress {
				ports := portSet(rule.Ports)
				if len(rule.To) == 0 {
					// DNS rule: no destinations, exactly UDP/53 and TCP/53.
					if len(ports) != 2 || !ports["UDP/53"] || !ports["TCP/53"] {
						t.Fatalf("DNS egress rule must allow only UDP/53 and TCP/53, got %#v", rule.Ports)
					}
					sawDNS = true
					continue
				}
				// User rule: exactly the configured CIDR and TCP/443, nothing else.
				if len(rule.To) != 1 || rule.To[0].IPBlock == nil ||
					rule.To[0].IPBlock.CIDR != "10.0.0.0/8" || len(rule.To[0].IPBlock.Except) != 0 ||
					rule.To[0].PodSelector != nil || rule.To[0].NamespaceSelector != nil {
					t.Fatalf("configured egress rule must target only 10.0.0.0/8, got %#v", rule.To)
				}
				if len(ports) != 1 || !ports["TCP/443"] {
					t.Fatalf("configured egress rule must allow only TCP/443, got %#v", rule.Ports)
				}
				sawConfigured = true
			}
			if !sawDNS {
				t.Fatalf("expected a DNS egress rule (UDP/53 + TCP/53), got %#v", netpol.Spec.Egress)
			}
			if !sawConfigured {
				t.Fatalf("expected the configured egress rule (10.0.0.0/8, TCP/443), got %#v", netpol.Spec.Egress)
			}
			t.Logf("NetworkPolicy %s restricts egress to 10.0.0.0/8:443 plus the automatic DNS rule", netpol.Name)
			return ctx
		}).
		Assess("egress restriction does not block inbound verification", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			// Ingress is unrestricted, so the operator's probe still reaches the
			// pod and the MCP handshake succeeds.
			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server, 3*time.Minute)
			t.Log("egress restriction applied while Verified=True (inbound probe unaffected)")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
