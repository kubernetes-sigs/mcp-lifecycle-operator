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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Controller - HTTPRoute", func() {
	const resourceName = "test-httproute"

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

		route := &gatewayv1.HTTPRoute{}
		err = k8sClient.Get(ctx, typeNamespacedName, route)
		if err == nil {
			Expect(k8sClient.Delete(ctx, route)).To(Succeed())
		}
	})

	reconcileMCPServer := func() {
		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
	}

	It("should not create HTTPRoute when spec.gateway is nil", func() {
		resource := newTestMCPServer(resourceName)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		err := k8sClient.Get(ctx, typeNamespacedName, route)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not found"))
	})

	It("should create HTTPRoute when spec.gateway is set", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.Name).To(Equal(resourceName))
	})

	It("should set correct parentRef on HTTPRoute", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name:      "my-gateway",
				Namespace: "gateway-ns",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.Spec.ParentRefs).To(HaveLen(1))
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("my-gateway"))
		Expect(route.Spec.ParentRefs[0].Namespace).NotTo(BeNil())
		Expect(string(*route.Spec.ParentRefs[0].Namespace)).To(Equal("gateway-ns"))
	})

	It("should set parentRef namespace to nil when not specified", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.Spec.ParentRefs[0].Namespace).To(BeNil())
	})

	It("should set backendRef to the MCPServer's Service name and port", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Config.Port = 9090
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.Spec.Rules).To(HaveLen(1))
		Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(1))
		Expect(string(route.Spec.Rules[0].BackendRefs[0].Name)).To(Equal(resourceName))
		Expect(route.Spec.Rules[0].BackendRefs[0].Port).NotTo(BeNil())
		Expect(*route.Spec.Rules[0].BackendRefs[0].Port).To(Equal(gatewayv1.PortNumber(9090)))
	})

	It("should use default /mcp path when config.path is empty", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.Spec.Rules[0].Matches).To(HaveLen(1))
		Expect(route.Spec.Rules[0].Matches[0].Path).NotTo(BeNil())
		Expect(*route.Spec.Rules[0].Matches[0].Path.Value).To(Equal("/mcp"))
	})

	It("should use custom path from config.path", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Config.Path = "/api/v1/mcp"
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(*route.Spec.Rules[0].Matches[0].Path.Value).To(Equal("/api/v1/mcp"))
	})

	It("should use PathPrefix match type", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(*route.Spec.Rules[0].Matches[0].Path.Type).To(Equal(gatewayv1.PathMatchPathPrefix))
	})

	It("should set owner reference on HTTPRoute", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())

		ownerRef := metav1.GetControllerOf(route)
		Expect(ownerRef).NotTo(BeNil())
		Expect(ownerRef.Name).To(Equal(resourceName))
		Expect(ownerRef.UID).To(Equal(mcpServer.UID))
	})

	It("should update HTTPRoute when parentRef changes", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "gateway-v1",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("gateway-v1"))

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Gateway.ParentRef.Name = "gateway-v2"
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		reconcileMCPServer()

		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("gateway-v2"))
	})

	It("should delete HTTPRoute when spec.gateway is removed", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Gateway = nil
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		reconcileMCPServer()

		err := k8sClient.Get(ctx, typeNamespacedName, route)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not found"))
	})

	It("should set hostnames when spec.gateway.hostname is set", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
			Hostname: "kubernetes-mcp.mcp.local",
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.Spec.Hostnames).To(HaveLen(1))
		Expect(string(route.Spec.Hostnames[0])).To(Equal("kubernetes-mcp.mcp.local"))
	})

	It("should not set hostnames when spec.gateway.hostname is empty", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.Spec.Hostnames).To(BeEmpty())
	})

	It("should set managed workload labels on HTTPRoute", func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.Labels).To(HaveKeyWithValue("app", "mcp-server"))
		Expect(route.Labels).To(HaveKeyWithValue("mcp-server", resourceName))
	})

	It("should reject updating HTTPRoute when owned by another controller", func() {
		By("Pre-creating an HTTPRoute owned by a different controller")
		foreignRoute := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "SomeOtherController",
						Name:       "foreign-owner",
						UID:        "foreign-controller-uid",
						Controller: new(true),
					},
				},
			},
			Spec: gatewayv1.HTTPRouteSpec{},
		}
		Expect(k8sClient.Create(ctx, foreignRoute)).To(Succeed())

		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is owned by"))

		By("Verifying the original foreign owner reference is still present")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.OwnerReferences).To(HaveLen(1))
		Expect(route.OwnerReferences[0].Name).To(Equal("foreign-owner"))
	})

	It("should not delete foreign-owned HTTPRoute when gateway is removed", func() {
		By("Pre-creating an HTTPRoute owned by a different controller")
		foreignRoute := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "SomeOtherController",
						Name:       "foreign-owner",
						UID:        "foreign-controller-uid",
						Controller: new(true),
					},
				},
			},
			Spec: gatewayv1.HTTPRouteSpec{},
		}
		Expect(k8sClient.Create(ctx, foreignRoute)).To(Succeed())

		resource := newTestMCPServer(resourceName)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconcileMCPServer()

		By("Verifying the foreign HTTPRoute was NOT deleted")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, route)).To(Succeed())
		Expect(route.OwnerReferences[0].Name).To(Equal("foreign-owner"))
	})

	It("should reject updating HTTPRoute with no controller owner", func() {
		By("Pre-creating an HTTPRoute with no owner")
		unownedRoute := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: gatewayv1.HTTPRouteSpec{},
		}
		Expect(k8sClient.Create(ctx, unownedRoute)).To(Succeed())

		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("has no controller owner"))
	})
})

