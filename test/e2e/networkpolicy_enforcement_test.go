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
	"fmt"
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

// TestNetworkPolicyEgressEnforcement proves that the CNI actually enforces an
// egress NetworkPolicy - it drops traffic to destinations the policy excludes
// and permits traffic to destinations it admits - using two real
// operator-managed MCP servers: a subject and a target. Unlike
// TestNetworkPolicyEgressRestrictionApplied above (which only confirms the
// generated policy object and that inbound verification still works), this test
// observes egress enforcement directly, from inside the subject pod.
//
// The subject's mcp-server image is a minimal binary with no shell, so the probe
// runs in a busybox ephemeral container injected into the subject pod (sharing
// its network namespace). The probe is a TCP reachability check with a short
// connect timeout against the target's real listening Service, so "allowed"
// genuinely means the connection was established and "blocked" surfaces as a
// timeout (non-zero exit), not a connection refused. ExecProbeInPod returns the
// command exit code separately from harness/transport errors, so the assertions
// distinguish a NetworkPolicy drop (err == nil, non-zero exit) from a broken
// probe (err != nil) rather than treating any failure as "blocked".
//
// Posture confound: under the restricted default posture the target's own
// ingress is deny-by-default and would block the probe regardless of the
// subject's egress. The target is therefore given an explicit ingressFrom that
// admits the subject's namespace, so the probe is valid under BOTH the open and
// restricted postures and the subject's egress remains the sole variable. (The
// operator's own verification probe to the target is then blocked, so the target
// only reaches Available, not Verified - which is all this test needs: a running,
// reachable listener.)
//
// Like TestNetworkPolicyIngressRestrictionEnforced, this relies on the e2e CNI
// (kindnet) preserving pod/namespace identity for east-west traffic with no SNAT
// rewrite, so the destination's namespace label is what the egress rule matches.
func TestNetworkPolicyEgressEnforcement(t *testing.T) {
	t.Parallel()

	tcp := corev1.ProtocolTCP
	// The operator-managed Service exposes the mcp-server on its configured port,
	// which defaults to 8080 (see NewMCPServer). Probe that real port so exit 0
	// means a listener accepted the connection.
	const serverPort = 8080
	port8080 := intstr.FromInt32(serverPort)

	// A namespace label that no namespace carries, used for the blocked case so
	// the subject's egress rule selects no destination and the target becomes
	// unreachable.
	const unreachableNamespace = "no-such-namespace-egress-enforcement"

	// Shared across the feature's Setup/Assess/Teardown closures. The target
	// lives in the per-test managed namespace (via SetupMCPServer, so it is in
	// context and torn down by TeardownMCPServer); the subject lives in a second
	// namespace created here, mirroring TestSamePortDifferentNamespaces.
	var (
		subject   *mcpv1beta1.MCPServer
		subjectNs string
	)

	feature := features.New("Egress NetworkPolicy drops disallowed and permits allowed traffic between managed servers").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()

			// The subject's namespace must exist before the target is created,
			// because the target's ingressFrom selects it by name.
			subjectNs = envconf.RandomName("e2e-egress-subject", 24)
			subjectNsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: subjectNs}}
			if err := r.Create(ctx, subjectNsObj); err != nil {
				t.Fatalf("failed to create subject namespace: %v", err)
			}
			t.Logf("created subject namespace %s", subjectNs)

			// Target in the managed namespace. Its ingress admits only the
			// subject namespace so the probe is valid under both postures.
			ctx = f.SetupMCPServer(ctx, t, cfg, "egress-target", true,
				f.WithNetwork(&mcpv1beta1.NetworkConfig{
					IngressFrom: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{corev1.LabelMetadataName: subjectNs},
							},
						},
					},
				}),
			)
			target := f.ServerFromContext(ctx)

			// A real listener with ready endpoints, so a successful probe proves
			// connectivity rather than racing pod startup.
			f.WaitForEndpointsReady(ctx, t, cfg, target.Namespace, target.Name)

			// Subject in its own namespace, egress allowed to the target
			// namespace on the server port (plus the controller's automatic DNS
			// rule, added because egressTo is set).
			subject = f.NewMCPServer("egress-subject", subjectNs,
				f.WithNetwork(&mcpv1beta1.NetworkConfig{
					EgressTo: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{corev1.LabelMetadataName: target.Namespace},
							},
						},
					},
					EgressPorts: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &port8080},
					},
				}),
			)
			if err := r.Create(ctx, subject); err != nil {
				t.Fatalf("failed to create subject MCPServer: %v", err)
			}
			f.WaitForMCPServerCondition(ctx, t, r, subject, mcpcontroller.ConditionTypeAvailable, metav1.ConditionTrue)
			t.Logf("subject %s/%s is Available", subject.Namespace, subject.Name)

			return ctx
		}).
		Assess("allowed egress reaches the target Service", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			target := f.ServerFromContext(ctx)
			pod := f.FindPodByLabel(ctx, t, cfg, subjectNs, fmt.Sprintf("mcp-server=%s", subject.Name))

			host := fmt.Sprintf("%s.%s.svc", target.Name, target.Namespace)
			// Retry until reachable: tolerate the brief window before the
			// subject's egress/DNS NetworkPolicy rules are programmed in the
			// dataplane, plus transient slowness on a loaded cluster.
			probeEgressUntil(ctx, t, cfg, subjectNs, pod.Name, host, serverPort, true)
			t.Logf("allowed egress confirmed: nc to %s:%d connected", host, serverPort)
			return ctx
		}).
		Assess("disallowed egress is dropped by the CNI", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			target := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			// Re-point the subject's egress at a namespace that does not exist, so
			// the target is no longer an admitted destination. Only the egress
			// peer changes; the DNS rule and the target itself are untouched, so a
			// drop is attributable solely to the egress policy.
			f.UpdateWithRetry(ctx, t, r, subject, func(s *mcpv1beta1.MCPServer) {
				s.Spec.Network.EgressTo = []networkingv1.NetworkPolicyPeer{
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{corev1.LabelMetadataName: unreachableNamespace},
						},
					},
				}
			})
			// Wait for the controller to reconcile the new generation, i.e. to
			// have rewritten the NetworkPolicy to the narrowed egress rule.
			f.WaitForMCPServerReconciled(ctx, t, r, subject, 3*time.Minute)

			pod := f.FindPodByLabel(ctx, t, cfg, subjectNs, fmt.Sprintf("mcp-server=%s", subject.Name))
			host := fmt.Sprintf("%s.%s.svc", target.Name, target.Namespace)
			// Retry until dropped: the controller has written the narrowed
			// NetworkPolicy (observedGeneration is current), but the CNI may take
			// a moment to reprogram the dataplane, during which the previous allow
			// rule can still admit the connection.
			probeEgressUntil(ctx, t, cfg, subjectNs, pod.Name, host, serverPort, false)
			t.Logf("egress drop confirmed: nc to %s:%d no longer connects", host, serverPort)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()
			if subject != nil {
				if err := r.Delete(ctx, subject); err != nil {
					t.Logf("failed to delete subject MCPServer: %v", err)
				}
			}
			if subjectNs != "" {
				nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: subjectNs}}
				if err := r.Delete(ctx, nsObj); err != nil {
					t.Logf("failed to delete subject namespace %s: %v", subjectNs, err)
				}
			}
			// Target teardown (it is the server stored in context).
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// probeEgressUntil repeatedly runs a busybox TCP reachability probe from podName
// to host:port until the observed reachability matches want, or a deadline
// elapses. It exists because egress enforcement is observed against the live
// dataplane: there is a brief window after a NetworkPolicy is written (or first
// created) during which the CNI has not yet (re)programmed its rules, and a
// loaded cluster can make a single connect legitimately slow. Probing once would
// turn that timing into a flake, so this polls instead.
//
// A harness/transport error from ExecProbeInPod is never a policy result and
// fails the test immediately. A persistent wrong reachability fails on timeout
// with the last observed exit code, so a genuine enforcement regression is still
// reported honestly rather than masked by the retry.
//
// Each attempt injects a fresh ephemeral container (see ExecProbeInPod); they
// accumulate on the pod but are bounded by namespace teardown, and the expected
// path matches want on the first attempt.
func probeEgressUntil(ctx context.Context, t *testing.T, cfg *envconf.Config,
	namespace, podName, host string, port int, want bool) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for attempt := 1; ; attempt++ {
		_, stderr, exitCode, err := f.ExecProbeInPod(ctx, t, cfg, namespace, podName,
			[]string{"nc", "-z", "-w3", host, fmt.Sprintf("%d", port)})
		if err != nil {
			t.Fatalf("probe harness/transport failure (not a policy result): %v", err)
		}
		reachable := exitCode == 0
		if reachable == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("probe to %s:%d: wanted reachable=%v but observed reachable=%v after %d attempts (last exit %d: %s)",
				host, port, want, reachable, attempt, exitCode, stderr)
		}
		time.Sleep(3 * time.Second)
	}
}
