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

// Package manifests holds cluster-independent conformance tests for the static
// deployment manifests. These run under `go test ./...` (no cluster required)
// and guard the operator NetworkPolicy contract: the controller-manager pod is
// deny-by-default on egress with an explicit DNS/apiserver/operand allow-list,
// and the existing ingress rules stay intact.
package manifests

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// operatorPolicyPath resolves config/network-policy/allow-metrics-traffic.yaml
// relative to this test file, so it works regardless of the test working dir.
func operatorPolicyPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	// this file lives in test/manifests/, repo root is two levels up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(root, "config", "network-policy", "allow-metrics-traffic.yaml")
}

func loadOperatorPolicy(t *testing.T) networkingv1.NetworkPolicy {
	t.Helper()
	data, err := os.ReadFile(operatorPolicyPath(t))
	if err != nil {
		t.Fatalf("read operator NetworkPolicy: %v", err)
	}
	var np networkingv1.NetworkPolicy
	if err := yaml.Unmarshal(data, &np); err != nil {
		t.Fatalf("unmarshal operator NetworkPolicy: %v", err)
	}
	return np
}

func hasPolicyType(types []networkingv1.PolicyType, want networkingv1.PolicyType) bool {
	for _, pt := range types {
		if pt == want {
			return true
		}
	}
	return false
}

// TestOperatorPolicyTypes asserts conformance assertion #1: the base policy
// declares both Ingress and Egress.
func TestOperatorPolicyTypes(t *testing.T) {
	np := loadOperatorPolicy(t)
	if !hasPolicyType(np.Spec.PolicyTypes, networkingv1.PolicyTypeIngress) {
		t.Errorf("policyTypes must include Ingress, got %v", np.Spec.PolicyTypes)
	}
	if !hasPolicyType(np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress) {
		t.Errorf("policyTypes must include Egress, got %v", np.Spec.PolicyTypes)
	}
}

// TestOperatorIngressPreserved asserts conformance assertion #4: the existing
// ingress rules (health 8081, webhook 9443, metrics 8443 from metrics:enabled)
// remain intact after adding egress.
func TestOperatorIngressPreserved(t *testing.T) {
	np := loadOperatorPolicy(t)

	ports := map[int32]bool{}
	metricsScoped := false
	for _, rule := range np.Spec.Ingress {
		for _, p := range rule.Ports {
			if p.Port != nil {
				ports[p.Port.IntVal] = true
			}
		}
		// The metrics rule (8443) must stay scoped to metrics:enabled namespaces.
		for _, peer := range rule.From {
			if peer.NamespaceSelector != nil &&
				peer.NamespaceSelector.MatchLabels["metrics"] == "enabled" {
				metricsScoped = true
			}
		}
	}

	for _, want := range []int32{8081, 8443, 9443} {
		if !ports[want] {
			t.Errorf("ingress must still allow port %d; ports present: %v", want, ports)
		}
	}
	if !metricsScoped {
		t.Error("ingress 8443 metrics rule must stay scoped to namespaces labeled metrics=enabled")
	}
}

// TestOperatorEgressAllowList asserts conformance assertions #2, #3 and the
// deny-by-default invariant: egress is an explicit allow-list (DNS, apiserver,
// operand pods) with no broad allow-all rule.
func TestOperatorEgressAllowList(t *testing.T) {
	np := loadOperatorPolicy(t)

	if len(np.Spec.Egress) == 0 {
		t.Fatal("egress must be a non-empty allow-list")
	}

	var (
		sawDNS      bool // E1
		sawAPI      bool // E2
		sawOperand  bool // E3
		sawAllowAll bool
	)

	for _, rule := range np.Spec.Egress {
		// An allow-all rule has neither peers nor port restrictions.
		if len(rule.To) == 0 && len(rule.Ports) == 0 {
			sawAllowAll = true
			continue
		}

		// E1 DNS: UDP+TCP on 53.
		for _, p := range rule.Ports {
			if p.Port != nil && p.Port.IntVal == 53 {
				sawDNS = true
			}
			if p.Port != nil && (p.Port.IntVal == 443 || p.Port.IntVal == 6443) {
				sawAPI = true
			}
		}

		// E3 operand: podSelector matching the mcp-server label via Exists.
		for _, peer := range rule.To {
			if peer.PodSelector == nil {
				continue
			}
			for _, expr := range peer.PodSelector.MatchExpressions {
				if expr.Key == "mcp-server" && expr.Operator == metav1.LabelSelectorOpExists {
					sawOperand = true
				}
			}
		}
	}

	if sawAllowAll {
		t.Error("egress must not contain an allow-all rule (empty to/ports) - it would defeat deny-by-default")
	}
	if !sawDNS {
		t.Error("egress must allow DNS (port 53) - E1")
	}
	if !sawAPI {
		t.Error("egress must allow the kube-apiserver (443/6443) - E2")
	}
	if !sawOperand {
		t.Error("egress must allow managed MCP server pods via the mcp-server label (Exists) - E3")
	}
}
