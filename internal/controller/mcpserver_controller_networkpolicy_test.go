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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Controller - reconcileNetworkPolicy", func() {
	const resourceName = "test-reconcile-netpol"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := newTestMCPServer(resourceName)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		}
	})

	It("should create a NetworkPolicy when none exists", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		err := reconciler.reconcileNetworkPolicy(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		netpol := &networkingv1.NetworkPolicy{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, netpol)
		Expect(err).NotTo(HaveOccurred())

		By("Verifying name and labels")
		Expect(netpol.Name).To(Equal(resourceName))
		Expect(netpol.Labels).To(HaveKeyWithValue("mcp-server", resourceName))

		By("Verifying podSelector targets MCP server pods")
		Expect(netpol.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("mcp-server", resourceName))

		By("Verifying only Ingress policyType is set")
		Expect(netpol.Spec.PolicyTypes).To(HaveLen(1))
		Expect(netpol.Spec.PolicyTypes[0]).To(Equal(networkingv1.PolicyTypeIngress))

		By("Verifying ingress allows only the configured port")
		Expect(netpol.Spec.Ingress).To(HaveLen(1))
		Expect(netpol.Spec.Ingress[0].Ports).To(HaveLen(1))
		Expect(netpol.Spec.Ingress[0].Ports[0].Port.IntValue()).To(Equal(8080))
		Expect(*netpol.Spec.Ingress[0].Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))

		By("Verifying ingress From is empty (all sources allowed on MCP port)")
		Expect(netpol.Spec.Ingress[0].From).To(BeEmpty())

		By("Verifying owner reference is set")
		Expect(netpol.OwnerReferences).To(HaveLen(1))
		Expect(netpol.OwnerReferences[0].Name).To(Equal(resourceName))
	})

	It("should not error when NetworkPolicy already exists", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		Expect(reconciler.reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())
		Expect(reconciler.reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())
	})
})

var _ = Describe("MCPServer Controller - NetworkPolicy Update", func() {
	Context("When port changes", func() {
		const resourceName = "test-netpol-update"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should update the NetworkPolicy ingress port when config.port changes", func() {
			resource := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &MCPServerReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				APIReader: k8sClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the initial NetworkPolicy port")
			netpol := &networkingv1.NetworkPolicy{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, netpol)).To(Succeed())
			Expect(netpol.Spec.Ingress).To(HaveLen(1))
			Expect(netpol.Spec.Ingress[0].Ports).To(HaveLen(1))
			Expect(netpol.Spec.Ingress[0].Ports[0].Port.IntValue()).To(Equal(8080))

			By("Updating the port in the MCPServer spec")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Config.Port = 9090
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again to pick up the port change")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the NetworkPolicy port was updated")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, netpol)).To(Succeed())
			Expect(netpol.Spec.Ingress).To(HaveLen(1))
			Expect(netpol.Spec.Ingress[0].Ports).To(HaveLen(1))
			Expect(netpol.Spec.Ingress[0].Ports[0].Port.IntValue()).To(Equal(9090))
		})
	})
})

var _ = Describe("MCPServer Controller - NetworkPolicy Reconciliation Failures", func() {
	const resourceName = "test-netpol-failure"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := newTestMCPServer(resourceName)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		}
	})

	It("should update status with NetworkPolicyUnavailable when creation fails", func() {
		By("Creating interceptor that returns error on NetworkPolicy Create")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*networkingv1.NetworkPolicy); ok {
					return fmt.Errorf("simulated networkpolicy creation failure")
				}
				return c.Create(ctx, obj, opts...)
			},
		})

		netpolFailureReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling with NetworkPolicy creation failure")
		_, err = netpolFailureReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated networkpolicy creation failure"))

		By("Verifying status is updated with NetworkPolicyUnavailable")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(acceptedCondition.Reason).To(Equal("Valid"))

		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonNetworkPolicyUnavailable))
		Expect(readyCondition.Message).To(ContainSubstring("Failed to reconcile NetworkPolicy"))
		Expect(readyCondition.Message).To(ContainSubstring("simulated networkpolicy creation failure"))

		Expect(mcpServer.Status.DeploymentName).To(Equal(resourceName))
	})

	It("should update status with NetworkPolicyUnavailable when update fails", func() {
		By("Initial reconcile to create resources")
		initialReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}
		_, err := initialReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying NetworkPolicy was created")
		netpol := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, netpol)).To(Succeed())

		By("Creating interceptor that returns error on NetworkPolicy Update")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*networkingv1.NetworkPolicy); ok {
					return fmt.Errorf("simulated networkpolicy update failure")
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		netpolFailureReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Updating MCPServer spec to trigger NetworkPolicy reconciliation")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Port = 9090
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling with NetworkPolicy update failure")
		_, err = netpolFailureReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated networkpolicy update failure"))

		By("Verifying status is updated with NetworkPolicyUnavailable")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(acceptedCondition.Reason).To(Equal("Valid"))

		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonNetworkPolicyUnavailable))
		Expect(readyCondition.Message).To(ContainSubstring("Failed to reconcile NetworkPolicy"))
		Expect(readyCondition.Message).To(ContainSubstring("simulated networkpolicy update failure"))

		Expect(mcpServer.Status.DeploymentName).To(Equal(resourceName))
	})
})

