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

package kuadrant

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
	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
)

func newTestMCPServer(name string) *mcpv1alpha1.MCPServer {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Source: mcpv1alpha1.Source{
				Type: mcpv1alpha1.SourceTypeContainerImage,
				ContainerImage: &mcpv1alpha1.ContainerImageSource{
					Ref: "docker.io/library/test-image:latest",
				},
			},
			Config: mcpv1alpha1.ServerConfig{
				Port: 8080,
			},
		},
	}
}

func setHTTPRouteAccepted(ctx context.Context, route *gatewayv1.HTTPRoute) {
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(route), route)).To(Succeed())
	route.Status = gatewayv1.HTTPRouteStatus{
		RouteStatus: gatewayv1.RouteStatus{
			Parents: []gatewayv1.RouteParentStatus{
				{
					ParentRef:      route.Spec.ParentRefs[0],
					ControllerName: "gateway.example.com/controller",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             "Accepted",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
		},
	}
	Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())
}

var _ = Describe("Kuadrant Provider Controller", func() {
	ctx := context.Background()

	const (
		mcpServerName = "test-kuadrant-mcp"
		bindingName   = "test-kuadrant-binding"
		configMapName = "test-kuadrant-config"
	)

	newReconciler := func() *Reconciler {
		return &Reconciler{
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

	validConfigData := func() map[string]string {
		return map[string]string{
			configKeyGatewayName:      "my-gateway",
			configKeyGatewayNamespace: "gateway-ns",
			configKeyHostname:         "myserver.mcp.local",
		}
	}

	createBinding := func() {
		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: mcpServerName,
				Provider:     ProviderName,
				ConfigRef:    configMapName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
	}

	doReconcile := func() (reconcile.Result, error) {
		r := newReconciler()
		return r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: "default"},
		})
	}

	AfterEach(func() {
		for _, obj := range []client.Object{
			&kuadrantapi.MCPServerRegistration{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: "default"}},
			&gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: "default"}},
			&mcpv1alpha1.MCPGatewayBinding{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: "default"}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "default"}},
			&mcpv1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: mcpServerName, Namespace: "default"}},
		} {
			_ = k8sClient.Delete(ctx, obj)
		}
	})

	It("should create HTTPRoute and MCPServerRegistration from valid binding", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		By("verifying HTTPRoute was created with sectionName")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())

		Expect(route.Spec.ParentRefs).To(HaveLen(1))
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("my-gateway"))
		Expect(string(*route.Spec.ParentRefs[0].Namespace)).To(Equal("gateway-ns"))
		Expect(route.Spec.ParentRefs[0].SectionName).NotTo(BeNil())
		Expect(string(*route.Spec.ParentRefs[0].SectionName)).To(Equal(defaultSectionName))

		Expect(route.Spec.Hostnames).To(HaveLen(1))
		Expect(string(route.Spec.Hostnames[0])).To(Equal("myserver.mcp.local"))

		Expect(route.Spec.Rules).To(HaveLen(1))
		Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(1))
		Expect(string(route.Spec.Rules[0].BackendRefs[0].Name)).To(Equal(mcpServerName))
		Expect(*route.Spec.Rules[0].BackendRefs[0].Port).To(Equal(gatewayv1.PortNumber(8080)))

		ownerRef := metav1.GetControllerOf(route)
		Expect(ownerRef).NotTo(BeNil())
		Expect(ownerRef.Name).To(Equal(bindingName))

		By("verifying MCPServerRegistration was created")
		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, reg)).To(Succeed())

		Expect(reg.Spec.TargetRef.Group).To(Equal("gateway.networking.k8s.io"))
		Expect(reg.Spec.TargetRef.Kind).To(Equal("HTTPRoute"))
		Expect(reg.Spec.TargetRef.Name).To(Equal(bindingName))
		Expect(reg.Spec.Path).To(Equal("/mcp"))
		Expect(reg.Spec.State).To(Equal("Enabled"))
		Expect(reg.Spec.Prefix).To(BeEmpty())

		regOwner := metav1.GetControllerOf(reg)
		Expect(regOwner).NotTo(BeNil())
		Expect(regOwner.Name).To(Equal(bindingName))

		By("initially setting Registered=False until gateway accepts the route")
		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("setting Registered=True once the gateway accepts the route")
		setHTTPRouteAccepted(ctx, route)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
		Expect(binding.Status.URL).To(Equal("http://myserver.mcp.local/mcp"))
	})

	It("should set optional prefix on MCPServerRegistration when present", func() {
		createMCPServer()
		data := validConfigData()
		data[configKeyPrefix] = "myserver_"
		createConfigMap(data)
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, reg)).To(Succeed())
		Expect(reg.Spec.Prefix).To(Equal("myserver_"))
	})

	It("should default sectionName to mcps", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())
		Expect(route.Spec.ParentRefs[0].SectionName).NotTo(BeNil())
		Expect(string(*route.Spec.ParentRefs[0].SectionName)).To(Equal("mcps"))
	})

	It("should use custom sectionName when provided", func() {
		createMCPServer()
		data := validConfigData()
		data[configKeySectionName] = "custom-listener"
		createConfigMap(data)
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())
		Expect(string(*route.Spec.ParentRefs[0].SectionName)).To(Equal("custom-listener"))
	})

	It("should return early when MCPServer not found", func() {
		createConfigMap(validConfigData())

		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: "default",
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: "nonexistent",
				Provider:     ProviderName,
				ConfigRef:    configMapName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).To(BeNil())
	})

	It("should set Registered=False when ConfigMap missing required keys", func() {
		createMCPServer()
		createConfigMap(map[string]string{"some-key": "some-value"})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyGatewayName))
	})

	It("should set Registered=False when hostname is missing", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      "my-gateway",
			configKeyGatewayNamespace: "gateway-ns",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyHostname))
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
				Provider:     ProviderName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("configRef is required"))
	})

	It("should update HTTPRoute and MCPServerRegistration when ConfigMap changes", func() {
		createMCPServer()
		data := validConfigData()
		data[configKeyPrefix] = "old_"
		createConfigMap(data)
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, reg)).To(Succeed())
		Expect(reg.Spec.Prefix).To(Equal("old_"))

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("my-gateway"))

		By("updating ConfigMap")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: "default"}, cm)).To(Succeed())
		cm.Data[configKeyGatewayName] = "updated-gateway"
		cm.Data[configKeyPrefix] = "new_"
		Expect(k8sClient.Update(ctx, cm)).To(Succeed())

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("updated-gateway"))

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: "default"}, reg)).To(Succeed())
		Expect(reg.Spec.Prefix).To(Equal("new_"))
	})

	Describe("findBindingsForConfigMap", func() {
		It("should return requests for bindings referencing the ConfigMap", func() {
			createBinding()

			r := newReconciler()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: "default",
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not return bindings for unrelated ConfigMaps", func() {
			createBinding()

			r := newReconciler()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-cm",
					Namespace: "default",
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(BeEmpty())
		})

		It("should not return bindings with non-kuadrant provider", func() {
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPGatewayBindingSpec{
					MCPServerRef: mcpServerName,
					Provider:     "httproute",
					ConfigRef:    configMapName,
				},
			}
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			r := newReconciler()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: "default",
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(BeEmpty())
		})
	})

	Describe("findBindingsForMCPServer", func() {
		It("should return requests for bindings referencing the MCPServer", func() {
			createBinding()

			r := newReconciler()
			server := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcpServerName,
					Namespace: "default",
				},
			}
			requests := r.findBindingsForMCPServer(ctx, server)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not return bindings for unrelated MCPServers", func() {
			createBinding()

			r := newReconciler()
			server := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-server",
					Namespace: "default",
				},
			}
			requests := r.findBindingsForMCPServer(ctx, server)
			Expect(requests).To(BeEmpty())
		})
	})
})
