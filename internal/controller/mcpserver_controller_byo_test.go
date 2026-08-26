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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

func newTestBYOMCPServer(name string, workloadRef *mcpv1alpha1.WorkloadReference, serviceRef *mcpv1alpha1.ServiceReference) *mcpv1alpha1.MCPServer {
	server := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Config: mcpv1alpha1.ServerConfig{
				Port: 8080,
			},
		},
	}
	if workloadRef != nil {
		server.Spec.WorkloadRef = workloadRef
	} else {
		server.Spec.Source = mcpv1alpha1.Source{
			Type: mcpv1alpha1.SourceTypeContainerImage,
			ContainerImage: &mcpv1alpha1.ContainerImageSource{
				Ref: "docker.io/library/test-image:latest",
			},
		}
	}
	if serviceRef != nil {
		server.Spec.ServiceRef = serviceRef
	}
	return server
}

var _ = Describe("MCPServer Controller - BYO Workload", func() {
	ctx := context.Background()

	Context("When reconciling with BYO Deployment workloadRef", func() {
		const resourceName = "test-byo-deployment"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-mcp-deploy",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "my-mcp"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "my-mcp"}},
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
			dep.Status.ReadyReplicas = 2
			dep.Status.Replicas = 2
			dep.Status.Conditions = []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
				{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
			}
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1alpha1.WorkloadReference{
				Name: "my-mcp-deploy",
				Kind: mcpv1alpha1.WorkloadKindDeployment,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-deploy", Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
		})

		It("should not create an operator-managed Deployment and should reflect BYO workload status", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			operatorDep := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, operatorDep)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "no operator-managed Deployment should be created for BYO workload")

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(ReasonAvailable))

			Expect(mcpServer.Status.WorkloadName).To(Equal("Deployment/my-mcp-deploy"))
			Expect(mcpServer.Status.WorkloadSummary).To(Equal("BYO:Deployment/my-mcp-deploy"))
			Expect(mcpServer.Status.Replicas).To(Equal(int32(2)))
			Expect(mcpServer.Status.ReadyReplicas).To(Equal(int32(2)))
		})
	})

	Context("When reconciling with BYO DaemonSet workloadRef", func() {
		const resourceName = "test-byo-daemonset"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			ds := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-mcp-ds",
					Namespace: "default",
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "my-mcp-ds"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "my-mcp-ds"}},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "server",
								Image: "my-mcp:latest",
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())
			ds.Status.NumberReady = 3
			ds.Status.DesiredNumberScheduled = 3
			Expect(k8sClient.Status().Update(ctx, ds)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1alpha1.WorkloadReference{
				Name: "my-mcp-ds",
				Kind: mcpv1alpha1.WorkloadKindDaemonSet,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			ds := &appsv1.DaemonSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-ds", Namespace: "default"}, ds); err == nil {
				Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
			}
		})

		It("should reflect DaemonSet status using NumberReady/DesiredNumberScheduled", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(ReasonAvailable))

			Expect(mcpServer.Status.WorkloadName).To(Equal("DaemonSet/my-mcp-ds"))
			Expect(mcpServer.Status.WorkloadSummary).To(Equal("BYO:DaemonSet/my-mcp-ds"))
			Expect(mcpServer.Status.Replicas).To(Equal(int32(3)))
			Expect(mcpServer.Status.ReadyReplicas).To(Equal(int32(3)))
		})
	})

	Context("When reconciling with BYO StatefulSet workloadRef", func() {
		const resourceName = "test-byo-statefulset"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-mcp-sts",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "my-mcp-sts"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "my-mcp-sts"}},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "server",
								Image: "my-mcp:latest",
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sts)).To(Succeed())
			sts.Status.ReadyReplicas = 3
			sts.Status.Replicas = 3
			Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1alpha1.WorkloadReference{
				Name: "my-mcp-sts",
				Kind: mcpv1alpha1.WorkloadKindStatefulSet,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			sts := &appsv1.StatefulSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-sts", Namespace: "default"}, sts); err == nil {
				Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
			}
		})

		It("should reflect StatefulSet status using ReadyReplicas/Spec.Replicas", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(ReasonAvailable))

			Expect(mcpServer.Status.WorkloadName).To(Equal("StatefulSet/my-mcp-sts"))
			Expect(mcpServer.Status.WorkloadSummary).To(Equal("BYO:StatefulSet/my-mcp-sts"))
			Expect(mcpServer.Status.Replicas).To(Equal(int32(3)))
			Expect(mcpServer.Status.ReadyReplicas).To(Equal(int32(3)))
		})
	})

	Context("When BYO workload reference does not exist", func() {
		const resourceName = "test-byo-missing-workload"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			server := newTestBYOMCPServer(resourceName, &mcpv1alpha1.WorkloadReference{
				Name: "nonexistent-deploy",
				Kind: mcpv1alpha1.WorkloadKindDeployment,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Accepted=False with descriptive message", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("Deployment"))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-deploy"))

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal(ReasonConfigurationInvalid))
		})
	})
})

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

			server := newTestBYOMCPServer(resourceName, &mcpv1alpha1.WorkloadReference{
				Name: "byo-svc-deploy",
				Kind: mcpv1alpha1.WorkloadKindDeployment,
			}, &mcpv1alpha1.ServiceReference{
				Name: "my-mcp-svc",
			})
			server.Spec.Config.Port = 0
			server.Spec.Config.Path = defaultMCPPath
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
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

			mcpServer := &mcpv1alpha1.MCPServer{}
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

			server := newTestBYOMCPServer(resourceName, &mcpv1alpha1.WorkloadReference{
				Name: "byo-svc-mcp-deploy",
				Kind: mcpv1alpha1.WorkloadKindDeployment,
			}, &mcpv1alpha1.ServiceReference{
				Name: "my-mcp-svc-multi",
			})
			server.Spec.Config.Port = 0
			server.Spec.Config.Path = defaultMCPPath
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
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

			mcpServer := &mcpv1alpha1.MCPServer{}
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
			server := newTestBYOMCPServer(resourceName, nil, &mcpv1alpha1.ServiceReference{
				Name: "nonexistent-svc",
			})
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set Accepted=False with descriptive message", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("Service"))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-svc"))

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal(ReasonConfigurationInvalid))
		})
	})

	Context("When serviceRef is set with explicit config.port", func() {
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
						{Name: "http", Port: 9090, Protocol: corev1.ProtocolTCP},
					},
					Selector: map[string]string{"app": "my-mcp"},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, nil, &mcpv1alpha1.ServiceReference{
				Name: "my-mcp-svc-port",
			})
			server.Spec.Config.Port = 7777
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-svc-port", Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
		})

		It("should use config.port instead of resolving from Service", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, dep)).To(Succeed())
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

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			Expect(mcpServer.Status.Address).NotTo(BeNil())
			Expect(mcpServer.Status.Address.URL).To(ContainSubstring("7777"))
			Expect(mcpServer.Status.Address.URL).NotTo(ContainSubstring("9090"))
			Expect(mcpServer.Status.Address.URL).To(ContainSubstring("my-mcp-svc-port"))
		})
	})
})

