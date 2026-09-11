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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

const testGatewayURL = "https://gateway.example.com/mcp"

var _ = Describe("MCPServer Controller - Gateway", func() {
	ctx := context.Background()

	Context("reconcileGatewayBinding", func() {
		const resourceName = "test-gw-binding"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Gateway = &mcpv1beta1.GatewaySpec{
				Provider:  "httproute",
				ConfigRef: "gw-config",
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create an MCPGatewayBinding when spec.gateway is set", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)
			Expect(err).NotTo(HaveOccurred())
			Expect(binding.Spec.MCPServerRef).To(Equal(resourceName))
			Expect(binding.Spec.Provider).To(Equal("httproute"))
			Expect(binding.Spec.ConfigRef).To(Equal("gw-config"))

			ownerRef := metav1.GetControllerOf(binding)
			Expect(ownerRef).NotTo(BeNil())
			Expect(ownerRef.Name).To(Equal(resourceName))
			Expect(ownerRef.Kind).To(Equal(mcpv1alpha1.MCPServerKind))
		})

		It("should delete MCPGatewayBinding when spec.gateway is removed", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			mcpServer.Spec.Gateway = nil
			err = reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())
		})

		It("should update MCPGatewayBinding when configRef changes", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			mcpServer.Spec.Gateway.ConfigRef = "new-config"
			err = reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)
			Expect(err).NotTo(HaveOccurred())
			Expect(binding.Spec.ConfigRef).To(Equal("new-config"))
		})

		It("should recreate MCPGatewayBinding when provider changes", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)
			Expect(err).NotTo(HaveOccurred())
			oldUID := binding.UID

			mcpServer.Spec.Gateway.Provider = "custom-provider"

			// First call deletes the old binding; creation deferred to next reconcile.
			err = reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			// Second call creates the new binding.
			err = reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)
			Expect(err).NotTo(HaveOccurred())
			Expect(binding.Spec.Provider).To(Equal("custom-provider"))
			Expect(binding.UID).NotTo(Equal(oldUID))
		})

		It("should be a no-op when spec.gateway is nil and no binding exists", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			mcpServer.Spec.Gateway = nil

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("reconcileGatewayCondition", func() {
		const resourceName = "test-gw-condition"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Gateway = &mcpv1beta1.GatewaySpec{
				Provider: "httproute",
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should return nil when spec.gateway is nil", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Gateway = nil

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			status := reconciler.reconcileGatewayCondition(ctx, mcpServer)
			Expect(status).To(BeNil())
		})

		It("should return BindingNotFound when binding doesn't exist", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			status := reconciler.reconcileGatewayCondition(ctx, mcpServer)
			Expect(status).NotTo(BeNil())
			Expect(status.condition.Type).To(Equal(ConditionTypeGatewayRegistered))
			Expect(status.condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(status.condition.Reason).To(Equal(ReasonGatewayBindingNotFound))
		})

		It("should return NotRegistered when binding exists but Registered condition is missing", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			status := reconciler.reconcileGatewayCondition(ctx, mcpServer)
			Expect(status).NotTo(BeNil())
			Expect(status.condition.Type).To(Equal(ConditionTypeGatewayRegistered))
			Expect(status.condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(status.condition.Reason).To(Equal(ReasonGatewayNotRegistered))
			Expect(status.bindingStatus).NotTo(BeNil())
			Expect(status.bindingStatus.Provider).To(Equal("httproute"))
		})

		It("should return NotRegistered with binding message when Registered=False", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)).To(Succeed())

			meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeRegistered,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonGatewayNotRegistered,
				Message:            "ConfigMap not found",
				LastTransitionTime: metav1.Now(),
			})
			Expect(k8sClient.Status().Update(ctx, binding)).To(Succeed())

			status := reconciler.reconcileGatewayCondition(ctx, mcpServer)
			Expect(status).NotTo(BeNil())
			Expect(status.condition.Type).To(Equal(ConditionTypeGatewayRegistered))
			Expect(status.condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(status.condition.Reason).To(Equal(ReasonGatewayNotRegistered))
			Expect(status.condition.Message).To(Equal("ConfigMap not found"))
			Expect(status.bindingStatus).NotTo(BeNil())
		})

		It("should return NotRegistered when Registered=True has stale ObservedGeneration", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)).To(Succeed())

			meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeRegistered,
				Status:             metav1.ConditionTrue,
				Reason:             ReasonGatewayRegistered,
				Message:            "HTTPRoute created",
				ObservedGeneration: binding.Generation - 1,
				LastTransitionTime: metav1.Now(),
			})
			Expect(k8sClient.Status().Update(ctx, binding)).To(Succeed())

			status := reconciler.reconcileGatewayCondition(ctx, mcpServer)
			Expect(status).NotTo(BeNil())
			Expect(status.condition.Type).To(Equal(ConditionTypeGatewayRegistered))
			Expect(status.condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(status.condition.Reason).To(Equal(ReasonGatewayNotRegistered))
		})

		It("should return Registered when binding has Registered=True", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)).To(Succeed())

			meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeRegistered,
				Status:             metav1.ConditionTrue,
				Reason:             ReasonGatewayRegistered,
				Message:            "HTTPRoute created",
				ObservedGeneration: binding.Generation,
				LastTransitionTime: metav1.Now(),
			})
			binding.Status.URL = testGatewayURL
			Expect(k8sClient.Status().Update(ctx, binding)).To(Succeed())

			status := reconciler.reconcileGatewayCondition(ctx, mcpServer)
			Expect(status).NotTo(BeNil())
			Expect(status.condition.Type).To(Equal(ConditionTypeGatewayRegistered))
			Expect(status.condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(status.condition.Reason).To(Equal(ReasonGatewayRegistered))
			Expect(status.gatewayAddress).To(Equal(testGatewayURL))
			Expect(status.bindingStatus).NotTo(BeNil())
			Expect(status.bindingStatus.Name).To(Equal(gatewayBindingName(resourceName)))
		})
	})

	Context("reconcileGatewayBinding ownership conflict", func() {
		const resourceName = "test-gw-ownership-bind"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Gateway = &mcpv1beta1.GatewaySpec{
				Provider:  "httproute",
				ConfigRef: "gw-config",
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			binding := &mcpv1alpha1.MCPGatewayBinding{}
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name: gatewayBindingName(resourceName), Namespace: "default",
			}, binding); err == nil {
				_ = k8sClient.Delete(ctx, binding)
			}
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should return error when existing binding is owned by a different controller", func() {
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			err := reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)).To(Succeed())

			binding.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "ConfigMap",
					Name:               "foreign-owner",
					UID:                "foreign-uid",
					Controller:         ptr.To(true), //nolint:modernize // new(bool) yields false, not true
					BlockOwnerDeletion: ptr.To(true), //nolint:modernize // new(bool) yields false, not true
				},
			}
			Expect(k8sClient.Update(ctx, binding)).To(Succeed())

			err = reconciler.reconcileGatewayBinding(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			Expect(IsOwnershipConflict(err)).To(BeTrue())
		})
	})

	Context("full reconcile with gateway", func() {
		const resourceName = "test-gw-reconcile"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Gateway = &mcpv1beta1.GatewaySpec{
				Provider: "httproute",
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set MCPServer Ready=False when gateway is configured but binding not registered", func() {
			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			readyCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(ReasonGatewayNotRegistered))

			gwCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeGatewayRegistered)
			Expect(gwCond).NotTo(BeNil())
			Expect(gwCond.Status).To(Equal(metav1.ConditionFalse))

			Expect(mcpServer.Status.GatewayBinding).NotTo(BeNil())
			Expect(mcpServer.Status.GatewayBinding.Provider).To(Equal("httproute"))
		})

		It("should not publish address until server is verified even when binding is registered", func() {
			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)).To(Succeed())

			meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeRegistered,
				Status:             metav1.ConditionTrue,
				Reason:             ReasonGatewayRegistered,
				Message:            "HTTPRoute created",
				ObservedGeneration: binding.Generation,
				LastTransitionTime: metav1.Now(),
			})
			binding.Status.URL = testGatewayURL
			Expect(k8sClient.Status().Update(ctx, binding)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			gwCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeGatewayRegistered)
			Expect(gwCond).NotTo(BeNil())
			Expect(gwCond.Status).To(Equal(metav1.ConditionTrue))

			Expect(mcpServer.Status.Address).To(BeNil())
		})

		It("should preserve gateway status when deployment reconciliation fails", func() {
			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			By("establishing gateway status via successful reconcile")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)).To(Succeed())

			meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeRegistered,
				Status:             metav1.ConditionTrue,
				Reason:             ReasonGatewayRegistered,
				Message:            "HTTPRoute created",
				ObservedGeneration: binding.Generation,
				LastTransitionTime: metav1.Now(),
			})
			binding.Status.URL = testGatewayURL
			Expect(k8sClient.Status().Update(ctx, binding)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			gwCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeGatewayRegistered)
			Expect(gwCond).NotTo(BeNil())
			Expect(gwCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(mcpServer.Status.GatewayBinding).NotTo(BeNil())

			By("triggering a deployment ownership conflict")
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name: resourceName, Namespace: "default",
			}, dep)).To(Succeed())
			dep.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "ConfigMap",
					Name:               "foreign-owner",
					UID:                "foreign-uid",
					Controller:         ptr.To(true), //nolint:modernize // new(bool) yields false, not true
					BlockOwnerDeletion: ptr.To(true), //nolint:modernize // new(bool) yields false, not true
				},
			}
			Expect(k8sClient.Update(ctx, dep)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying gateway status survived the deployment failure")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			gwCond = meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeGatewayRegistered)
			Expect(gwCond).NotTo(BeNil(), "GatewayRegistered condition should be preserved after deployment failure")
			Expect(gwCond.Status).To(Equal(metav1.ConditionTrue))

			Expect(mcpServer.Status.GatewayBinding).NotTo(BeNil(), "GatewayBinding status should be preserved after deployment failure")
			Expect(mcpServer.Status.GatewayBinding.Provider).To(Equal("httproute"))
		})

	})

	Context("full reconcile with gateway binding ownership conflict", func() {
		const resourceName = "test-gw-ownership"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Gateway = &mcpv1beta1.GatewaySpec{
				Provider: "httproute",
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			binding := &mcpv1alpha1.MCPGatewayBinding{}
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name: gatewayBindingName(resourceName), Namespace: "default",
			}, binding); err == nil {
				_ = k8sClient.Delete(ctx, binding)
			}
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Available=False when gateway binding has ownership conflict", func() {
			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)).To(Succeed())

			binding.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "ConfigMap",
					Name:               "foreign-owner",
					UID:                "foreign-uid",
					Controller:         ptr.To(true), //nolint:modernize // new(bool) yields false, not true
					BlockOwnerDeletion: ptr.To(true), //nolint:modernize // new(bool) yields false, not true
				},
			}
			Expect(k8sClient.Update(ctx, binding)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			availableCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCond).NotTo(BeNil())
			Expect(availableCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCond.Reason).To(Equal(ReasonGatewayNotRegistered))
		})
	})

	Context("validation error with gateway", func() {
		const resourceName = "test-gw-validation"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Gateway = &mcpv1beta1.GatewaySpec{
				Provider: "httproute",
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should preserve gateway status when permanent validation error occurs", func() {
			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			By("establishing gateway status via successful reconcile")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)).To(Succeed())

			meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeRegistered,
				Status:             metav1.ConditionTrue,
				Reason:             ReasonGatewayRegistered,
				Message:            "HTTPRoute created",
				ObservedGeneration: binding.Generation,
				LastTransitionTime: metav1.Now(),
			})
			binding.Status.URL = testGatewayURL
			Expect(k8sClient.Status().Update(ctx, binding)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			gwCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeGatewayRegistered)
			Expect(gwCond).NotTo(BeNil())
			Expect(gwCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(mcpServer.Status.GatewayBinding).NotTo(BeNil())

			By("triggering a permanent validation error via missing ConfigMap reference")
			mcpServer.Spec.Config.Storage = []mcpv1beta1.StorageMount{
				{
					Path: "/data",
					Source: mcpv1beta1.StorageSource{
						Type: mcpv1beta1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "nonexistent-cm",
							},
						},
					},
				},
			}
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying gateway status survived the validation error")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAccepted)
			Expect(acceptedCond).NotTo(BeNil())
			Expect(acceptedCond.Status).To(Equal(metav1.ConditionFalse))

			gwCond = meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeGatewayRegistered)
			Expect(gwCond).NotTo(BeNil(), "GatewayRegistered condition should be preserved after validation error")
			Expect(gwCond.Status).To(Equal(metav1.ConditionTrue))

			Expect(mcpServer.Status.GatewayBinding).NotTo(BeNil(), "GatewayBinding status should be preserved after validation error")
			Expect(mcpServer.Status.GatewayBinding.Provider).To(Equal("httproute"))
		})

		It("should clean up gateway binding when gateway removed despite validation error", func() {
			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			By("establishing a gateway binding via successful reconcile")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)).To(Succeed())

			By("removing gateway config AND introducing a validation error")
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Gateway = nil
			mcpServer.Spec.Config.Storage = []mcpv1beta1.StorageMount{
				{
					Path: "/data",
					Source: mcpv1beta1.StorageSource{
						Type: mcpv1beta1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "nonexistent-cm",
							},
						},
					},
				},
			}
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the binding was cleaned up even though validation failed")
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      gatewayBindingName(resourceName),
				Namespace: "default",
			}, binding)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(),
				"gateway binding should be deleted even when validation fails")
		})
	})

	Context("applyGatewayStatus", func() {
		It("should not override Ready reason when already False", func() {
			mcpServer := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-apply-gw",
					Namespace:  "default",
					Generation: 1,
				},
			}

			readyCondition := metav1.Condition{
				Type:               ConditionTypeAvailable,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonDeploymentUnavailable,
				Message:            "Deployment not available",
				ObservedGeneration: 1,
				LastTransitionTime: metav1.Now(),
			}

			gwStatus := &gatewayStatus{
				condition: metav1.Condition{
					Type:               ConditionTypeGatewayRegistered,
					Status:             metav1.ConditionFalse,
					Reason:             ReasonGatewayNotRegistered,
					Message:            "Waiting for registration",
					ObservedGeneration: 1,
					LastTransitionTime: metav1.Now(),
				},
			}

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			result, _ := reconciler.applyGatewayStatus(mcpServer, gwStatus, readyCondition, "http://svc:8080/mcp")

			Expect(result.Status).To(Equal(metav1.ConditionFalse))
			Expect(result.Reason).To(Equal(ReasonDeploymentUnavailable))
		})

		It("should override Ready when not already False", func() {
			mcpServer := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-apply-gw-override",
					Namespace:  "default",
					Generation: 1,
				},
			}

			readyCondition := metav1.Condition{
				Type:               ConditionTypeAvailable,
				Status:             metav1.ConditionTrue,
				Reason:             ReasonAvailable,
				Message:            "Available",
				ObservedGeneration: 1,
				LastTransitionTime: metav1.Now(),
			}

			gwStatus := &gatewayStatus{
				condition: metav1.Condition{
					Type:               ConditionTypeGatewayRegistered,
					Status:             metav1.ConditionFalse,
					Reason:             ReasonGatewayNotRegistered,
					Message:            "Waiting for registration",
					ObservedGeneration: 1,
					LastTransitionTime: metav1.Now(),
				},
			}

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			result, _ := reconciler.applyGatewayStatus(mcpServer, gwStatus, readyCondition, "http://svc:8080/mcp")

			Expect(result.Status).To(Equal(metav1.ConditionFalse))
			Expect(result.Reason).To(Equal(ReasonGatewayNotRegistered))
		})

		It("should override mcpURL when gateway provides an address", func() {
			mcpServer := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-apply-gw-url",
					Namespace:  "default",
					Generation: 1,
				},
			}

			readyCondition := metav1.Condition{
				Type:               ConditionTypeAvailable,
				Status:             metav1.ConditionTrue,
				Reason:             ReasonAvailable,
				Message:            "Available",
				ObservedGeneration: 1,
				LastTransitionTime: metav1.Now(),
			}

			gwStatus := &gatewayStatus{
				condition: metav1.Condition{
					Type:               ConditionTypeGatewayRegistered,
					Status:             metav1.ConditionTrue,
					Reason:             ReasonGatewayRegistered,
					Message:            "Registered",
					ObservedGeneration: 1,
					LastTransitionTime: metav1.Now(),
				},
				gatewayAddress: testGatewayURL,
			}

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			_, url := reconciler.applyGatewayStatus(mcpServer, gwStatus, readyCondition, "http://svc:8080/mcp")

			Expect(url).To(Equal(testGatewayURL))
		})
	})

	Context("emitGatewayBindingReconcileFailed", func() {
		It("should emit a warning event", func() {
			reconciler, fr := newReconcilerForTestWithFakeEvents(k8sClient, k8sClient.Scheme())

			mcpServer := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-emit-gw",
					Namespace: "default",
				},
			}

			reconciler.emitGatewayBindingReconcileFailed(mcpServer, "binding error")
			Expect(fr.Events).To(HaveLen(1))
		})

		It("should not panic when recorder is nil", func() {
			reconciler := &MCPServerReconciler{}
			mcpServer := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-emit-gw-nil",
					Namespace: "default",
				},
			}
			reconciler.emitGatewayBindingReconcileFailed(mcpServer, "binding error")
		})
	})

	Context("gatewayBindingName", func() {
		It("should append -gateway-binding suffix", func() {
			Expect(gatewayBindingName("my-server")).To(Equal("my-server-gateway-binding"))
		})

		It("should truncate and hash when name exceeds 253 chars", func() {
			longName := ""
			for len(longName) < 250 {
				longName += "a"
			}
			name := gatewayBindingName(longName)
			Expect(len(name)).To(BeNumerically("<=", 253))
			Expect(name).To(HaveSuffix(gatewayBindingSuffix))
		})
	})
})
