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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

var _ = Describe("BYO workload fetch errors", func() {
	DescribeTable("preserves API error classification and workload identity", func(kind mcpv1beta1.WorkloadKind) {
		reader := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).Build()
		workload, err := getWorkload(context.Background(), reader, "missing-workload", kind, "default")
		Expect(workload).To(BeNil())
		Expect(errors.IsNotFound(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring(string(kind)))
		Expect(err.Error()).To(ContainSubstring("missing-workload"))
	},
		Entry("Deployment", mcpv1beta1.WorkloadKindDeployment),
		Entry("DaemonSet", mcpv1beta1.WorkloadKindDaemonSet),
		Entry("StatefulSet", mcpv1beta1.WorkloadKindStatefulSet),
	)
	It("rejects an unsupported kind as a configuration error", func() {
		reader := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).Build()
		_, err := getWorkload(context.Background(), reader, "anything", mcpv1beta1.WorkloadKind("CronJob"), "default")
		Expect(err).To(BeAssignableToTypeOf(&ValidationError{}))
		Expect(err.Error()).To(ContainSubstring("unsupported workloadRef kind: CronJob"))
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

var _ = Describe("BYO port admission", func() {
	It("should require a container port for a managed workload even with serviceRef", func() {
		server := newTestBYOMCPServer("managed-byo-service-no-port", nil, &mcpv1beta1.ServiceReference{Name: "external"})
		server.Spec.Config.Port = 0
		err := k8sClient.Create(context.Background(), server)
		Expect(errors.IsInvalid(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("config.port is required"))
	})
})

var _ = Describe("BYO workload mode admission", func() {
	DescribeTable("should preserve workload management mode on updates", func(byo bool) {
		ctx := context.Background()
		name := "immutable-managed-mode"
		var ref *mcpv1beta1.WorkloadReference
		if byo {
			name = "immutable-byo-mode"
			ref = &mcpv1beta1.WorkloadReference{Name: "external", Kind: mcpv1beta1.WorkloadKindDeployment}
		}
		server := newTestBYOMCPServer(name, ref, nil)
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, server)).To(Succeed()) })

		// Ordinary updates and changing an existing BYO reference remain valid.
		server.Labels = map[string]string{"team": "platform"}
		if byo {
			server.Spec.WorkloadRef.Name = "another-workload"
			server.Spec.WorkloadRef.Kind = mcpv1beta1.WorkloadKindStatefulSet
		}
		Expect(k8sClient.Update(ctx, server)).To(Succeed())

		if byo {
			server.Spec.WorkloadRef = nil
			server.Spec.Source = newTestBYOMCPServer("unused", nil, nil).Spec.Source
		} else {
			server.Spec.Source = mcpv1beta1.Source{}
			server.Spec.WorkloadRef = &mcpv1beta1.WorkloadReference{Name: "external", Kind: mcpv1beta1.WorkloadKindDeployment}
		}
		err := k8sClient.Update(ctx, server)
		Expect(errors.IsInvalid(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("workloadRef cannot be added or removed after creation"))
	},
		Entry("managed to BYO", false),
		Entry("BYO to managed", true),
	)
})

var _ = Describe("BYO workload mixed selector", func() {
	It("should require serviceRef rather than broaden a mixed workload selector", func() {
		ctx := context.Background()
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "mixed-selector-workload", Namespace: "default"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels:      map[string]string{"app": "mcp"},
					MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "version", Operator: metav1.LabelSelectorOpIn, Values: []string{"v2", "v3"}}},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "mcp", "version": "v2"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "server", Image: "mcp:latest"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, dep)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, dep)).To(Succeed()) })
		server := newTestBYOMCPServer("mixed-selector-server", &mcpv1beta1.WorkloadReference{Name: dep.Name, Kind: mcpv1beta1.WorkloadKindDeployment}, nil)
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, server)).To(Succeed()) })
		r := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(server)}
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, req.NamespacedName, server)).To(Succeed())
		accepted := meta.FindStatusCondition(server.Status.Conditions, ConditionTypeAccepted)
		Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
		Expect(accepted.Message).To(ContainSubstring("matchExpressions"))
		Expect(accepted.Message).To(ContainSubstring("serviceRef"))
		Expect(errors.IsNotFound(k8sClient.Get(ctx, req.NamespacedName, &corev1.Service{}))).To(BeTrue())

		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "mixed-selector-service", Namespace: "default"}, Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "mcp", "version": "v2"}, Ports: []corev1.ServicePort{{Port: 8080}},
		}}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, svc)).To(Succeed()) })
		server.Spec.ServiceRef = &mcpv1beta1.ServiceReference{Name: svc.Name}
		Expect(k8sClient.Update(ctx, server)).To(Succeed())
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, req.NamespacedName, server)).To(Succeed())
		Expect(meta.FindStatusCondition(server.Status.Conditions, ConditionTypeAccepted).Status).To(Equal(metav1.ConditionTrue))
	})
})

