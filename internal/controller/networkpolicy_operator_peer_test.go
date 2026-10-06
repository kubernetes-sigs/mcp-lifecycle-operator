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
	"reflect"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func operatorSelfPeer(namespace string, podLabels map[string]string) []networkingv1.NetworkPolicyPeer {
	peer := networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{namespaceNameLabel: namespace},
		},
	}
	if len(podLabels) > 0 {
		peer.PodSelector = &metav1.LabelSelector{MatchLabels: podLabels}
	}
	return []networkingv1.NetworkPolicyPeer{peer}
}

func TestOperatorIngressPeers(t *testing.T) {
	const operatorNS = "mcp-operator-system"
	operatorLabels := map[string]string{"control-plane": "controller-manager"}

	userIngress := &mcpv1beta1.NetworkConfig{
		IngressFrom: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
	}

	tt := []struct {
		name       string
		posture    NetworkPolicyDefaultPosture
		operatorNS string
		podLabels  map[string]string
		server     *mcpv1beta1.MCPServer
		want       []networkingv1.NetworkPolicyPeer
	}{
		{
			name:       "open posture never derives a peer",
			posture:    PostureOpen,
			operatorNS: operatorNS,
			podLabels:  operatorLabels,
			server:     postureTestServer("plain", nil),
			want:       nil,
		},
		{
			name:       "restricted with namespace and pod labels selects the operator pod",
			posture:    PostureRestricted,
			operatorNS: operatorNS,
			podLabels:  operatorLabels,
			server:     postureTestServer("plain", nil),
			want:       operatorSelfPeer(operatorNS, operatorLabels),
		},
		{
			name:       "restricted without pod labels stays deny-by-default",
			posture:    PostureRestricted,
			operatorNS: operatorNS,
			podLabels:  nil,
			server:     postureTestServer("plain", nil),
			want:       nil,
		},
		{
			name:       "explicit IngressFrom takes precedence over derivation",
			posture:    PostureRestricted,
			operatorNS: operatorNS,
			podLabels:  operatorLabels,
			server:     postureTestServer("plain", userIngress),
			want:       nil,
		},
		{
			name:       "unknown operator namespace stays deny-by-default",
			posture:    PostureRestricted,
			operatorNS: "",
			podLabels:  operatorLabels,
			server:     postureTestServer("plain", nil),
			want:       nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			r := &MCPServerReconciler{
				NetworkPolicyDefaultPosture: tc.posture,
				OperatorNamespace:           tc.operatorNS,
				OperatorPodLabels:           tc.podLabels,
			}

			got := r.operatorIngressPeers(context.Background(), tc.server)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("operatorIngressPeers() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestOperatorIngressPeersCopiesPodLabels verifies the derived peer does not
// alias the reconciler's OperatorPodLabels map, so a later mutation of the
// returned selector cannot corrupt the operator identity used by the next
// reconcile.
func TestOperatorIngressPeersCopiesPodLabels(t *testing.T) {
	labels := map[string]string{"control-plane": "controller-manager"}
	r := &MCPServerReconciler{
		NetworkPolicyDefaultPosture: PostureRestricted,
		OperatorNamespace:           "mcp-operator-system",
		OperatorPodLabels:           labels,
	}

	got := r.operatorIngressPeers(context.Background(), postureTestServer("plain", nil))
	got[0].PodSelector.MatchLabels["control-plane"] = "tampered"

	if labels["control-plane"] != "controller-manager" {
		t.Fatalf("operatorIngressPeers() aliased OperatorPodLabels; source map was mutated to %q",
			labels["control-plane"])
	}
}
