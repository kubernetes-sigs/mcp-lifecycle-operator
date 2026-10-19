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
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

const (
	// envNetworkPolicyPosture is an OPTIONAL override: when set it forces the
	// posture the suite asserts, regardless of what is deployed. When it is
	// unset/empty the posture is auto-detected from the live operator (see
	// networkPolicyPosture). Our own CI sets it to exercise both modes; other
	// runners (e.g. downstream smoke that deploys restricted) can leave it unset
	// and still assert the right posture with zero wiring.
	envNetworkPolicyPosture = "E2E_NETWORK_POLICY_POSTURE"

	// networkPolicyPostureFlag is the operator flag whose value is the operand's
	// default NetworkPolicy posture. Compared with leading dashes stripped so both
	// -flag=value and --flag=value match.
	networkPolicyPostureFlag = "network-policy-default-posture"

	// managerContainerName is the operator controller-manager container name
	// (config/manager/manager.yaml) that carries the posture flag.
	managerContainerName = "manager"

	// posturePolicyOpen is the historical default: allow-all ingress on the
	// server port when the server declares no source of its own.
	posturePolicyOpen = "open"
	// posturePolicyRestricted is the least-privilege default the product ships
	// with downstream: deny-by-default ingress plus operator-derived peers.
	posturePolicyRestricted = "restricted"
)

// networkPolicyPosture reports the operand NetworkPolicy default posture the
// assertions should expect. E2E_NETWORK_POLICY_POSTURE, when set, is an explicit
// override and wins (an unrecognized value fails the test). When it is
// unset/empty the posture is auto-detected from the live operator, so any runner
// self-adjusts to whatever posture the operand was actually deployed with without
// having to set the env var.
func networkPolicyPosture(ctx context.Context, t *testing.T, cfg *envconf.Config) string {
	t.Helper()
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(envNetworkPolicyPosture))); v != "" {
		switch v {
		case posturePolicyOpen:
			return posturePolicyOpen
		case posturePolicyRestricted:
			return posturePolicyRestricted
		default:
			t.Fatalf("invalid %s %q: must be %q or %q", envNetworkPolicyPosture, v, posturePolicyOpen, posturePolicyRestricted)
			return ""
		}
	}
	return detectOperandPosture(ctx, t, cfg)
}

// detectOperandPosture resolves the effective NetworkPolicy default posture from
// the live operator by scanning the controller-manager Deployment's manager
// container args for --network-policy-default-posture. The Deployment arg is the
// source of truth for the default posture; an absent flag resolves to "open", the
// operator's built-in default. The operator Deployment is located via the
// discovery framework (its namespace) and the same control-plane label selector
// the framework already uses for the operator pod.
func detectOperandPosture(ctx context.Context, t *testing.T, cfg *envconf.Config) string {
	t.Helper()
	ns := f.MustDiscoverOperatorOnce(ctx, cfg, t).Namespace
	if ns == "" {
		t.Fatal("could not resolve operator namespace to detect the NetworkPolicy posture")
	}

	var deployments appsv1.DeploymentList
	if err := cfg.Client().Resources(ns).List(ctx, &deployments,
		resources.WithLabelSelector(f.OperatorControllerSelector),
	); err != nil {
		t.Fatalf("listing operator deployments in %s: %v", ns, err)
	}
	if len(deployments.Items) == 0 {
		t.Fatalf("no operator controller-manager deployment found in %s", ns)
	}

	deploy := &deployments.Items[0]
	args, ok := managerContainerArgs(deploy)
	if !ok {
		// Fail rather than default to "open": a missing manager container means
		// detection is broken, and silently asserting the laxer posture would let
		// a real restricted-vs-open regression pass. If this ever fires because
		// the container was renamed or wrapped, set E2E_NETWORK_POLICY_POSTURE to
		// assert explicitly.
		t.Fatalf("operator deployment %s/%s has no %q container to read the NetworkPolicy posture from",
			ns, deploy.Name, managerContainerName)
	}
	posture, err := postureFromArgs(args)
	if err != nil {
		t.Fatalf("detecting NetworkPolicy posture from operator deployment %s/%s: %v", ns, deploy.Name, err)
	}
	t.Logf("auto-detected operand NetworkPolicy posture %q from deployment %s/%s", posture, ns, deploy.Name)
	return posture
}

// managerContainerArgs returns the args of the manager container and whether that
// container exists. The caller distinguishes "no manager container" (a detection
// failure) from "manager container with no posture flag" (a legitimate default).
func managerContainerArgs(deploy *appsv1.Deployment) ([]string, bool) {
	for _, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name == managerContainerName {
			return c.Args, true
		}
	}
	return nil, false
}