var _ = Describe("MCPServer Controller - HTTPRoute Event Emission", func() {
	const resourceName = "test-httproute-events"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Gateway = &mcpv1alpha1.GatewayConfig{
			ParentRef: mcpv1alpha1.GatewayParentRef{
				Name: "my-gateway",
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	})

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		}
	})

	It("should emit a Warning HTTPRouteReconcileFailed event only when error message changes", func() {
		failMsg := "simulated httproute creation failure"
		reconciler, fr := newReconcilerForTestWithFakeEvents(k8sClient, k8sClient.Scheme())

		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*gatewayv1.HTTPRoute); ok {
					return fmt.Errorf("%s", failMsg)
				}
				return c.Create(ctx, obj, opts...)
			},
		})
		reconciler.Client = interceptedClient

		By("First HTTPRoute reconcile failure - Warning event emitted once")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())

		var routeFailedEvent string
		Eventually(func(g Gomega) {
			for _, ev := range drainEvents(fr.Events) {
				if strings.Contains(ev, corev1.EventTypeWarning) && strings.Contains(ev, ReasonGatewayRouteUnavailable) {
					routeFailedEvent = ev
					break
				}
			}
			g.Expect(routeFailedEvent).NotTo(BeEmpty())
			g.Expect(routeFailedEvent).To(ContainSubstring(resourceName))
			g.Expect(routeFailedEvent).To(ContainSubstring("Failed to reconcile HTTPRoute"))
			g.Expect(routeFailedEvent).To(ContainSubstring(failMsg))
		}).Should(Succeed())

		By("Second reconcile with same error - no duplicate event")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())
		Consistently(fr.Events, 300*time.Millisecond, 20*time.Millisecond).ShouldNot(Receive())

		By("Change error message - second Warning event emitted")
		failMsg = "simulated httproute ownership failure"
		interceptedClient = interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*gatewayv1.HTTPRoute); ok {
					return fmt.Errorf("%s", failMsg)
				}
				return c.Create(ctx, obj, opts...)
			},
		})
		reconciler.Client = interceptedClient

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())

		var secondRouteFailedEvent string
		Eventually(func(g Gomega) {
			for _, ev := range drainEvents(fr.Events) {
				if strings.Contains(ev, corev1.EventTypeWarning) && strings.Contains(ev, ReasonGatewayRouteUnavailable) {
					secondRouteFailedEvent = ev
					break
				}
			}
			g.Expect(secondRouteFailedEvent).NotTo(BeEmpty())
			g.Expect(secondRouteFailedEvent).To(ContainSubstring(resourceName))
			g.Expect(secondRouteFailedEvent).To(ContainSubstring(failMsg))
			g.Expect(secondRouteFailedEvent).NotTo(Equal(routeFailedEvent))
		}).Should(Succeed())
	})
})
