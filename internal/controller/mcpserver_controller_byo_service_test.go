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
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

var _ = Describe("MCPServer Controller - BYO Service", func() {
	ctx := context.Background()

	Context("When reconciling with BYO Service serviceRef", func() {
		const resourceName = "test-byo-service"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "byo-svc-deploy",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "byo-svc"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "byo-svc"}},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "server",
								Image: "my-mcp:latest",
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, dep)).To(Succeed())
			dep.Status.ReadyReplicas = 1
			dep.Status.Replicas = 1
			dep.Status.AvailableReplicas = 1
			dep.Status.ObservedGeneration = dep.Generation
			dep.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}}
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-mcp-svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 9090, Protocol: corev1.ProtocolTCP},
					},
					Selector: map[string]string{"app": "my-mcp"},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "byo-svc-deploy",
				Kind: mcpv1beta1.WorkloadKindDeployment,
			}, &mcpv1beta1.ServiceReference{
				Name: "my-mcp-svc",
			})
			server.Spec.Config.Port = 0
			server.Spec.Config.Path = DefaultMCPPath
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-svc", Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "byo-svc-deploy", Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
		})

		It("should not create an operator-managed Service and should use serviceRef in MCP URL", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			operatorSvc := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, operatorSvc)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "no operator-managed Service should be created for BYO service")

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			Expect(mcpServer.Status.ServiceName).To(Equal("my-mcp-svc"))
		})
	})

	Context("When BYO Service has port named mcp", func() {
		const resourceName = "test-byo-service-mcp-port"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "byo-svc-mcp-deploy",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "byo-svc-mcp"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "byo-svc-mcp"}},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "server",
								Image: "my-mcp:latest",
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, dep)).To(Succeed())
			dep.Status.ReadyReplicas = 1
			dep.Status.Replicas = 1
			dep.Status.AvailableReplicas = 1
			dep.Status.ObservedGeneration = dep.Generation
			dep.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}}
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-mcp-svc-multi",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "metrics", Port: 9090, Protocol: corev1.ProtocolTCP},
						{Name: "mcp", Port: 3000, Protocol: corev1.ProtocolTCP},
						{Name: "admin", Port: 8081, Protocol: corev1.ProtocolTCP},
					},
					Selector: map[string]string{"app": "my-mcp"},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "byo-svc-mcp-deploy",
				Kind: mcpv1beta1.WorkloadKindDeployment,
			}, &mcpv1beta1.ServiceReference{
				Name: "my-mcp-svc-multi",
			})
			server.Spec.Config.Port = 0
			server.Spec.Config.Path = DefaultMCPPath
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-svc-multi", Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "byo-svc-mcp-deploy", Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
		})

		It("should prefer the port named mcp over other ports", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			Expect(mcpServer.Status.ServiceName).To(Equal("my-mcp-svc-multi"))
			Expect(mcpServer.Status.Address).NotTo(BeNil())
			Expect(mcpServer.Status.Address.URL).To(ContainSubstring(":3000/"),
				"should resolve to the mcp-named port, not metrics (9090) or admin (8081)")
		})
	})

	Context("When BYO Service reference does not exist", func() {
		const resourceName = "test-byo-missing-service"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			server := newTestBYOMCPServer(resourceName, nil, &mcpv1beta1.ServiceReference{
				Name: "nonexistent-svc",
			})
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
		})

		It("should set Accepted=False with descriptive message", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAccepted)
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("Service"))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-svc"))

			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCondition.Reason).To(Equal(ReasonConfigurationInvalid))
		})
	})

	Context("When a BYO Service translates the managed container port", func() {
		const resourceName = "test-byo-service-explicit-port"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-mcp-svc-port",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 80, TargetPort: intstr.FromInt32(8080), Protocol: corev1.ProtocolTCP},
					},
					Selector: map[string]string{"app": "my-mcp"},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, nil, &mcpv1beta1.ServiceReference{
				Name: "my-mcp-svc-port",
			})
			server.Spec.Config.Port = 8080
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-svc-port", Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
		})

		It("should verify the Service port while retaining the container and probe ports", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			var dialedURL string
			controllerReconciler.MCPDialer = func(_ context.Context, url string, _ *http.Transport) (*mcpv1beta1.MCPServerInfo, error) {
				dialedURL = url
				return &mcpv1beta1.MCPServerInfo{Name: "test-server"}, nil
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(8080)))
			Expect(dep.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port).To(Equal(intstr.FromInt32(8080)))
			dep.Status.ReadyReplicas = 1
			dep.Status.Replicas = 1
			dep.Status.Conditions = []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			}
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			Expect(dialedURL).To(Equal("http://my-mcp-svc-port.default.svc.cluster.local:80/mcp"))
			Expect(mcpServer.Status.Address).NotTo(BeNil())
			Expect(mcpServer.Status.Address.URL).To(ContainSubstring(":80/"))
			Expect(mcpServer.Status.Address.URL).NotTo(ContainSubstring(":8080/"))
			Expect(mcpServer.Status.Address.URL).To(ContainSubstring("my-mcp-svc-port"))
		})
	})
})

var _ = Describe("resolveServicePort - unit tests", func() {
	ctx := context.Background()

	It("should return error for missing Service", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).Build()

		_, err := resolveServicePort(ctx, fakeClient, "no-such-svc", "default", 0)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("referenced Service no-such-svc not found"))
	})

	It("should return error when Service has no ports", func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "no-ports-svc", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "test"}},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(svc).Build()

		_, err := resolveServicePort(ctx, fakeClient, "no-ports-svc", "default", 0)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("has no ports"))
	})

	It("should return configPort when > 0 without fetching Service", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).Build()

		port, err := resolveServicePort(ctx, fakeClient, "any-svc", "default", 9999)
		Expect(err).NotTo(HaveOccurred())
		Expect(port).To(Equal(int32(9999)))
	})

	It("should prefer mcp-named port over others", func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "multi-port-svc", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "metrics", Port: 9090},
					{Name: "mcp", Port: 4000},
					{Name: "admin", Port: 8081},
				},
				Selector: map[string]string{"app": "test"},
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(svc).Build()

		port, err := resolveServicePort(ctx, fakeClient, "multi-port-svc", "default", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(port).To(Equal(int32(4000)))
	})

	It("should fall back to first port when no mcp-named port exists", func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "single-port-svc", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 8080},
				},
				Selector: map[string]string{"app": "test"},
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(svc).Build()

		port, err := resolveServicePort(ctx, fakeClient, "single-port-svc", "default", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(port).To(Equal(int32(8080)))
	})
})
