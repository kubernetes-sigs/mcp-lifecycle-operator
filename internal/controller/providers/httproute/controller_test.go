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

package httproute

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
)

const (
	testNamespace   = "default"
	testGatewayName = "my-gateway"
	testGatewayNS   = "gateway-ns"
)

func newTestMCPServer(name string) *mcpv1alpha1.MCPServer {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
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

var _ = Describe("HTTPRoute Provider Controller", func() {
	ctx := context.Background()

	const (
		mcpServerName = "test-binding-mcp"
		bindingName   = "test-binding"
		configMapName = "test-gw-config"
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
				Namespace: testNamespace,
			},
			Data: data,
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
	}

	createBinding := func(provider string) {
		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
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
			&gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: testNamespace}},
			&mcpv1alpha1.MCPGatewayBinding{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: testNamespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: testNamespace}},
			&mcpv1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: mcpServerName, Namespace: testNamespace}},
		} {
			_ = k8sClient.Delete(ctx, obj)
		}
	})

	It("should create an HTTPRoute from a binding with httproute provider", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      testGatewayName,
			configKeyGatewayNamespace: testGatewayNS,
		})
		createBinding(ProviderName)

		r := newReconciler()
		result, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())

		Expect(route.Spec.ParentRefs).To(HaveLen(1))
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal(testGatewayName))
		Expect(string(*route.Spec.ParentRefs[0].Namespace)).To(Equal(testGatewayNS))

		Expect(route.Spec.Rules).To(HaveLen(1))
		Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(1))
		Expect(string(route.Spec.Rules[0].BackendRefs[0].Name)).To(Equal(mcpServerName))
		Expect(route.Spec.Rules[0].BackendRefs[0].Port).NotTo(BeNil())
		Expect(*route.Spec.Rules[0].BackendRefs[0].Port).To(Equal(gatewayv1.PortNumber(8080)))

		ownerRef := metav1.GetControllerOf(route)
		Expect(ownerRef).NotTo(BeNil())
		Expect(ownerRef.Name).To(Equal(bindingName))
		Expect(ownerRef.Kind).To(Equal(mcpv1alpha1.MCPGatewayBindingKind))

		By("initially setting Registered=False until gateway accepts the route")
		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("setting Registered=True once the gateway accepts the route")
		setHTTPRouteAccepted(ctx, route)

		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
	})

	It("should return early when MCPServer not found", func() {
		createConfigMap(map[string]string{
			configKeyGatewayName:      testGatewayName,
			configKeyGatewayNamespace: testGatewayNS,
		})

		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: "nonexistent",
				Provider:     ProviderName,
				ConfigRef:    configMapName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		r := newReconciler()
		result, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).To(BeNil())
	})

	It("should set Registered=False when ConfigMap missing", func() {
		createMCPServer()
		createBinding(ProviderName)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configMapName))
	})

	It("should set Registered=False when ConfigMap missing required keys", func() {
		createMCPServer()
		createConfigMap(map[string]string{"some-key": "some-value"})
		createBinding(ProviderName)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyGatewayName))
	})

	It("should set hostname on HTTPRoute when configured in ConfigMap", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      testGatewayName,
			configKeyGatewayNamespace: testGatewayNS,
			configKeyHostname:         "mcp.example.com",
		})
		createBinding(ProviderName)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Spec.Hostnames).To(HaveLen(1))
		Expect(string(route.Spec.Hostnames[0])).To(Equal("mcp.example.com"))

		setHTTPRouteAccepted(ctx, route)

		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		Expect(binding.Status.URL).To(Equal("http://mcp.example.com/mcp"))
	})

	It("should use default /mcp path when MCPServer path not set", func() {
		server := newTestMCPServer(mcpServerName)
		server.Spec.Config.Path = ""
		Expect(k8sClient.Create(ctx, server)).To(Succeed())

		createConfigMap(map[string]string{
			configKeyGatewayName:      testGatewayName,
			configKeyGatewayNamespace: testGatewayNS,
		})
		createBinding(ProviderName)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(*route.Spec.Rules[0].Matches[0].Path.Value).To(Equal("/mcp"))
	})

	It("should update existing HTTPRoute when ConfigMap changes", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      testGatewayName,
			configKeyGatewayNamespace: testGatewayNS,
		})
		createBinding(ProviderName)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal(testGatewayName))

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: testNamespace}, cm)).To(Succeed())
		cm.Data[configKeyGatewayName] = "updated-gateway"
		Expect(k8sClient.Update(ctx, cm)).To(Succeed())

		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("updated-gateway"))
	})

	It("should restore ownerReference when stripped but spec unchanged", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      testGatewayName,
			configKeyGatewayNamespace: testGatewayNS,
		})
		createBinding(ProviderName)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(metav1.GetControllerOf(route)).NotTo(BeNil())

		route.OwnerReferences = nil
		Expect(k8sClient.Update(ctx, route)).To(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(metav1.GetControllerOf(route)).To(BeNil())

		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		ownerRef := metav1.GetControllerOf(route)
		Expect(ownerRef).NotTo(BeNil())
		Expect(ownerRef.Name).To(Equal(bindingName))
		Expect(ownerRef.Kind).To(Equal(mcpv1alpha1.MCPGatewayBindingKind))
	})

	It("should set Registered=False when gateway-namespace is empty string", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      testGatewayName,
			configKeyGatewayNamespace: "",
		})
		createBinding(ProviderName)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyGatewayNamespace))
	})

	It("should set Registered=False when configRef is empty", func() {
		createMCPServer()

		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: mcpServerName,
				Provider:     ProviderName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("configRef is required"))
	})

	Describe("findBindingsForConfigMap", func() {
		It("should return requests for bindings referencing the ConfigMap", func() {
			createBinding(ProviderName)

			r := newReconciler()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not return bindings for unrelated ConfigMaps", func() {
			createBinding(ProviderName)

			r := newReconciler()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-cm",
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(BeEmpty())
		})

		It("should not return bindings with non-httproute provider", func() {
			createBinding("custom-vendor")

			r := newReconciler()
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(BeEmpty())
		})
	})

	Describe("findBindingsForGateway", func() {
		It("should return requests for bindings whose ConfigMap references the Gateway", func() {
			createConfigMap(map[string]string{
				configKeyGatewayName:      testGatewayName,
				configKeyGatewayNamespace: testGatewayNS,
			})
			createBinding(ProviderName)

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testGatewayName,
					Namespace: testGatewayNS,
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not return bindings for a different Gateway name", func() {
			createConfigMap(map[string]string{
				configKeyGatewayName:      testGatewayName,
				configKeyGatewayNamespace: testGatewayNS,
			})
			createBinding(ProviderName)

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-gateway",
					Namespace: testGatewayNS,
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})

		It("should not return bindings for a different Gateway namespace", func() {
			createConfigMap(map[string]string{
				configKeyGatewayName:      testGatewayName,
				configKeyGatewayNamespace: testGatewayNS,
			})
			createBinding(ProviderName)

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testGatewayName,
					Namespace: "other-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})

		It("should not return bindings with non-httproute provider", func() {
			createConfigMap(map[string]string{
				configKeyGatewayName:      testGatewayName,
				configKeyGatewayNamespace: testGatewayNS,
			})
			createBinding("custom-vendor")

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testGatewayName,
					Namespace: testGatewayNS,
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})
	})

	Describe("findBindingsForMCPServer", func() {
		It("should return requests for bindings referencing the MCPServer", func() {
			createBinding(ProviderName)

			r := newReconciler()
			server := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcpServerName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForMCPServer(ctx, server)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not return bindings for unrelated MCPServers", func() {
			createBinding(ProviderName)

			r := newReconciler()
			server := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-server",
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForMCPServer(ctx, server)
			Expect(requests).To(BeEmpty())
		})

		It("should not return bindings with non-httproute provider", func() {
			createBinding("custom-vendor")

			r := newReconciler()
			server := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcpServerName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForMCPServer(ctx, server)
			Expect(requests).To(BeEmpty())
		})
	})
})
