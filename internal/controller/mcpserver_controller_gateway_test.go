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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

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
			resource.Spec.Gateway = &mcpv1alpha1.GatewaySpec{
				ClassName: ProviderHTTPRoute,
				ConfigRef: "gw-config",
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create an MCPGatewayBinding when spec.gateway is set", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
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
			Expect(binding.Spec.Provider).To(Equal(ProviderHTTPRoute))
			Expect(binding.Spec.ConfigRef).To(Equal("gw-config"))

			ownerRef := metav1.GetControllerOf(binding)
			Expect(ownerRef).NotTo(BeNil())
			Expect(ownerRef.Name).To(Equal(resourceName))
			Expect(ownerRef.Kind).To(Equal(mcpv1alpha1.MCPServerKind))
		})

		It("should delete MCPGatewayBinding when spec.gateway is removed", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
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
			mcpServer := &mcpv1alpha1.MCPServer{}
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
			mcpServer := &mcpv1alpha1.MCPServer{}
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

			mcpServer.Spec.Gateway.ClassName = "custom-provider"
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
			mcpServer := &mcpv1alpha1.MCPServer{}
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
			resource.Spec.Gateway = &mcpv1alpha1.GatewaySpec{
				ClassName: ProviderHTTPRoute,
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should return nil when spec.gateway is nil", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			mcpServer.Spec.Gateway = nil

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			status := reconciler.reconcileGatewayCondition(ctx, mcpServer)
			Expect(status).To(BeNil())
		})

		It("should return BindingNotFound when binding doesn't exist", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			status := reconciler.reconcileGatewayCondition(ctx, mcpServer)
			Expect(status).NotTo(BeNil())
			Expect(status.condition.Type).To(Equal(ConditionTypeGatewayRegistered))
			Expect(status.condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(status.condition.Reason).To(Equal(ReasonGatewayBindingNotFound))
		})

		It("should return NotRegistered when binding exists but Registered condition is missing", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
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
			Expect(status.bindingStatus.Provider).To(Equal(ProviderHTTPRoute))
		})

		It("should return Registered when binding has Registered=True", func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
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
				LastTransitionTime: metav1.Now(),
			})
			binding.Status.URL = "https://gateway.example.com/mcp"
			Expect(k8sClient.Status().Update(ctx, binding)).To(Succeed())

			status := reconciler.reconcileGatewayCondition(ctx, mcpServer)
			Expect(status).NotTo(BeNil())
			Expect(status.condition.Type).To(Equal(ConditionTypeGatewayRegistered))
			Expect(status.condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(status.condition.Reason).To(Equal(ReasonGatewayRegistered))
			Expect(status.gatewayAddress).To(Equal("https://gateway.example.com/mcp"))
			Expect(status.bindingStatus).NotTo(BeNil())
			Expect(status.bindingStatus.Name).To(Equal(gatewayBindingName(resourceName)))
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
			resource.Spec.Gateway = &mcpv1alpha1.GatewaySpec{
				ClassName: ProviderHTTPRoute,
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
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

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			readyCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeReady)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(ReasonGatewayNotRegistered))

			gwCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeGatewayRegistered)
			Expect(gwCond).NotTo(BeNil())
			Expect(gwCond.Status).To(Equal(metav1.ConditionFalse))

			Expect(mcpServer.Status.GatewayBinding).NotTo(BeNil())
			Expect(mcpServer.Status.GatewayBinding.Provider).To(Equal(ProviderHTTPRoute))
		})

		It("should reflect gateway URL into MCPServer address when binding is registered", func() {
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
				LastTransitionTime: metav1.Now(),
			})
			binding.Status.URL = "https://gateway.example.com/mcp"
			Expect(k8sClient.Status().Update(ctx, binding)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			Expect(mcpServer.Status.Address).NotTo(BeNil())
			Expect(mcpServer.Status.Address.URL).To(Equal("https://gateway.example.com/mcp"))

			gwCond := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeGatewayRegistered)
			Expect(gwCond).NotTo(BeNil())
			Expect(gwCond.Status).To(Equal(metav1.ConditionTrue))
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