var _ = Describe("BYO availability from workload status", func() {
	daemonSet := func(status appsv1.DaemonSetStatus) client.Object {
		return &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "status-ds", Namespace: "default", Generation: 2}, Status: status}
	}
	statefulSet := func(replicas int32, status appsv1.StatefulSetStatus) client.Object {
		return &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "status-sts", Namespace: "default", Generation: 2}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}, Status: status}
	}
	deployment := func(replicas int32, observed int64) client.Object {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "status-deployment", Namespace: "default", Generation: 2},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			Status: appsv1.DeploymentStatus{
				ObservedGeneration: observed, ReadyReplicas: 1, AvailableReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}},
			},
		}
	}
	DescribeTable("uses availability rather than requiring a completed rollout", func(obj client.Object, kind mcpv1beta1.WorkloadKind, expected metav1.ConditionStatus, reason, message string) {
		ctx := context.Background()
		reader := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(obj).Build()
		workload, err := getWorkload(ctx, reader, obj.GetName(), kind, obj.GetNamespace())
		Expect(err).NotTo(HaveOccurred())
		server := newTestBYOMCPServer("status-server", &mcpv1beta1.WorkloadReference{Name: obj.GetName(), Kind: kind}, nil)
		r := newReconcilerForTest(reader, k8sClient.Scheme())
		observation, err := r.observeWorkload(ctx, server, workload, metav1.Condition{Status: metav1.ConditionTrue})
		Expect(err).NotTo(HaveOccurred())
		condition := observation.availability
		Expect(condition.Status).To(Equal(expected))
		Expect(condition.Reason).To(Equal(reason))
		Expect(condition.Message).To(ContainSubstring(message))
	},
		Entry("DaemonSet with partial availability and incomplete scheduling", daemonSet(appsv1.DaemonSetStatus{
			ObservedGeneration: 2, DesiredNumberScheduled: 3, CurrentNumberScheduled: 2, NumberReady: 2, NumberAvailable: 1, NumberUnavailable: 2,
		}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionTrue, ReasonAvailable, "2 unavailable; 2 of 3 instances scheduled"),
		Entry("DaemonSet ready pods have not met minReadySeconds", daemonSet(appsv1.DaemonSetStatus{
			ObservedGeneration: 2, DesiredNumberScheduled: 3, CurrentNumberScheduled: 3, NumberReady: 3, NumberAvailable: 0, NumberUnavailable: 3,
		}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "0 of 3 instances available (3 ready)"),
		Entry("DaemonSet with no status", daemonSet(appsv1.DaemonSetStatus{}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionUnknown, ReasonInitializing, "report status"),
		Entry("observed DaemonSet with no matching nodes", daemonSet(appsv1.DaemonSetStatus{ObservedGeneration: 2}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "no nodes matching"),
		Entry("DaemonSet status predates spec update", daemonSet(appsv1.DaemonSetStatus{
			ObservedGeneration: 1, DesiredNumberScheduled: 3, NumberReady: 3, NumberAvailable: 3,
		}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "processing spec update"),
		Entry("StatefulSet with partial availability during rollout", statefulSet(3, appsv1.StatefulSetStatus{
			ObservedGeneration: 2, ReadyReplicas: 2, AvailableReplicas: 1, UpdatedReplicas: 2,
		}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionTrue, ReasonAvailable, "rolling update in progress"),
		Entry("StatefulSet ready pods have not met minReadySeconds", statefulSet(3, appsv1.StatefulSetStatus{
			ObservedGeneration: 2, ReadyReplicas: 3, AvailableReplicas: 0, UpdatedReplicas: 3,
		}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "0 of 3 instances available (3 ready)"),
		Entry("StatefulSet scaled to zero with lingering replicas", statefulSet(0, appsv1.StatefulSetStatus{
			ObservedGeneration: 1, ReadyReplicas: 3, AvailableReplicas: 3,
		}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionTrue, ReasonScaledToZero, "scaled to zero"),
		Entry("StatefulSet with no status", statefulSet(3, appsv1.StatefulSetStatus{}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionUnknown, ReasonInitializing, "report status"),
		Entry("StatefulSet status predates spec update", statefulSet(3, appsv1.StatefulSetStatus{
			ObservedGeneration: 1, ReadyReplicas: 3, AvailableReplicas: 3,
		}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "processing spec update"),
		Entry("Deployment uses managed availability semantics for partial availability", deployment(3, 2), mcpv1beta1.WorkloadKindDeployment, metav1.ConditionTrue, ReasonAvailable, "1 of 3 instances healthy"),
		Entry("Deployment status predates spec update", deployment(3, 1), mcpv1beta1.WorkloadKindDeployment, metav1.ConditionFalse, ReasonWorkloadUnavailable, "processing spec update"),
		Entry("Deployment scaled to zero with lingering replicas", deployment(0, 2), mcpv1beta1.WorkloadKindDeployment, metav1.ConditionTrue, ReasonScaledToZero, "scaled to 0"),
	)
})
