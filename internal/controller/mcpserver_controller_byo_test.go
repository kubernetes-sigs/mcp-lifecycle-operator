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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func newTestBYOMCPServer(name string, workloadRef *mcpv1beta1.WorkloadReference, serviceRef *mcpv1beta1.ServiceReference) *mcpv1beta1.MCPServer {
	server := &mcpv1beta1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: mcpv1beta1.MCPServerSpec{
			Config: mcpv1beta1.ServerConfig{
				Port: 8080,
			},
		},
	}
	if workloadRef != nil {
		server.Spec.WorkloadRef = workloadRef
	} else {
		server.Spec.Source = mcpv1beta1.Source{
			Type: mcpv1beta1.SourceTypeContainerImage,
			ContainerImage: &mcpv1beta1.ContainerImageSource{
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

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "my-mcp-deploy",
				Kind: mcpv1beta1.WorkloadKindDeployment,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-deploy", Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
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

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(availableCondition.Reason).To(Equal(ReasonAvailable))

			Expect(mcpServer.Status.WorkloadName).To(Equal("Deployment/my-mcp-deploy"))
			Expect(mcpServer.Status.WorkloadSummary).To(Equal("BYO:Deployment/my-mcp-deploy"))
			Expect(mcpServer.Status.Replicas).To(Equal(int32(2)))
			Expect(mcpServer.Status.ReadyReplicas).To(Equal(int32(2)))
		})

		It("should retain BYO identity when the referenced workload disappears", func() {
			r := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			req := reconcile.Request{NamespacedName: typeNamespacedName}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			server := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			Expect(server.Status.WorkloadName).To(Equal("Deployment/my-mcp-deploy"))
			Expect(server.Status.WorkloadSummary).To(Equal("BYO:Deployment/my-mcp-deploy"))
			baseline := server.Status
			Expect(k8sClient.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "my-mcp-deploy", Namespace: "default"}})).To(Succeed())

			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			Expect(meta.FindStatusCondition(server.Status.Conditions, ConditionTypeAccepted).Status).To(Equal(metav1.ConditionFalse))
			Expect(server.Status.WorkloadName).To(Equal(baseline.WorkloadName))
			Expect(server.Status.WorkloadSummary).To(Equal(baseline.WorkloadSummary))
			Expect(server.Status.ServiceName).To(Equal(baseline.ServiceName))
			Expect(server.Status.Replicas).To(Equal(baseline.Replicas))
			Expect(server.Status.ReadyReplicas).To(Equal(baseline.ReadyReplicas))
		})

		It("should create a Service using the BYO workload's selector labels", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, svc)).To(Succeed())
			Expect(svc.Spec.Selector).To(Equal(map[string]string{"app": "my-mcp"}))
			Expect(svc.Spec.Ports[0].TargetPort).To(Equal(intstr.FromInt32(8080)), "BYO pods have no named container port")
		})
		It("should register a BYO workload with a gateway and publish its address", func() {
			server := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			server.Spec.Gateway = &mcpv1beta1.GatewaySpec{Provider: "httproute", ConfigRef: "gw-config"}
			Expect(k8sClient.Update(ctx, server)).To(Succeed())
			r := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			req := reconcile.Request{NamespacedName: typeNamespacedName}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			binding := &mcpv1alpha1.MCPGatewayBinding{}
			key := types.NamespacedName{Name: gatewayBindingName(resourceName), Namespace: "default"}
			Expect(k8sClient.Get(ctx, key, binding)).To(Succeed())
			DeferCleanup(func() { Expect(k8sClient.Delete(ctx, binding)).To(Succeed()) })
			Expect(binding.Spec.MCPServerRef).To(Equal(resourceName))
			Expect(binding.Spec.ConfigRef).To(Equal("gw-config"))
			Expect(metav1.GetControllerOf(binding).UID).To(Equal(server.UID))
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			Expect(meta.FindStatusCondition(server.Status.Conditions, ConditionTypeGatewayRegistered).Status).To(Equal(metav1.ConditionFalse))
			Expect(meta.FindStatusCondition(server.Status.Conditions, ConditionTypeAvailable).Reason).To(Equal(ReasonGatewayNotRegistered))

			binding.Status.URL = testGatewayURL
			meta.SetStatusCondition(&binding.Status.Conditions, metav1.Condition{
				Type: ConditionTypeRegistered, Status: metav1.ConditionTrue, Reason: "Registered", ObservedGeneration: binding.Generation,
			})
			Expect(k8sClient.Status().Update(ctx, binding)).To(Succeed())
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			Expect(meta.FindStatusCondition(server.Status.Conditions, ConditionTypeGatewayRegistered).Status).To(Equal(metav1.ConditionTrue))
			Expect(meta.FindStatusCondition(server.Status.Conditions, ConditionTypeAvailable).Status).To(Equal(metav1.ConditionTrue))
			Expect(server.Status.GatewayBinding.Name).To(Equal(binding.Name))
			Expect(server.Status.Address.URL).To(Equal(testGatewayURL))
		})

		DescribeTable("BYO resource failure status", func(gatewayFailure bool) {
			r := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			req := reconcile.Request{NamespacedName: typeNamespacedName}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			server := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			Expect(meta.FindStatusCondition(server.Status.Conditions, ConditionTypeVerified).Status).To(Equal(metav1.ConditionTrue))
			Expect(server.Status.Address).NotTo(BeNil())

			expectedReason := ReasonServiceUnavailable
			if gatewayFailure {
				server.Spec.Gateway = &mcpv1beta1.GatewaySpec{Provider: "httproute"}
				expectedReason = ReasonGatewayNotRegistered
			} else {
				server.Spec.Config.Port++ // Force an update of the managed Service.
			}
			Expect(k8sClient.Update(ctx, server)).To(Succeed())
			wrapped, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())
			r.Client = interceptor.NewClient(wrapped, interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*mcpv1alpha1.MCPGatewayBinding); ok {
						return fmt.Errorf("injected binding failure")
					}
					return c.Create(ctx, obj, opts...)
				},
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if _, ok := obj.(*corev1.Service); ok {
						return fmt.Errorf("injected Service failure")
					}
					return c.Update(ctx, obj, opts...)
				},
			})
			_, err = r.Reconcile(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, server)).To(Succeed())
			available := meta.FindStatusCondition(server.Status.Conditions, ConditionTypeAvailable)
			Expect(available.Status).To(Equal(metav1.ConditionFalse))
			Expect(available.Reason).To(Equal(expectedReason))
			Expect(meta.FindStatusCondition(server.Status.Conditions, ConditionTypeVerified).Status).To(Equal(metav1.ConditionUnknown))
			Expect(server.Status.Address).To(BeNil())
			Expect(server.Status.WorkloadName).To(Equal("Deployment/my-mcp-deploy"))
		},
			Entry("Service update failure withdraws verification", false),
			Entry("gateway binding failure withdraws verification", true),
		)

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
			ds.Status.ObservedGeneration = ds.Generation
			ds.Status.NumberAvailable = ds.Status.NumberReady
			ds.Status.CurrentNumberScheduled = ds.Status.DesiredNumberScheduled
			Expect(k8sClient.Status().Update(ctx, ds)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "my-mcp-ds",
				Kind: mcpv1beta1.WorkloadKindDaemonSet,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			ds := &appsv1.DaemonSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-ds", Namespace: "default"}, ds); err == nil {
				Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
		})

		It("should reflect DaemonSet status using NumberReady/DesiredNumberScheduled", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(availableCondition.Reason).To(Equal(ReasonAvailable))

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
			sts.Status.ObservedGeneration = sts.Generation
			sts.Status.AvailableReplicas = sts.Status.ReadyReplicas
			sts.Status.UpdatedReplicas = sts.Status.ReadyReplicas
			Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "my-mcp-sts",
				Kind: mcpv1beta1.WorkloadKindStatefulSet,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			sts := &appsv1.StatefulSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "my-mcp-sts", Namespace: "default"}, sts); err == nil {
				Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
		})

		It("should reflect StatefulSet status using ReadyReplicas/Spec.Replicas", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(availableCondition.Reason).To(Equal(ReasonAvailable))

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
			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "nonexistent-deploy",
				Kind: mcpv1beta1.WorkloadKindDeployment,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
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

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAccepted)
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("Deployment"))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-deploy"))

			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCondition.Reason).To(Equal(ReasonConfigurationInvalid))
		})
	})

	Context("When BYO workload has only matchExpressions (no matchLabels)", func() {
		const resourceName = "test-byo-match-expressions"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "expr-only-deploy",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](1),
					Selector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{{
							Key:      "app",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"my-mcp"},
						}},
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

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "expr-only-deploy",
				Kind: mcpv1beta1.WorkloadKindDeployment,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "expr-only-deploy", Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
		})

		It("should set Accepted=False because Service selector requires matchLabels", func() {
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
			Expect(acceptedCondition.Message).To(ContainSubstring("matchLabels"))
			Expect(acceptedCondition.Message).To(ContainSubstring("serviceRef"))
		})
	})
})

