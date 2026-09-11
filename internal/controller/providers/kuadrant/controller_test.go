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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
	providertesting "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/testing"
)

const testNamespace = "default"

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

func setRegistrationReady(ctx context.Context, name, namespace string) {
	reg := &kuadrantapi.MCPServerRegistration{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, reg)).To(Succeed())
	reg.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			LastTransitionTime: metav1.Now(),
		},
	}
	Expect(k8sClient.Status().Update(ctx, reg)).To(Succeed())
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
						{
							Type:               string(gatewayv1.RouteConditionResolvedRefs),
							Status:             metav1.ConditionTrue,
							Reason:             "ResolvedRefs",
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
				Namespace: testNamespace,
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
			configKeyPrefix:           "myserver_",
		}
	}

	createBinding := func() {
		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
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
			NamespacedName: types.NamespacedName{Name: bindingName, Namespace: testNamespace},
		})
	}

	AfterEach(func() {
		for _, obj := range []client.Object{
			&kuadrantapi.MCPServerRegistration{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: testNamespace}},
			&gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: testNamespace}},
			&mcpv1alpha1.MCPGatewayBinding{ObjectMeta: metav1.ObjectMeta{Name: bindingName, Namespace: testNamespace}},
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: testNamespace}},
			&mcpv1beta1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: mcpServerName, Namespace: testNamespace}},
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
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())

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
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())

		Expect(reg.Spec.TargetRef.Group).To(Equal("gateway.networking.k8s.io"))
		Expect(reg.Spec.TargetRef.Kind).To(Equal("HTTPRoute"))
		Expect(reg.Spec.TargetRef.Name).To(Equal(bindingName))
		Expect(reg.Spec.Path).To(Equal("/mcp"))
		Expect(reg.Spec.State).To(Equal("Enabled"))
		Expect(reg.Spec.Prefix).To(Equal("myserver_"))

		regOwner := metav1.GetControllerOf(reg)
		Expect(regOwner).NotTo(BeNil())
		Expect(regOwner.Name).To(Equal(bindingName))

		By("initially setting Registered=False until gateway accepts the route")
		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("setting Registered=False (RegistrationNotReady) once the route is accepted but registration is not ready")
		setHTTPRouteAccepted(ctx, route)

		result, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRegistrationNotReady))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("setting Registered=True once the MCPServerRegistration is ready")
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
		Expect(binding.Status.URL).To(Equal("http://myserver.mcp.local/mcp"))
	})

	It("should not set Registered=True when MCPServerRegistration is not ready", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		setHTTPRouteAccepted(ctx, route)

		By("setting a NotReady condition on the MCPServerRegistration")
		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		reg.Status.Conditions = []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "NotReady",
				Message:            "no valid mcpgatewayextensions configured",
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(k8sClient.Status().Update(ctx, reg)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRegistrationNotReady))
		Expect(registered.Message).To(Equal("no valid mcpgatewayextensions configured"))
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("transitioning to Registered=True once the MCPServerRegistration becomes ready")
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered = meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
		Expect(binding.Status.URL).To(Equal("http://myserver.mcp.local/mcp"))
	})

	It("should set Registered=False when prefix is missing", func() {
		createMCPServer()
		data := validConfigData()
		delete(data, configKeyPrefix)
		createConfigMap(data)
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyPrefix))
	})

	It("should default sectionName to mcps", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
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
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(string(*route.Spec.ParentRefs[0].SectionName)).To(Equal("custom-listener"))
	})

	It("should use default /mcp path when MCPServer path not set", func() {
		server := newTestMCPServer(mcpServerName)
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Spec.Rules[0].Matches[0].Path.Value).NotTo(BeNil())
		Expect(*route.Spec.Rules[0].Matches[0].Path.Value).To(Equal(mcpcontroller.DefaultMCPPath))

		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		Expect(reg.Spec.Path).To(Equal(mcpcontroller.DefaultMCPPath))
	})

	It("should set Registered=False when gateway-name is empty string", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName:      "",
			configKeyGatewayNamespace: "gateway-ns",
			configKeyHostname:         "mcp.local",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyGatewayName))
	})

	It("should return early when MCPServer not found", func() {
		createConfigMap(validConfigData())

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

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
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
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
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
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
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
				Namespace: testNamespace,
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: mcpServerName,
				Provider:     ProviderName,
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
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
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		Expect(reg.Spec.Prefix).To(Equal("old_"))

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("my-gateway"))

		By("updating ConfigMap")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: testNamespace}, cm)).To(Succeed())
		cm.Data[configKeyGatewayName] = "updated-gateway"
		cm.Data[configKeyPrefix] = "new_"
		Expect(k8sClient.Update(ctx, cm)).To(Succeed())

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("updated-gateway"))

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		Expect(reg.Spec.Prefix).To(Equal("new_"))
	})

	It("should ignore HTTPRoute status from a different parent gateway", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())

		By("setting accepted status from a different gateway")
		differentGW := gatewayv1.Namespace("other-ns")
		route.Status = gatewayv1.HTTPRouteStatus{
			RouteStatus: gatewayv1.RouteStatus{
				Parents: []gatewayv1.RouteParentStatus{
					{
						ParentRef: gatewayv1.ParentReference{
							Name:      "other-gateway",
							Namespace: &differentGW,
						},
						ControllerName: "gateway.example.com/controller",
						Conditions: []metav1.Condition{
							{
								Type:               string(gatewayv1.RouteConditionAccepted),
								Status:             metav1.ConditionTrue,
								Reason:             "Accepted",
								LastTransitionTime: metav1.Now(),
							},
							{
								Type:               string(gatewayv1.RouteConditionResolvedRefs),
								Status:             metav1.ConditionTrue,
								Reason:             "ResolvedRefs",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
	})

	It("should ignore stale HTTPRoute conditions from a previous generation", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		oldGeneration := route.Generation

		By("updating the route spec to bump its generation")
		newPath := "/mcp/v2"
		route.Spec.Rules[0].Matches[0].Path.Value = &newPath
		Expect(k8sClient.Update(ctx, route)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(route.Generation).To(BeNumerically(">", oldGeneration))

		By("setting accepted conditions with the old generation")
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
								ObservedGeneration: oldGeneration,
							},
							{
								Type:               string(gatewayv1.RouteConditionResolvedRefs),
								Status:             metav1.ConditionTrue,
								Reason:             "ResolvedRefs",
								LastTransitionTime: metav1.Now(),
								ObservedGeneration: oldGeneration,
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
	})

	Describe("findBindingsForConfigMap", func() {
		It("should return requests for bindings referencing the ConfigMap", func() {
			createBinding()

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
			createBinding()

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

		It("should not return bindings with non-kuadrant provider", func() {
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: testNamespace,
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
					Namespace: testNamespace,
				},
			}
			requests := r.findBindingsForConfigMap(ctx, cm)
			Expect(requests).To(BeEmpty())
		})
	})

	It("should return early when binding is not found", func() {
		r := newReconciler()
		result, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))
	})

	It("should set Registered=False when ConfigMap does not exist", func() {
		createMCPServer()
		binding := &mcpv1alpha1.MCPGatewayBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bindingName,
				Namespace: testNamespace,
			},
			Spec: mcpv1alpha1.MCPGatewayBindingSpec{
				MCPServerRef: mcpServerName,
				Provider:     ProviderName,
				ConfigRef:    "missing-configmap",
			},
		}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("not found"))
	})

	It("should set Registered=False when gateway-namespace is missing", func() {
		createMCPServer()
		createConfigMap(map[string]string{
			configKeyGatewayName: "my-gateway",
		})
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring(configKeyGatewayNamespace))
	})

	It("should not accept route when gateway name matches but namespace differs", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())

		By("setting accepted status with matching name but different namespace")
		wrongNS := gatewayv1.Namespace("wrong-namespace")
		route.Status = gatewayv1.HTTPRouteStatus{
			RouteStatus: gatewayv1.RouteStatus{
				Parents: []gatewayv1.RouteParentStatus{
					{
						ParentRef: gatewayv1.ParentReference{
							Name:      gatewayv1.ObjectName("my-gateway"),
							Namespace: &wrongNS,
						},
						ControllerName: "gateway.example.com/controller",
						Conditions: []metav1.Condition{
							{
								Type:               string(gatewayv1.RouteConditionAccepted),
								Status:             metav1.ConditionTrue,
								Reason:             "Accepted",
								LastTransitionTime: metav1.Now(),
							},
							{
								Type:               string(gatewayv1.RouteConditionResolvedRefs),
								Status:             metav1.ConditionTrue,
								Reason:             "ResolvedRefs",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
	})

	It("should skip status update when nothing changed", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		result, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		result2, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())
		Expect(result2.RequeueAfter).To(BeNumerically(">", 0))

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Reason).To(Equal(reasonRouteNotAccepted))
	})

	It("should skip status update when reconciled twice after accepted", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		setHTTPRouteAccepted(ctx, route)
		setRegistrationReady(ctx, bindingName, testNamespace)

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionTrue))
	})

	It("should delete stale resources when config becomes invalid", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		By("first reconcile creates HTTPRoute and MCPServerRegistration")
		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())

		By("deleting the ConfigMap to invalidate the config")
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: testNamespace}, cm)).To(Succeed())
		Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

		By("second reconcile should delete stale resources")
		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg))).To(BeTrue())

		binding := &mcpv1alpha1.MCPGatewayBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, binding)).To(Succeed())
		registered := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
		Expect(registered).NotTo(BeNil())
		Expect(registered.Status).To(Equal(metav1.ConditionFalse))
		Expect(registered.Message).To(ContainSubstring("not found"))
	})

	It("should restore ownerReference when stripped but spec unchanged", func() {
		createMCPServer()
		createConfigMap(validConfigData())
		createBinding()

		_, err := doReconcile()
		Expect(err).NotTo(HaveOccurred())

		By("stripping owner reference from HTTPRoute")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		route.OwnerReferences = nil
		Expect(k8sClient.Update(ctx, route)).To(Succeed())

		By("stripping owner reference from MCPServerRegistration")
		reg := &kuadrantapi.MCPServerRegistration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		reg.OwnerReferences = nil
		Expect(k8sClient.Update(ctx, reg)).To(Succeed())

		By("reconciling should restore owner references")
		_, err = doReconcile()
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, route)).To(Succeed())
		Expect(metav1.GetControllerOf(route)).NotTo(BeNil())
		Expect(metav1.GetControllerOf(route).Name).To(Equal(bindingName))

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: testNamespace}, reg)).To(Succeed())
		Expect(metav1.GetControllerOf(reg)).NotTo(BeNil())
		Expect(metav1.GetControllerOf(reg).Name).To(Equal(bindingName))
	})

	Describe("findBindingsForGateway", func() {
		It("should return requests for bindings whose ConfigMap references the gateway", func() {
			createConfigMap(validConfigData())
			createBinding()

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(bindingName))
		})

		It("should not return requests for unrelated gateways", func() {
			createConfigMap(validConfigData())
			createBinding()

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})

		It("should not return requests when ConfigMap is missing", func() {
			createBinding()

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})

		It("should skip bindings with empty configRef", func() {
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
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})

		It("should skip bindings with non-kuadrant provider", func() {
			createConfigMap(validConfigData())
			binding := &mcpv1alpha1.MCPGatewayBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bindingName,
					Namespace: testNamespace,
				},
				Spec: mcpv1alpha1.MCPGatewayBindingSpec{
					MCPServerRef: mcpServerName,
					Provider:     "httproute",
					ConfigRef:    configMapName,
				},
			}
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			r := newReconciler()
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-gateway",
					Namespace: "gateway-ns",
				},
			}
			requests := r.findBindingsForGateway(ctx, gw)
			Expect(requests).To(BeEmpty())
		})
	})

	Describe("SetupWithManager", func() {
		It("should register the controller without error", func() {
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: k8sClient.Scheme(),
			})
			Expect(err).NotTo(HaveOccurred())

			r := &Reconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			Expect(r.SetupWithManager(mgr)).To(Succeed())
		})

		It("should skip when HTTPRoute CRD is not found", func() {
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: k8sClient.Scheme(),
			})
			Expect(err).NotTo(HaveOccurred())

			wrappedMgr := &providertesting.CRDMissingManager{
				Manager: mgr,
				MissingGVKs: map[schema.GroupVersionKind]bool{
					{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}: true,
				},
			}

			r := &Reconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			Expect(r.SetupWithManager(wrappedMgr)).To(Succeed())
		})

		It("should skip when MCPServerRegistration CRD is not found", func() {
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: k8sClient.Scheme(),
			})
			Expect(err).NotTo(HaveOccurred())

			wrappedMgr := &providertesting.CRDMissingManager{
				Manager: mgr,
				MissingGVKs: map[schema.GroupVersionKind]bool{
					{Group: "mcp.kuadrant.io", Version: "v1alpha1", Kind: "MCPServerRegistration"}: true,
				},
			}

			r := &Reconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			Expect(r.SetupWithManager(wrappedMgr)).To(Succeed())
		})

		It("should return error when HTTPRoute CRD check fails with non-NoMatch error", func() {
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: k8sClient.Scheme(),
			})
			Expect(err).NotTo(HaveOccurred())

			wrappedMgr := &providertesting.CRDMissingManager{
				Manager: mgr,
				ErrorGVKs: map[schema.GroupVersionKind]error{
					{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}: fmt.Errorf("connection refused"),
				},
			}

			r := &Reconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			err = r.SetupWithManager(wrappedMgr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("checking for HTTPRoute CRD"))
		})

		It("should return error when MCPServerRegistration CRD check fails with non-NoMatch error", func() {
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: k8sClient.Scheme(),
			})
			Expect(err).NotTo(HaveOccurred())

			wrappedMgr := &providertesting.CRDMissingManager{
				Manager: mgr,
				ErrorGVKs: map[schema.GroupVersionKind]error{
					{Group: "mcp.kuadrant.io", Version: "v1alpha1", Kind: "MCPServerRegistration"}: fmt.Errorf("connection refused"),
				},
			}

			r := &Reconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}
			err = r.SetupWithManager(wrappedMgr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("checking for MCPServerRegistration CRD"))
		})
	})

	Describe("findBindingsForMCPServer", func() {
		It("should return requests for bindings referencing the MCPServer", func() {
			createBinding()

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
			createBinding()

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
	})
})
