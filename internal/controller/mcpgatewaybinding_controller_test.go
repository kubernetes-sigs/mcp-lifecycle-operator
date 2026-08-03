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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPGatewayBinding Controller (httproute)", func() {
	ctx := context.Background()

	const (
		mcpServerName = "test-binding-mcp"
		bindingName   = "test-binding"
		configMapName = "test-gw-config"
	)

	newReconciler := func() *MCPGatewayBindingReconciler {
		return &MCPGatewayBindingReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	}

	createMCPServer := func() {
		server := newTestMCPServer(mcpServerName)
		server.Spec.Config.Path = "/mcp"
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
	}

	createConfigMap := func(data map[string]string) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: "default",
			},
			Data: data,
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
	}

	createBinding := func(provider string) {
		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: mcpServerName,
				Provider:     provider,
				ConfigRef:    configMapName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
	}

	AfterEach(func() {
		for _, obj := range []client.Object{
			&gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: "default"}},
			&mcpv1alpha1.MCPGatewayBinding{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: "default"}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "default"}},
			&mcpv1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: mcpServerName, Namespace: "default"}},
		} {
			_ = k8sClient.Delete(ctx, obj)
		}
	})

	It("should create an HTTPRoute from a binding with httproute provider", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      "my-gateway",
			configKeyGatewayNamespace: "gateway-ns",
		})
		createBinding(ProviderHTTPRoute)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())

		Expect(route.Spec.ParentRefs).To(HaveLen(1))
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("my-gateway"))
		Expect(string(*route.Spec.ParentRefs[0].Namespace)).To(Equal("gateway-ns"))

		Expect(route.Spec.Rules).To(HaveLen(1))
		Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(1))
		Expect(string(route.Spec.Rules[0].BackendRefs[0].Name)).To(Equal(mcpServerName))
		Expect(route.Spec.Rules[0].BackendRefs[0].Port).NotTo(BeNil())
		Expect(*route.Spec.Rules[0].BackendRefs[0].Port).To(Equal(gatewayv1.PortNumber(8080)))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))

		ownerRef := metav1.GetControllerOf(route)
		Expect(ownerRef).NotTo(BeNil())
		Expect(ownerRef.Name).To(Equal(bindingName))
		Expect(ownerRef.Kind).To(Equal(mcpv1alpha1.MCPGatewayBindingKind))
	})

	It("should ignore bindings with non-httproute provider", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      "my-gateway",
			configKeyGatewayNamespace: "gateway-ns",
		})
		createBinding("custom-vendor")

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		err = k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)
		Expect(err).To(HaveOccurred())
		Expect(client.IgnoreNotFound(err)).To(Succeed())
	})

	It("should set Registered=False when MCPServer not found", func() {
		createConfigMap(map[string]string{
			configKeyGatewayName:      "my-gateway",
			configKeyGatewayNamespace: "gateway-ns",
		})

		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: "nonexistent",
				Provider:     ProviderHTTPRoute,
				ConfigRef:    configMapName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("nonexistent"))
	})

	It("should set Registered=False when ConfigMap missing", func() {
		createMCPServer()
		createBinding(ProviderHTTPRoute)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configMapName))
	})

	It("should set Registered=False when ConfigMap missing required keys", func() {
		createMCPServer()
		createConfigMap(map[string]string{"some-key": "some-value"})
		createBinding(ProviderHTTPRoute)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyGatewayName))
	})

	It("should set hostname on HTTPRoute when configured in ConfigMap", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      "my-gateway",
			configKeyGatewayNamespace: "gateway-ns",
			configKeyHostname:         "mcp.example.com",
		})
		createBinding(ProviderHTTPRoute)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())
		Expect(route.Spec.Hostnames).To(HaveLen(1))
		Expect(string(route.Spec.Hostnames[0])).To(Equal("mcp.example.com"))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		Expect(binding.Status.URL).To(Equal("http://mcp.example.com/mcp"))
	})

	It("should use default /mcp path when MCPServer path not set", func() {
		server := newTestMCPServer(mcpServerName)
		server.Spec.Config.Path = ""
		Expect(k8sClient.Create(ctx, server)).To(Succeed())

		createConfigMap(map[string]string{
			configKeyGatewayName:      "my-gateway",
			configKeyGatewayNamespace: "gateway-ns",
		})
		createBinding(ProviderHTTPRoute)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())
		Expect(*route.Spec.Rules[0].Matches[0].Path.Value).To(Equal("/mcp"))
	})

	It("should update existing HTTPRoute when ConfigMap changes", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      "my-gateway",
			configKeyGatewayNamespace: "gateway-ns",
		})
		createBinding(ProviderHTTPRoute)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("my-gateway"))

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: "default"}, cm)).To(Succeed())
		cm.Data[configKeyGatewayName] = "updated-gateway"
		Expect(k8sClient.Update(ctx, cm)).To(Succeed())

		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("updated-gateway"))
	})

	It("should set Registered=False when gateway-namespace is empty string", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      "my-gateway",
			configKeyGatewayNamespace: "",
		})
		createBinding(ProviderHTTPRoute)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyGatewayNamespace))
	})

	It("should set Registered=False when configRef is empty", func() {
		createMCPServer()

		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: mcpServerName,
				Provider:     ProviderHTTPRoute,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("configRef is required"))
	})
})