var _ = Describe("MCPServer Controller - BYO not-ready workload", func() {
	ctx := context.Background()

	Context("When BYO Deployment is not ready", func() {
		const resourceName = "test-byo-not-ready"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-ready-deploy",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "not-ready"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "not-ready"}},
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
			dep.Status.ReadyReplicas = 0
			dep.Status.Replicas = 3
			dep.Status.ObservedGeneration = dep.Generation
			dep.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionFalse, Message: "waiting for replicas"}}
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "not-ready-deploy",
				Kind: mcpv1beta1.WorkloadKindDeployment,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "not-ready-deploy", Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
		})

		It("should set Available=False with WorkloadUnavailable reason", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCondition.Reason).To(Equal(ReasonWorkloadUnavailable))
			Expect(availableCondition.Message).To(ContainSubstring("waiting for replicas"))
		})
	})

	Context("When BYO Deployment is scaled to zero", func() {
		const resourceName = "test-byo-scaled-zero"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "zero-scale-deploy",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To[int32](0),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "zero-scale"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "zero-scale"}},
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
			dep.Status.ReadyReplicas = 0
			dep.Status.Replicas = 0
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "zero-scale-deploy",
				Kind: mcpv1beta1.WorkloadKindDeployment,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "zero-scale-deploy", Namespace: "default"}, dep); err == nil {
				Expect(k8sClient.Delete(ctx, dep)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
		})

		It("should set Available=True with ScaledToZero reason", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(availableCondition.Reason).To(Equal(ReasonScaledToZero))
			Expect(availableCondition.Message).To(ContainSubstring("scaled to 0"))
		})
	})

	Context("When BYO DaemonSet has no schedulable nodes", func() {
		const resourceName = "test-byo-no-schedulable-nodes"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			ds := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "init-ds",
					Namespace: "default",
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "init-ds"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "init-ds"}},
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
			ds.Status.ObservedGeneration = ds.Generation
			Expect(k8sClient.Status().Update(ctx, ds)).To(Succeed())

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "init-ds",
				Kind: mcpv1beta1.WorkloadKindDaemonSet,
			}, nil)
			Expect(k8sClient.Create(ctx, server)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			ds := &appsv1.DaemonSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "init-ds", Namespace: "default"}, ds); err == nil {
				Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
			}
			svc := &corev1.Service{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, svc); err == nil {
				Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			}
		})

		It("should set Available=False with WorkloadUnavailable reason", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCondition.Reason).To(Equal(ReasonWorkloadUnavailable))
			Expect(availableCondition.Message).To(ContainSubstring("no nodes matching"))
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
			dep.Status.AvailableReplicas = 2
			dep.Status.ObservedGeneration = dep.Generation
			dep.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}}
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

			server := newTestBYOMCPServer(resourceName, &mcpv1beta1.WorkloadReference{
				Name: "combined-deploy",
				Kind: mcpv1beta1.WorkloadKindDeployment,
			}, &mcpv1beta1.ServiceReference{
				Name: "combined-svc",
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

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			Expect(mcpServer.Status.WorkloadName).To(Equal("Deployment/combined-deploy"))
			Expect(mcpServer.Status.WorkloadSummary).To(Equal("BYO:Deployment/combined-deploy"))
			Expect(mcpServer.Status.ServiceName).To(Equal("combined-svc"))
			Expect(mcpServer.Status.Replicas).To(Equal(int32(2)))
			Expect(mcpServer.Status.ReadyReplicas).To(Equal(int32(2)))

			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(availableCondition.Reason).To(Equal(ReasonAvailable))
		})
	})

	Context("Existing MCPServer without refs still works (backward compatibility)", func() {
		const resourceName = "test-backward-compat"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			server := &mcpv1beta1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1beta1.MCPServerSpec{
					Source: mcpv1beta1.Source{
						Type: mcpv1beta1.SourceTypeContainerImage,
						ContainerImage: &mcpv1beta1.ContainerImageSource{
							Ref: "docker.io/library/compat-image:v1",
						},
					},
					Config: mcpv1beta1.ServerConfig{
						Port: 8080,
					},
				},
			}
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

			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			Expect(mcpServer.Status.DeploymentName).To(Equal(resourceName))
			Expect(mcpServer.Status.ServiceName).To(Equal(resourceName))
			Expect(mcpServer.Status.WorkloadSummary).To(Equal("docker.io/library/compat-image:v1"))
			Expect(mcpServer.Status.WorkloadName).To(BeEmpty())
		})
	})
})
