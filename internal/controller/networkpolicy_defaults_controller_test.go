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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

var _ = Describe("MCPServer Controller - NetworkPolicy Restricted Posture (ingress)", func() {
	ctx := context.Background()

	restrictedReconciler := func() *MCPServerReconciler {
		return &MCPServerReconciler{
			Client:                      k8sClient,
			Scheme:                      k8sClient.Scheme(),
			APIReader:                   k8sClient,
			NetworkPolicyDefaultPosture: PostureRestricted,
		}
	}

	It("should emit deny-by-default ingress when no IngressFrom is set", func() {
		mcpServer := newTestMCPServer("test-netpol-restricted-noingress")
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, mcpServer)
		}()

		Expect(restrictedReconciler().reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())

		netpol := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-netpol-restricted-noingress",
			Namespace: "default",
		}, netpol)).To(Succeed())

		By("Verifying ingress is empty (deny-by-default), not a source-open rule")
		Expect(netpol.Spec.Ingress).To(BeEmpty())

		By("Verifying Ingress is still declared in policyTypes")
		Expect(netpol.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))

		By("Verifying no fabricated source appears anywhere in ingress")
		for _, rule := range netpol.Spec.Ingress {
			for _, peer := range rule.From {
				Expect(peer.PodSelector == nil && peer.NamespaceSelector == nil && peer.IPBlock == nil).
					To(BeFalse(), "empty ingress peer must never be fabricated")
			}
		}
	})

	It("should honor IngressFrom unchanged under restricted posture", func() {
		mcpServer := newTestMCPServer("test-netpol-restricted-ingress")
		mcpServer.Spec.Network = &mcpv1beta1.NetworkConfig{
			IngressFrom: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"mcp-client": "true"},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, mcpServer)
		}()

		Expect(restrictedReconciler().reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())

		netpol := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-netpol-restricted-ingress",
			Namespace: "default",
		}, netpol)).To(Succeed())

		By("Verifying the declared source is honored with the server port")
		Expect(netpol.Spec.Ingress).To(HaveLen(1))
		Expect(netpol.Spec.Ingress[0].From).To(HaveLen(1))
		Expect(netpol.Spec.Ingress[0].From[0].NamespaceSelector).NotTo(BeNil())
		Expect(netpol.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels).To(
			HaveKeyWithValue("mcp-client", "true"))
		Expect(netpol.Spec.Ingress[0].Ports).To(HaveLen(1))
		Expect(netpol.Spec.Ingress[0].Ports[0].Port.IntValue()).To(Equal(8080))
		Expect(*netpol.Spec.Ingress[0].Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
	})

	It("should transition from deny-by-default to honored source when IngressFrom is added", func() {
		mcpServer := newTestMCPServer("test-netpol-restricted-transition")
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, mcpServer)
		}()

		reconciler := restrictedReconciler()

		By("Initial reconcile with no source yields deny-by-default")
		Expect(reconciler.reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())
		netpol := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-netpol-restricted-transition",
			Namespace: "default",
		}, netpol)).To(Succeed())
		Expect(netpol.Spec.Ingress).To(BeEmpty())

		By("Adding a source and reconciling again")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(mcpServer), mcpServer)).To(Succeed())
		mcpServer.Spec.Network = &mcpv1beta1.NetworkConfig{
			IngressFrom: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "agent"},
					},
				},
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())
		Expect(reconciler.reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-netpol-restricted-transition",
			Namespace: "default",
		}, netpol)).To(Succeed())
		Expect(netpol.Spec.Ingress).To(HaveLen(1))
		Expect(netpol.Spec.Ingress[0].From).To(HaveLen(1))
		Expect(netpol.Spec.Ingress[0].From[0].PodSelector).NotTo(BeNil())
	})
})