// postureFromArgs scans container args for the posture flag in the --flag=value
// form (leading dashes are ignored, so -flag=value matches too). That is the only
// form the operator is ever deployed with: the restricted overlay appends
// --network-policy-default-posture=restricted, and the base manager args are all
// =value or boolean flags. A flag that is absent entirely resolves to "open" (the
// operator's built-in default). The last occurrence wins, mirroring flag parsing.
func postureFromArgs(args []string) (string, error) {
	raw := ""
	found := false
	for _, a := range args {
		if v, ok := strings.CutPrefix(strings.TrimLeft(a, "-"), networkPolicyPostureFlag+"="); ok {
			raw, found = v, true
		}
	}
	if !found {
		return posturePolicyOpen, nil
	}
	switch v := strings.ToLower(strings.TrimSpace(raw)); v {
	case "", posturePolicyOpen:
		return posturePolicyOpen, nil
	case posturePolicyRestricted:
		return posturePolicyRestricted, nil
	default:
		return "", fmt.Errorf("unrecognized --%s value %q", networkPolicyPostureFlag, raw)
	}
}

// ingressAdmitsNamespace reports whether any peer in the ingress rule selects
// the given namespace by its immutable kubernetes.io/metadata.name label, which
// is how the controller pins the operator self-peer. It deliberately ignores any
// further narrowing (e.g. a pod selector) and any additional peers, so the check
// stays a loose "a self-peer is present" assertion.
func ingressAdmitsNamespace(rule networkingv1.NetworkPolicyIngressRule, namespace string) bool {
	for _, peer := range rule.From {
		if peer.NamespaceSelector == nil {
			continue
		}
		if peer.NamespaceSelector.MatchLabels[corev1.LabelMetadataName] == namespace {
			return true
		}
	}
	return false
}