var _ = Describe("MCPServer Controller - NetworkPolicy Reconcile Events", func() {
	const resourceName = "test-netpol-events"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := newTestMCPServer(resourceName)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		}
	})

	It("should emit a Warning NetworkPolicyReconcileFailed event only when error message changes", func() {
		failMsg := "simulated networkpolicy creation failure"
		reconciler, fr := newReconcilerForTestWithFakeEvents(k8sClient, k8sClient.Scheme())

		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*networkingv1.NetworkPolicy); ok {
					return fmt.Errorf("%s", failMsg)
				}
				return c.Create(ctx, obj, opts...)
			},
		})
		reconciler.Client = interceptedClient

		By("First NetworkPolicy reconcile failure - Warning event emitted once")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())

		var netpolFailedEvent string
		Eventually(func(g Gomega) {
			for _, ev := range drainEvents(fr.Events) {
				if strings.Contains(ev, corev1.EventTypeWarning) && strings.Contains(ev, ReasonNetworkPolicyUnavailable) {
					netpolFailedEvent = ev
					break
				}
			}
			g.Expect(netpolFailedEvent).NotTo(BeEmpty())
			g.Expect(netpolFailedEvent).To(ContainSubstring(resourceName))
			g.Expect(netpolFailedEvent).To(ContainSubstring("Failed to reconcile NetworkPolicy"))
			g.Expect(netpolFailedEvent).To(ContainSubstring(failMsg))
		}).Should(Succeed())

		By("Second reconcile with same error - no duplicate event")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())
		Consistently(fr.Events, 300*time.Millisecond, 20*time.Millisecond).ShouldNot(Receive())

		By("Change error message - second Warning event emitted")
		failMsg = "simulated networkpolicy ownership failure"
		interceptedClient = interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*networkingv1.NetworkPolicy); ok {
					return fmt.Errorf("%s", failMsg)
				}
				return c.Create(ctx, obj, opts...)
			},
		})
		reconciler.Client = interceptedClient

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())

		var secondNetpolFailedEvent string
		Eventually(func(g Gomega) {
			for _, ev := range drainEvents(fr.Events) {
				if strings.Contains(ev, corev1.EventTypeWarning) && strings.Contains(ev, ReasonNetworkPolicyUnavailable) {
					secondNetpolFailedEvent = ev
					break
				}
			}
			g.Expect(secondNetpolFailedEvent).NotTo(BeEmpty())
			g.Expect(secondNetpolFailedEvent).To(ContainSubstring(resourceName))
			g.Expect(secondNetpolFailedEvent).To(ContainSubstring(failMsg))
			g.Expect(secondNetpolFailedEvent).NotTo(Equal(netpolFailedEvent))
		}).Should(Succeed())
	})
})

