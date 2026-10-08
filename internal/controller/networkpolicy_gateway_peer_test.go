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
	"errors"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func gatewayServer(configRef string, network *mcpv1beta1.NetworkConfig) *mcpv1beta1.MCPServer {
	s := postureTestServer("gw-server", network)
	s.Spec.Gateway = &mcpv1beta1.GatewaySpec{Provider: "httproute", ConfigRef: configRef}
	return s
}

func gatewayNamespacePeer(namespace string) []networkingv1.NetworkPolicyPeer {
	return []networkingv1.NetworkPolicyPeer{
		{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{namespaceNameLabel: namespace},
			},
		},
	}
}

// TestDefaultIngressRulesGatewayPeer covers how a resolved gateway peer flows
// into the ingress rules. The resolution itself is covered by
// TestGatewayIngressPeers; here the peer is supplied directly.
func TestDefaultIngressRulesGatewayPeer(t *testing.T) {
	peer := gatewayNamespacePeer("gateway-system")
	userPeer := []networkingv1.NetworkPolicyPeer{
		{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "client"}}},
	}

	tt := []struct {
		name         string
		server       *mcpv1beta1.MCPServer
		posture      NetworkPolicyDefaultPosture
		gatewayPeers []networkingv1.NetworkPolicyPeer
		want         []networkingv1.NetworkPolicyIngressRule
	}{
		{
			name:         "restricted with gateway peer admits the gateway namespace on the server port",
			server:       gatewayServer("gw-config", nil),
			posture:      PostureRestricted,
			gatewayPeers: peer,
			want: []networkingv1.NetworkPolicyIngressRule{
				{Ports: ingressPorts(gatewayServer("gw-config", nil)), From: peer},
			},
		},
		{
			name:         "restricted without a gateway peer stays deny-by-default",
			server:       gatewayServer("gw-config", nil),
			posture:      PostureRestricted,
			gatewayPeers: nil,
			want:         []networkingv1.NetworkPolicyIngressRule{},
		},
		{
			name:         "explicit IngressFrom is honored and the gateway peer is ignored",
			server:       gatewayServer("gw-config", &mcpv1beta1.NetworkConfig{IngressFrom: userPeer}),
			posture:      PostureRestricted,
			gatewayPeers: peer,
			want: []networkingv1.NetworkPolicyIngressRule{
				{Ports: ingressPorts(gatewayServer("gw-config", nil)), From: userPeer},
			},
		},
		{
			name:         "open posture ignores the gateway peer (any source already reaches the port)",
			server:       gatewayServer("gw-config", nil),
			posture:      PostureOpen,
			gatewayPeers: peer,
			want: []networkingv1.NetworkPolicyIngressRule{
				{Ports: ingressPorts(gatewayServer("gw-config", nil))},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultIngressRules(tc.server, tc.posture, tc.gatewayPeers)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("defaultIngressRules() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestGatewayIngressPeers(t *testing.T) {
	const gwNamespace = "gateway-system"

	configMap := func(name string, data map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Data:       data,
		}
	}

	tt := []struct {
		name    string
		posture NetworkPolicyDefaultPosture
		server  *mcpv1beta1.MCPServer
		objects []client.Object
		want    []networkingv1.NetworkPolicyPeer
	}{
		{
			name:    "open posture never derives a peer",
			posture: PostureOpen,
			server:  gatewayServer("gw-config", nil),
			objects: []client.Object{configMap("gw-config", map[string]string{gatewayConfigKeyNamespace: gwNamespace})},
			want:    nil,
		},
		{
			name:    "not gateway-routed",
			posture: PostureRestricted,
			server:  postureTestServer("plain", nil),
			want:    nil,
		},
		{
			name:    "non-httproute provider stays deny-by-default even with a resolvable ConfigMap",
			posture: PostureRestricted,
			server: func() *mcpv1beta1.MCPServer {
				s := gatewayServer("gw-config", nil)
				s.Spec.Gateway.Provider = "kuadrant"
				return s
			}(),
			objects: []client.Object{configMap("gw-config", map[string]string{gatewayConfigKeyNamespace: gwNamespace})},
			want:    nil,
		},
		{
			name:    "explicit IngressFrom takes precedence over derivation",
			posture: PostureRestricted,
			server: gatewayServer("gw-config", &mcpv1beta1.NetworkConfig{
				IngressFrom: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
			}),
			objects: []client.Object{configMap("gw-config", map[string]string{gatewayConfigKeyNamespace: gwNamespace})},
			want:    nil,
		},
		{
			name:    "no configRef cannot be resolved",
			posture: PostureRestricted,
			server:  gatewayServer("", nil),
			want:    nil,
		},
		{
			name:    "missing ConfigMap falls back to deny-by-default",
			posture: PostureRestricted,
			server:  gatewayServer("absent", nil),
			want:    nil,
		},
		{
			name:    "ConfigMap without the gateway-namespace key",
			posture: PostureRestricted,
			server:  gatewayServer("gw-config", nil),
			objects: []client.Object{configMap("gw-config", map[string]string{"route-hostname": "mcp.example.com"})},
			want:    nil,
		},
		{
			name:    "resolves the gateway namespace into a namespaceSelector peer",
			posture: PostureRestricted,
			server:  gatewayServer("gw-config", nil),
			objects: []client.Object{configMap("gw-config", map[string]string{gatewayConfigKeyNamespace: gwNamespace})},
			want:    gatewayNamespacePeer(gwNamespace),
		},
		{
			name:    "trims whitespace around the resolved namespace",
			posture: PostureRestricted,
			server:  gatewayServer("gw-config", nil),
			objects: []client.Object{configMap("gw-config", map[string]string{gatewayConfigKeyNamespace: "  " + gwNamespace + "  "})},
			want:    gatewayNamespacePeer(gwNamespace),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.objects...).
				Build()
			r := &MCPServerReconciler{Client: fakeClient, APIReader: fakeClient, NetworkPolicyDefaultPosture: tc.posture}

			got, err := r.gatewayIngressPeers(context.Background(), tc.server)
			if err != nil {
				t.Fatalf("gatewayIngressPeers() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("gatewayIngressPeers() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// getErrorClient wraps a client so every Get returns a fixed error, letting the
// test exercise the transient-read-failure path without the interceptor package
// (which this repo does not vendor).
type getErrorClient struct {
	client.Client
	err error
}

func (c getErrorClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	return c.err
}

// TestGatewayIngressPeersPropagatesGetError verifies that a non-NotFound failure
// to read the gateway ConfigMap is propagated instead of collapsing to
// deny-by-default, so a transient read blip cannot overwrite an existing
// gateway-allowing policy.
func TestGatewayIngressPeersPropagatesGetError(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	errClient := getErrorClient{Client: base, err: errors.New("boom")}
	r := &MCPServerReconciler{
		Client:                      errClient,
		APIReader:                   errClient,
		NetworkPolicyDefaultPosture: PostureRestricted,
	}

	got, err := r.gatewayIngressPeers(context.Background(), gatewayServer("gw-config", nil))
	if err == nil {
		t.Fatalf("gatewayIngressPeers() expected an error, got peers %#v", got)
	}
	if got != nil {
		t.Fatalf("gatewayIngressPeers() expected nil peers on error, got %#v", got)
	}
}
