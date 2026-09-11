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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

var _ = Describe("MCPServer Controller - NetworkPolicy posture condition", func() {
	ctx := context.Background()

	reconcileOnce := func(name string) *mcpv1beta1.MCPServer {
		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		mcpServer := &mcpv1beta1.MCPServer{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, mcpServer)).To(Succeed())
		return mcpServer
	}

	postureCondition := func(mcpServer *mcpv1beta1.MCPServer) *metav1.Condition {
		return meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeNetworkPolicyRestricted)
	}

	It("reports Unrestricted for an MCPServer with no network restrictions", func() {
		name := "posture-default"
		Expect(k8sClient.Create(ctx, newTestMCPServer(name))).To(Succeed())
		defer func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, mcpServer) == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		mcpServer := reconcileOnce(name)

		cond := postureCondition(mcpServer)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(ReasonNetworkPolicyUnrestricted))
		Expect(cond.ObservedGeneration).To(Equal(mcpServer.Generation))

		By("appearing exactly once (not duplicated across reconciles)")
		mcpServer = reconcileOnce(name)
		count := 0
		for _, c := range mcpServer.Status.Conditions {
			if c.Type == ConditionTypeNetworkPolicyRestricted {
				count++
			}
		}
		Expect(count).To(Equal(1))
	})

	It("reports Restricted when both ingress and egress are restricted", func() {
		name := "posture-restricted"
		mcpServer := newTestMCPServer(name)
		mcpServer.Spec.Network = &mcpv1beta1.NetworkConfig{
			IngressFrom: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"mcp-client": "true"}}},
			},
			EgressTo: []networkingv1.NetworkPolicyPeer{
				{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}},
			},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, mcpServer) }()

		got := reconcileOnce(name)
		cond := postureCondition(got)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(ReasonNetworkPolicyRestricted))
	})

	It("reports EgressUnrestricted when only ingress is restricted", func() {
		name := "posture-ingress-only"
		mcpServer := newTestMCPServer(name)
		mcpServer.Spec.Network = &mcpv1beta1.NetworkConfig{
			IngressFrom: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"mcp-client": "true"}}},
			},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, mcpServer) }()

		got := reconcileOnce(name)
		cond := postureCondition(got)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(ReasonNetworkPolicyEgressUnrestricted))
	})

	It("transitions Unrestricted -> Restricted and advances lastTransitionTime", func() {
		name := "posture-transition"
		Expect(k8sClient.Create(ctx, newTestMCPServer(name))).To(Succeed())
		defer func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, mcpServer) == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		first := reconcileOnce(name)
		firstCond := postureCondition(first)
		Expect(firstCond).NotTo(BeNil())
		Expect(firstCond.Reason).To(Equal(ReasonNetworkPolicyUnrestricted))
		firstStamp := firstCond.LastTransitionTime

		By("a no-op reconcile preserving lastTransitionTime")
		steady := reconcileOnce(name)
		Expect(postureCondition(steady).LastTransitionTime).To(Equal(firstStamp))

		By("restricting both directions")
		mcpServer := &mcpv1beta1.MCPServer{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, mcpServer)).To(Succeed())
		mcpServer.Spec.Network = &mcpv1beta1.NetworkConfig{
			IngressFrom: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"mcp-client": "true"}}},
			},
			EgressTo: []networkingv1.NetworkPolicyPeer{
				{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}},
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		changed := reconcileOnce(name)
		changedCond := postureCondition(changed)
		Expect(changedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(changedCond.Reason).To(Equal(ReasonNetworkPolicyRestricted))
		// On a real transition the stamp is recomputed to "now" and must never
		// move backwards. Strict advancement is asserted in the pure unit test
		// (TestNetworkPolicyPostureConditionPreservesTransitionTime); here the
		// two reconciles can land in the same wall-clock second, which serializes
		// to an equal metav1.Time, so we only assert monotonicity.
		Expect(changedCond.LastTransitionTime.Before(&firstStamp)).To(BeFalse())
	})

	It("keeps the posture condition when reconcile fails on invalid config", func() {
		// The posture is one of several conditions applied under a single field
		// manager via Server-Side Apply. A failure/short-circuit path that omits it
		// from the apply would prune the previously-set condition. This guards that
		// the invalid-config path still carries the posture forward.
		name := "posture-invalid-config"
		mcpServer := newTestMCPServer(name)
		mcpServer.Spec.Config.EnvFrom = []corev1.EnvFromSource{
			{ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "does-not-exist"},
			}},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, mcpServer) }()

		got := reconcileOnce(name)

		By("the reconcile landing on the invalid-config path")
		available := meta.FindStatusCondition(got.Status.Conditions, ConditionTypeAvailable)
		Expect(available).NotTo(BeNil())
		Expect(available.Reason).To(Equal(ReasonConfigurationInvalid))

		By("the NetworkPolicy posture condition still being present")
		cond := postureCondition(got)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal(ReasonNetworkPolicyUnrestricted))
	})

	It("does not gate readiness: a False posture does not set Available for a posture reason (FR-006)", func() {
		name := "posture-non-gating"
		Expect(k8sClient.Create(ctx, newTestMCPServer(name))).To(Succeed())
		defer func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, mcpServer) == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		mcpServer := reconcileOnce(name)

		By("the posture being False (Unrestricted)")
		Expect(postureCondition(mcpServer).Status).To(Equal(metav1.ConditionFalse))

		By("the Available condition being driven by deployment state, not the posture")
		available := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
		Expect(available).NotTo(BeNil())
		postureReasons := []string{
			ReasonNetworkPolicyRestricted,
			ReasonNetworkPolicyIngressUnrestricted,
			ReasonNetworkPolicyEgressUnrestricted,
			ReasonNetworkPolicyUnrestricted,
		}
		Expect(postureReasons).NotTo(ContainElement(available.Reason))
	})
})