func TestNetworkPolicyCreated(t *testing.T) {
	t.Parallel()
	feature := features.New("MCPServer creates NetworkPolicy").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Fast).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "netpol-test", true)
		}).
		Assess("NetworkPolicy exists with correct spec", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			netpol := &networkingv1.NetworkPolicy{}
			if err := r.Get(ctx, server.Name, server.Namespace, netpol); err != nil {
				t.Fatalf("NetworkPolicy not found: %v", err)
			}

			// Verify podSelector targets MCP server pods
			if val, ok := netpol.Spec.PodSelector.MatchLabels["mcp-server"]; !ok || val != server.Name {
				t.Fatalf("expected podSelector {mcp-server: %s}, got %v", server.Name, netpol.Spec.PodSelector.MatchLabels)
			}

			// Verify Ingress and Egress policyTypes
			if len(netpol.Spec.PolicyTypes) != 2 {
				t.Fatalf("expected 2 policyTypes, got %d: %v", len(netpol.Spec.PolicyTypes), netpol.Spec.PolicyTypes)
			}
			hasIngress, hasEgress := false, false
			for _, pt := range netpol.Spec.PolicyTypes {
				switch pt {
				case networkingv1.PolicyTypeIngress:
					hasIngress = true
				case networkingv1.PolicyTypeEgress:
					hasEgress = true
				}
			}
			if !hasIngress || !hasEgress {
				t.Fatalf("expected policyTypes [Ingress, Egress], got %v", netpol.Spec.PolicyTypes)
			}

			// Verify ingress allows only the configured port
			expectedPort := int(server.Spec.Config.Port)
			if len(netpol.Spec.Ingress) != 1 {
				t.Fatalf("expected 1 ingress rule, got %d", len(netpol.Spec.Ingress))
			}
			rule := netpol.Spec.Ingress[0]
			if len(rule.Ports) != 1 {
				t.Fatalf("expected 1 port in ingress rule, got %d", len(rule.Ports))
			}
			if rule.Ports[0].Port.IntValue() != expectedPort {
				t.Fatalf("expected ingress port %d, got %d", expectedPort, rule.Ports[0].Port.IntValue())
			}
			if *rule.Ports[0].Protocol != corev1.ProtocolTCP {
				t.Fatalf("expected protocol TCP, got %s", *rule.Ports[0].Protocol)
			}

			// Verify ingress From according to the posture the operator was
			// deployed with. Under "open" (historical default) the default rule
			// has no source, so any client may reach the server port. Under
			// "restricted" the operator cannot leave a server deny-by-default
			// (its own verification handshake would be black-holed), so it
			// derives at least its own controller pod as an ingress peer.
			switch networkPolicyPosture(ctx, t, cfg) {
			case posturePolicyOpen:
				if len(rule.From) != 0 {
					t.Fatalf("expected empty From (allow all sources), got %d entries", len(rule.From))
				}
			case posturePolicyRestricted:
				// Loose on purpose: assert only that a peer selecting the
				// operator namespace is present, not an exact count or single
				// entry, so a later change that adds another derived peer (e.g.
				// an apiserver-proxy peer) does not break this.
				operatorNs := f.MustDiscoverOperatorOnce(ctx, cfg, t).Namespace
				if operatorNs == "" {
					t.Fatal("restricted posture: could not resolve operator namespace to locate the self-peer")
				}
				if !ingressAdmitsNamespace(rule, operatorNs) {
					t.Fatalf("restricted posture: expected an ingress peer selecting the operator namespace %q, got From=%#v",
						operatorNs, rule.From)
				}
				t.Logf("restricted posture: default NetworkPolicy admits the operator self-peer (namespace %q)", operatorNs)
			}

			// Verify egress allows all traffic
			if len(netpol.Spec.Egress) != 1 {
				t.Fatalf("expected 1 egress rule, got %d", len(netpol.Spec.Egress))
			}
			egressRule := netpol.Spec.Egress[0]
			if len(egressRule.Ports) != 0 {
				t.Fatalf("expected empty egress ports (allow all), got %d", len(egressRule.Ports))
			}
			if len(egressRule.To) != 0 {
				t.Fatalf("expected empty egress To (allow all destinations), got %d", len(egressRule.To))
			}

			// Verify owner reference
			if len(netpol.OwnerReferences) == 0 {
				t.Fatal("expected owner reference on NetworkPolicy")
			}
			if netpol.OwnerReferences[0].Name != server.Name {
				t.Fatalf("expected owner %s, got %s", server.Name, netpol.OwnerReferences[0].Name)
			}

			t.Logf("NetworkPolicy %s exists with correct spec: podSelector={mcp-server: %s}, port=8080/TCP, ingress+egress",
				netpol.Name, server.Name)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestNetworkPolicyPortUpdate(t *testing.T) {
	t.Parallel()
	feature := features.New("MCPServer NetworkPolicy port update").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "netpol-port", true)
		}).
		Assess("update port from 8080 to 3002", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.UpdateWithRetry(ctx, t, r, server, func(s *mcpv1beta1.MCPServer) {
				s.Spec.Config.Port = 3002
			})
			t.Log("updated MCPServer port to 3002")

			return ctx
		}).
		Assess("NetworkPolicy reflects updated port after reconciliation", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			netpol := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
			}
			err := wait.For(
				conditions.New(r).ResourceMatch(netpol, func(obj k8s.Object) bool {
					np := obj.(*networkingv1.NetworkPolicy)
					if len(np.Spec.Ingress) != 1 || len(np.Spec.Ingress[0].Ports) != 1 {
						return false
					}
					return np.Spec.Ingress[0].Ports[0].Port.IntValue() == 3002
				}),
				wait.WithTimeout(1*time.Minute),
				wait.WithInterval(2*time.Second),
				wait.WithContext(ctx),
			)
			if err != nil {
				t.Fatalf("NetworkPolicy port did not update to 3002: %v", err)
			}

			t.Log("NetworkPolicy port updated to 3002")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestNetworkPolicyGarbageCollected(t *testing.T) {
	t.Parallel()
	feature := features.New("MCPServer NetworkPolicy garbage collection").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "netpol-gc", true)
		}).
		Assess("NetworkPolicy exists", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			netpol := &networkingv1.NetworkPolicy{}
			if err := r.Get(ctx, server.Name, server.Namespace, netpol); err != nil {
				t.Fatalf("NetworkPolicy not found: %v", err)
			}
			t.Logf("NetworkPolicy %s exists", netpol.Name)
			return ctx
		}).
		Assess("NetworkPolicy is deleted when MCPServer is deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Delete(ctx, server); err != nil {
				t.Fatalf("failed to delete MCPServer: %v", err)
			}
			t.Logf("deleted MCPServer %s/%s", server.Namespace, server.Name)

			netpol := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
			}
			err := wait.For(
				conditions.New(r).ResourceDeleted(netpol),
				wait.WithTimeout(1*time.Minute),
				wait.WithInterval(2*time.Second),
				wait.WithContext(ctx),
			)
			if err != nil {
				t.Fatalf("NetworkPolicy was not garbage collected: %v", err)
			}
			t.Log("NetworkPolicy was garbage collected")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