var _ = Describe("MCPServer Controller - NetworkPolicy ExtraLabels/ExtraAnnotations on creation", func() {
	ctx := context.Background()

	It("should apply ExtraLabels and ExtraAnnotations on initial NetworkPolicy creation", func() {
		mcpServer := newTestMCPServer("test-netpol-extra-metadata")
		mcpServer.Spec.ExtraLabels = map[string]string{
			"team": "platform",
			"env":  "staging",
		}
		mcpServer.Spec.ExtraAnnotations = map[string]string{
			"example.com/owner": "team-a",
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-netpol-extra-metadata", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		err := reconciler.reconcileNetworkPolicy(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		createdNetpol := &networkingv1.NetworkPolicy{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-netpol-extra-metadata",
			Namespace: "default",
		}, createdNetpol)
		Expect(err).NotTo(HaveOccurred())

		By("Verifying ExtraLabels on NetworkPolicy metadata")
		Expect(createdNetpol.Labels).To(HaveKeyWithValue("team", "platform"))
		Expect(createdNetpol.Labels).To(HaveKeyWithValue("env", "staging"))

		By("Verifying ExtraAnnotations on NetworkPolicy metadata")
		Expect(createdNetpol.Annotations).To(HaveKeyWithValue("example.com/owner", "team-a"))

		By("Verifying tracking annotations are set")
		Expect(createdNetpol.Annotations).To(HaveKey(managedExtraLabels))
		Expect(createdNetpol.Annotations).To(HaveKey(managedExtraAnnotations))
	})
})

var _ = Describe("MCPServer Controller - NetworkPolicy ExtraLabels/ExtraAnnotations update", func() {
	ctx := context.Background()

	It("should update ExtraLabels and ExtraAnnotations on existing NetworkPolicy", func() {
		mcpServer := newTestMCPServer("test-netpol-meta-update")
		mcpServer.Spec.ExtraLabels = map[string]string{
			"team": "platform",
		}
		mcpServer.Spec.ExtraAnnotations = map[string]string{
			"example.com/owner": "team-a",
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-netpol-meta-update", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Initial reconcile creates NetworkPolicy with labels/annotations")
		Expect(reconciler.reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())

		By("Changing ExtraLabels and ExtraAnnotations")
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "test-netpol-meta-update", Namespace: "default"}, mcpServer)).To(Succeed())
		mcpServer.Spec.ExtraLabels = map[string]string{
			"team": "infrastructure",
			"env":  "production",
		}
		mcpServer.Spec.ExtraAnnotations = map[string]string{
			"example.com/owner":   "team-b",
			"example.com/contact": "ops@example.com",
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling again to update NetworkPolicy metadata")
		Expect(reconciler.reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())

		updatedNetpol := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-netpol-meta-update",
			Namespace: "default",
		}, updatedNetpol)).To(Succeed())

		By("Verifying labels updated to new values")
		Expect(updatedNetpol.Labels).To(HaveKeyWithValue("team", "infrastructure"))
		Expect(updatedNetpol.Labels).To(HaveKeyWithValue("env", "production"))

		By("Verifying old annotations replaced and new annotations applied")
		Expect(updatedNetpol.Annotations).To(HaveKeyWithValue("example.com/owner", "team-b"))
		Expect(updatedNetpol.Annotations).To(HaveKeyWithValue("example.com/contact", "ops@example.com"))
	})

	It("should remove ExtraLabels and ExtraAnnotations when cleared from spec", func() {
		mcpServer := newTestMCPServer("test-netpol-meta-clear")
		mcpServer.Spec.ExtraLabels = map[string]string{
			"team": "platform",
		}
		mcpServer.Spec.ExtraAnnotations = map[string]string{
			"example.com/owner": "team-a",
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-netpol-meta-clear", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Initial reconcile creates NetworkPolicy with metadata")
		Expect(reconciler.reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())

		By("Clearing ExtraLabels and ExtraAnnotations from spec")
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "test-netpol-meta-clear", Namespace: "default"}, mcpServer)).To(Succeed())
		mcpServer.Spec.ExtraLabels = map[string]string{}
		mcpServer.Spec.ExtraAnnotations = map[string]string{}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling to clean up metadata")
		Expect(reconciler.reconcileNetworkPolicy(ctx, mcpServer)).To(Succeed())

		updatedNetpol := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-netpol-meta-clear",
			Namespace: "default",
		}, updatedNetpol)).To(Succeed())

		By("Verifying custom labels are removed")
		Expect(updatedNetpol.Labels).NotTo(HaveKey("team"))
		Expect(updatedNetpol.Labels).To(HaveKeyWithValue("mcp-server", "test-netpol-meta-clear"))

		By("Verifying custom annotations are removed")
		Expect(updatedNetpol.Annotations).NotTo(HaveKey("example.com/owner"))
	})
})