var _ = Describe("MCPServer Controller - BYO Combined", func() {
	ctx := context.Background()

	Context("When reconciling with both workloadRef and serviceRef", func() {
		const resourceName = "test-byo-combined"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "combined-deploy",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "combined"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "combined"}},
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
			dep.Status.ReadyReplicas = 2
			dep.Status.Replicas = 2
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "combined-svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "mcp", Port: 5000, Protocol: corev1.ProtocolTCP},
					},
					Selector: map[string]string{"app": "combined"},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1alpha1.WorkloadReference{
				Name: "combined-deploy",
				Kind: mcpv1alpha1.WorkloadKindDeployment,
			}, &mcpv1alpha1.ServiceReference{
				Name: "combined-svc",
			})
			server.Spec.Config.Port = 0
			server.Spec.Config.Path = defaultMCPPath
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "combined-deploy", Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "combined-svc", Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
		})

		It("should use both BYO workload status and BYO service for URL", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			operatorDep := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, operatorDep)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "no operator-managed Deployment should be created")

			operatorSvc := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, operatorSvc)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "no operator-managed Service should be created")

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			Expect(mcpServer.Status.WorkloadName).To(Equal("Deployment/combined-deploy"))
			Expect(mcpServer.Status.WorkloadSummary).To(Equal("BYO:Deployment/combined-deploy"))
			Expect(mcpServer.Status.ServiceName).To(Equal("combined-svc"))
			Expect(mcpServer.Status.Replicas).To(Equal(int32(2)))
			Expect(mcpServer.Status.ReadyReplicas).To(Equal(int32(2)))

			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Reason).To(Equal(ReasonAvailable))
		})
	})

	Context("Existing MCPServer without refs still works (backward compatibility)", func() {
		const resourceName = "test-backward-compat"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			server := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/compat-image:v1",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
		})

		It("should create operator-managed Deployment and Service as before", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("docker.io/library/compat-image:v1"))

			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, svc)).To(Succeed())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			Expect(mcpServer.Status.DeploymentName).To(Equal(resourceName))
			Expect(mcpServer.Status.ServiceName).To(Equal(resourceName))
			Expect(mcpServer.Status.WorkloadSummary).To(Equal("docker.io/library/compat-image:v1"))
			Expect(mcpServer.Status.WorkloadName).To(BeEmpty())
		})
	})
})
