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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
)

const (
	testNamespace   = "default"
	testGatewayName = "my-gateway"
	testGatewayNS   = "gateway-ns"
)

func newTestMCPServer(name string) *mcpv1beta1.MCPServer {
	return &mcpv1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: mcpv1beta1.MCPServerSpec{
			Source: mcpv1beta1.Source{
				Type: mcpv1beta1.SourceTypeContainerImage,
				ContainerImage: &mcpv1beta1.ContainerImageSource{
					Ref: "docker.io/library/test-image:latest",
				},
			},
			Config: mcpv1beta1.ServerConfig{
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
							ObservedGeneration: route.Generation,
						},
						{
							Type:               string(gatewayv1.RouteConditionResolvedRefs),
							Status:             metav1.ConditionTrue,
							Reason:             "ResolvedRefs",
							LastTransitionTime: metav1.Now(),
							ObservedGeneration: route.Generation,
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
			Client:    k8sClient,
			APIReader: k8sClient,
			Scheme:    k8sClient.Scheme(),
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
			&mcpv1beta1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: mcpServerName, Namespace: testNamespace}},
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

		By("setting PublicAddressPending once the gateway accepts the route but no public hostname is available")
		setHTTPRouteAccepted(ctx, route)

		result, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(mcpcontroller.ReasonPublicAddressPending))
	})

	It("should return early when binding not found", func() {
		r := newReconciler()
		result, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "nonexistent-binding", Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))
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

	It("should set hostname on HTTPRoute from route-hostname and use public-hostname for status URL", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      testGatewayName,
			configKeyGatewayNamespace: testGatewayNS,
			configKeyRouteHostname:    "internal.mcp.local",
			configKeyPublicHostname:   "mcp.example.com",
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
		Expect(string(route.Spec.Hostnames[0])).To(Equal("internal.mcp.local"))

		setHTTPRouteAccepted(ctx, route)

		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		Expect(binding.Status.URL).To(Equal("http://mcp.example.com/mcp"))
	})

	DescribeTable("should reject an invalid route-hostname without creating an HTTPRoute",
		func(badHostname string) {
			createMCPServer()
			createConfigMap(map[string]string{
				configKeyGatewayName:      testGatewayName,
				configKeyGatewayNamespace: testGatewayNS,
				configKeyRouteHostname:    badHostname,
			})
			createBinding(ProviderName)

			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			route := &gatewayv1.HTTPRoute{}
			getErr := k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)
			Expect(apierrors.IsNotFound(getErr)).To(BeTrue(), "no HTTPRoute should be created for an invalid hostname")

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
			registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
			Expect(registered).NotTo(BeNil())
			Expect(registered.Status).To(Equal(metav1.ConditionFalse))
			Expect(registered.Message).To(ContainSubstring(configKeyRouteHostname))
		},
		Entry("with port", "mcp.example.com:8080"),
		Entry("with scheme", "https://mcp.example.com"),
		Entry("with path", "mcp.example.com/mcp"),
		Entry("uppercase", "MCP.example.com"),
		Entry("IP address", "10.0.0.1"),
	)

	It("should set PublicAddressPending when no public-hostname and no Gateway status address", func() {
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
		setHTTPRouteAccepted(ctx, route)

		result, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(mcpcontroller.ReasonPublicAddressPending))
	})

	It("should fall back to route-hostname for status URL when no public-hostname set", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      testGatewayName,
			configKeyGatewayNamespace: testGatewayNS,
			configKeyRouteHostname:    "route.mcp.local",
		})
		createBinding(ProviderName)

		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		setHTTPRouteAccepted(ctx, route)

		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
		Expect(binding.Status.URL).To(Equal("http://route.mcp.local/mcp"))
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

	It("should preserve LastTransitionTime when Registered status unchanged", func() {
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
		originalTransition := registered.LastTransitionTime

		createConfigMap(map[string]string{"some-key": "some-value"})

		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyGatewayName))
		Expect(registered.LastTransitionTime).To(Equal(originalTransition))
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

	It("should not set Registered=True when Accepted but ResolvedRefs missing", func() {
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

		By("setting only Accepted=True without ResolvedRefs")
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

		result, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
	})

	It("should delete stale HTTPRoute when setNotRegistered is called", func() {
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

		By("removing the ConfigMap to trigger setNotRegistered on next reconcile")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: testNamespace}, cm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

		_, err = r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		By("verifying the HTTPRoute was cleaned up")
		err = k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "stale HTTPRoute should be deleted")
	})

	It("should not delete stale HTTPRoute owned by a different controller", func() {
		createMCPServer()
		createBinding(ProviderName)

		foreignRoute := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Name:       "foreign-owner",
						UID:        "foreign-uid",
						Controller: ptr.To(true), //nolint:modernize // new(bool) yields false, not true
					},
				},
			},
			Spec: gatewayv1.HTTPRouteSpec{},
		}
		Expect(k8sClient.Create(ctx, foreignRoute)).To(Succeed())

		By("reconciling without a ConfigMap to trigger setNotRegistered")
		r := newReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())

		By("verifying the foreign-owned HTTPRoute was NOT deleted")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(metav1.GetControllerOf(route).Name).To(Equal("foreign-owner"))
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

		It("should map from PartialObjectMetadata (what the metadata-only watch delivers)", func() {
			createBinding(ProviderName)

			r := newReconciler()
			// WatchesMetadata delivers a *metav1.PartialObjectMetadata, not a full
			// ConfigMap. The handler must map using only name/namespace.
			partial := &metav1.PartialObjectMetadata{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForConfigMap(ctx, partial)
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

		It("should enqueue binding when referenced ConfigMap read fails (avoid dropped reconcile)", func() {
			// The ConfigMap is intentionally not created, so the uncached read
			// returns NotFound. The binding must still be enqueued so the
			// idempotent Reconcile runs rather than the Gateway event being
			// silently dropped for this binding.
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
	})

	Describe("findBindingsForMCPServer", func() {
		It("should return requests for bindings referencing the MCPServer", func() {
			createBinding(ProviderName)

			r := newReconciler()
			server := &mcpv1beta1.MCPServer{
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
			server := &mcpv1beta1.MCPServer{
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
			server := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcpServerName,
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForMCPServer(ctx, server)
			Expect(requests).To(BeEmpty())
		})
	})

	Describe("findBindingsForGateway (empty configRef)", func() {
		It("should skip bindings with empty configRef", func() {
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: testNamespace,
				},
				Spec: mcpv1alpha1.MCPGatewayBindingSpec{
					MCPServerRef: mcpServerName,
					Provider:     ProviderName,
					ConfigRef:    "",
				},
			}
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

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

	Describe("SetupWithManager", func() {
		It("should register the controller when the HTTPRoute CRD is present", func() {
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme:  k8sClient.Scheme(),
				Metrics: metricsserver.Options{BindAddress: "0"},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(Setup(mgr)).To(Succeed())
		})

	})

	It("should set Registered=False when gateway-name present but gateway-namespace key missing", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName: testGatewayName,
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
})
