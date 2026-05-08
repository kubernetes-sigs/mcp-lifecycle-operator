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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Controller - Owned Resource Cleanup", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-ownerref"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)
			if err == nil {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			}
		})

		It("should set controller owner reference on created Deployment", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())

			Expect(deployment.OwnerReferences).To(HaveLen(1))
			ownerRef := deployment.OwnerReferences[0]
			Expect(ownerRef.Name).To(Equal(mcpServer.Name))
			Expect(ownerRef.UID).To(Equal(mcpServer.UID))
			Expect(*ownerRef.Controller).To(BeTrue())
			Expect(ownerRef.Kind).To(Equal("MCPServer"))
			Expect(ownerRef.APIVersion).To(Equal("mcp.x-k8s.io/v1alpha1"))
		})

		It("should set controller owner reference on created Service", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())

			Expect(service.OwnerReferences).To(HaveLen(1))
			ownerRef := service.OwnerReferences[0]
			Expect(ownerRef.Name).To(Equal(mcpServer.Name))
			Expect(ownerRef.UID).To(Equal(mcpServer.UID))
			Expect(*ownerRef.Controller).To(BeTrue())
			Expect(ownerRef.Kind).To(Equal("MCPServer"))
			Expect(ownerRef.APIVersion).To(Equal("mcp.x-k8s.io/v1alpha1"))
		})

		It("should preserve owner references across reconciliation updates", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling to create initial resources")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			originalUID := mcpServer.UID

			By("Updating the MCPServer port to trigger a Service update")
			mcpServer.Spec.Config.Port = 9090
			Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

			By("Reconciling again after the update")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Deployment owner reference is preserved")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].UID).To(Equal(originalUID))
			Expect(*deployment.OwnerReferences[0].Controller).To(BeTrue())

			By("Verifying Service owner reference is preserved")
			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].UID).To(Equal(originalUID))
			Expect(*service.OwnerReferences[0].Controller).To(BeTrue())
		})
	})
})

var _ = Describe("MCPServer Controller - Foreign Owned Resources", func() {
	ctx := context.Background()

	Context("When a Deployment already exists with a different owner", func() {
		const resourceName = "test-foreign-deploy"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Pre-creating a Deployment owned by a different controller")
			foreignDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "SomeOtherController",
							Name:       "foreign-owner",
							UID:        types.UID("foreign-controller-uid"),
							Controller: new(true),
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: managedWorkloadSelector(resourceName),
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: managedWorkloadLabels(resourceName),
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "other", Image: "other-image:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, foreignDeployment)).To(Succeed())

			By("Creating the MCPServer CR")
			resource := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should reject updating deployment when owned by another controller", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is owned by"))
			Expect(err.Error()).To(ContainSubstring("cannot be managed by MCPServer"))

			By("Verifying the deployment spec was NOT overwritten")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("other-image:latest"))

			By("Verifying the original foreign owner reference is still present")
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].Name).To(Equal("foreign-owner"))
			Expect(*deployment.OwnerReferences[0].Controller).To(BeTrue())
		})
	})

	Context("When a Service already exists with a different owner", func() {
		const resourceName = "test-foreign-svc"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Pre-creating a Service owned by a different controller")
			foreignService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "SomeOtherController",
							Name:       "foreign-svc-owner",
							UID:        types.UID("foreign-svc-controller-uid"),
							Controller: new(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: managedWorkloadSelector(resourceName),
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       9999,
							TargetPort: intstr.FromInt32(9999),
							Protocol:   corev1.ProtocolTCP,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, foreignService)).To(Succeed())

			By("Creating the MCPServer CR")
			resource := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)
			if err == nil {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			}
		})

		It("should reject updating service when owned by another controller", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is owned by"))
			Expect(err.Error()).To(ContainSubstring("cannot be managed by MCPServer"))

			By("Verifying the service port was NOT updated")
			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(9999)))

			By("Verifying the original foreign owner reference is still present")
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].Name).To(Equal("foreign-svc-owner"))
			Expect(*service.OwnerReferences[0].Controller).To(BeTrue())
		})
	})

	Context("When a Deployment exists with no controller owner", func() {
		const resourceName = "test-unowned-deploy"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Pre-creating a Deployment with no owner")
			unownedDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
					// No ownerReferences - simulates manually created resource
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{LabelKeyApp: "manual"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{LabelKeyApp: "manual"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "manual", Image: "manual-image:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, unownedDeployment)).To(Succeed())

			By("Creating the MCPServer CR")
			resource := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should reject updating unowned deployment", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("has no controller owner"))
			Expect(err.Error()).To(ContainSubstring("delete the resource first or choose a different name"))

			By("Verifying the deployment was NOT updated")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("manual-image:latest"))

			By("Verifying the deployment still has no owner")
			Expect(deployment.OwnerReferences).To(BeEmpty())

			By("Verifying the MCPServer status shows deployment unavailable with ownership error")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("DeploymentUnavailable"))
			Expect(readyCondition.Message).To(ContainSubstring("has no controller owner"))
		})
	})

	Context("When a Service exists with no controller owner", func() {
		const resourceName = "test-unowned-svc"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Creating MCPServer first to create Deployment")
			mcpServer := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			By("Pre-creating a Service with no owner")
			unownedService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
					// No ownerReferences
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{LabelKeyApp: "manual"},
					Ports: []corev1.ServicePort{
						{
							Name:     "http",
							Port:     9999,
							Protocol: corev1.ProtocolTCP,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, unownedService)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)
			if err == nil {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			}
		})

		It("should reject updating unowned service", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("has no controller owner"))

			By("Verifying the service was NOT updated")
			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(9999)))

			By("Verifying the MCPServer status shows service unavailable with ownership error")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("ServiceUnavailable"))
			Expect(readyCondition.Message).To(ContainSubstring("has no controller owner"))
		})
	})

	Context("When a Deployment has multiple owner references but no controller", func() {
		const resourceName = "test-multi-owner-deploy"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Creating MCPServer")
			resource := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Pre-creating a Deployment with multiple non-controller owners")
			multiOwnerDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "mcp.x-k8s.io/v1alpha1",
							Kind:       "MCPServer",
							Name:       "some-other-server",
							UID:        types.UID("other-mcpserver-uid"),
							Controller: new(false), // Not a controller
						},
						{
							APIVersion: "apps/v1",
							Kind:       "ReplicaSet",
							Name:       "some-replicaset",
							UID:        types.UID("replicaset-uid"),
							Controller: new(false), // Also not a controller
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: managedWorkloadSelector(resourceName),
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: managedWorkloadLabels(resourceName),
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  ManagedWorkloadName,
									Image: "manual-image:latest",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, multiOwnerDeployment)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should reject updating deployment with multiple non-controller owners", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("has no controller owner"))

			By("Verifying the deployment was NOT updated")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("manual-image:latest"))

			By("Verifying all original owner references are still present")
			Expect(deployment.OwnerReferences).To(HaveLen(2))
			Expect(deployment.OwnerReferences[0].Name).To(Equal("some-other-server"))
			Expect(*deployment.OwnerReferences[0].Controller).To(BeFalse())
			Expect(deployment.OwnerReferences[1].Name).To(Equal("some-replicaset"))
			Expect(*deployment.OwnerReferences[1].Controller).To(BeFalse())

			By("Verifying the MCPServer status shows deployment unavailable with ownership error")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("DeploymentUnavailable"))
			Expect(readyCondition.Message).To(ContainSubstring("has no controller owner"))
		})
	})

	Context("When a Service has multiple owner references but no controller", func() {
		const resourceName = "test-multi-owner-svc"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("Creating MCPServer first to create Deployment")
			mcpServer := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			By("Pre-creating a Service with multiple non-controller owners")
			multiOwnerService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "mcp.x-k8s.io/v1alpha1",
							Kind:       "MCPServer",
							Name:       "some-other-server",
							UID:        types.UID("other-mcpserver-uid"),
							Controller: new(false), // Not a controller
						},
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Name:       "some-config",
							UID:        types.UID("config-uid"),
							Controller: new(false), // Also not a controller
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{LabelKeyApp: "manual"},
					Ports: []corev1.ServicePort{
						{
							Name:     "http",
							Port:     9999,
							Protocol: corev1.ProtocolTCP,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, multiOwnerService)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)
			if err == nil {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			}
		})

		It("should reject updating service with multiple non-controller owners", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("has no controller owner"))

			By("Verifying the service was NOT updated")
			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(9999)))

			By("Verifying all original owner references are still present")
			Expect(service.OwnerReferences).To(HaveLen(2))
			Expect(service.OwnerReferences[0].Name).To(Equal("some-other-server"))
			Expect(*service.OwnerReferences[0].Controller).To(BeFalse())
			Expect(service.OwnerReferences[1].Name).To(Equal("some-config"))
			Expect(*service.OwnerReferences[1].Controller).To(BeFalse())

			By("Verifying the MCPServer status shows service unavailable with ownership error")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCondition.Reason).To(Equal("ServiceUnavailable"))
			Expect(readyCondition.Message).To(ContainSubstring("has no controller owner"))
		})
	})

	Context("When a Deployment is orphaned from a deleted MCPServer", func() {
		const resourceName = "test-orphaned-deploy"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		It("should adopt deployment when MCPServer is recreated with same name", func() {
			By("Creating first MCPServer")
			oldMCPServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/old-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, oldMCPServer)).To(Succeed())

			By("Reconciling to create deployment")
			reconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment was created with old MCPServer owner")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			oldUID := oldMCPServer.UID
			Expect(deployment.OwnerReferences[0].UID).To(Equal(oldUID))

			By("Deleting MCPServer but keeping deployment")
			// Re-fetch to get latest version after reconciliation
			Expect(k8sClient.Get(ctx, typeNamespacedName, oldMCPServer)).To(Succeed())
			// Remove finalizers if any
			oldMCPServer.Finalizers = nil
			Expect(k8sClient.Update(ctx, oldMCPServer)).To(Succeed())
			Expect(k8sClient.Delete(ctx, oldMCPServer)).To(Succeed())

			// Manually set owner reference to simulate orphaned deployment
			// (with stale UID from deleted MCPServer)
			deployment.OwnerReferences[0].UID = types.UID("old-deleted-uid")
			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

			By("Creating new MCPServer with same name")
			newMCPServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/new-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, newMCPServer)).To(Succeed())

			By("Reconciling new MCPServer")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment was adopted by new MCPServer")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].UID).To(Equal(newMCPServer.UID))
			Expect(deployment.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(deployment.OwnerReferences[0].Kind).To(Equal("MCPServer"))

			By("Verifying deployment spec was updated to new image")
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("docker.io/library/new-image:latest"))

			By("Verifying MCPServer status shows successful reconciliation")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Eventually(func() string {
				err := k8sClient.Get(ctx, typeNamespacedName, mcpServer)
				if err != nil {
					return ""
				}
				readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
				if readyCondition == nil {
					return ""
				}
				return readyCondition.Reason
			}).Should(Or(
				Equal("Initializing"),          // Initial state after creation
				Equal("DeploymentUnavailable"), // Deployment exists but not ready yet
				Equal("Available"),             // Fully ready
			), "MCPServer should be reconciling successfully after adopting orphaned deployment")

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, newMCPServer)).To(Succeed())
		})
	})

	Context("When a Service is orphaned from a deleted MCPServer", func() {
		const resourceName = "test-orphaned-svc"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		It("should adopt service when MCPServer is recreated with same name", func() {
			By("Creating first MCPServer")
			oldMCPServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/old-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, oldMCPServer)).To(Succeed())

			By("Reconciling to create service")
			reconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying service was created with old MCPServer owner")
			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.OwnerReferences).To(HaveLen(1))
			oldUID := oldMCPServer.UID
			Expect(service.OwnerReferences[0].UID).To(Equal(oldUID))

			By("Deleting MCPServer but keeping service")
			// Re-fetch to get latest version after reconciliation
			Expect(k8sClient.Get(ctx, typeNamespacedName, oldMCPServer)).To(Succeed())
			// Remove finalizers if any
			oldMCPServer.Finalizers = nil
			Expect(k8sClient.Update(ctx, oldMCPServer)).To(Succeed())
			Expect(k8sClient.Delete(ctx, oldMCPServer)).To(Succeed())

			// Manually set owner reference to simulate orphaned service
			// (with stale UID from deleted MCPServer)
			service.OwnerReferences[0].UID = types.UID("old-deleted-uid")
			Expect(k8sClient.Update(ctx, service)).To(Succeed())

			By("Creating new MCPServer with same name")
			newMCPServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: "docker.io/library/new-image:latest",
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 9090,
					},
				},
			}
			Expect(k8sClient.Create(ctx, newMCPServer)).To(Succeed())

			By("Reconciling new MCPServer")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying service was adopted by new MCPServer")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].UID).To(Equal(newMCPServer.UID))
			Expect(service.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(service.OwnerReferences[0].Kind).To(Equal("MCPServer"))

			By("Verifying service spec was updated to new port")
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(9090)))

			By("Verifying MCPServer status shows successful reconciliation")
			mcpServer := &mcpv1alpha1.MCPServer{}
			Eventually(func() string {
				err := k8sClient.Get(ctx, typeNamespacedName, mcpServer)
				if err != nil {
					return ""
				}
				readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
				if readyCondition == nil {
					return ""
				}
				return readyCondition.Reason
			}).Should(Or(
				Equal("Initializing"),          // Initial state after creation
				Equal("DeploymentUnavailable"), // Deployment exists but not ready yet
				Equal("Available"),             // Fully ready
			), "MCPServer should be reconciling successfully after adopting orphaned service")

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, newMCPServer)).To(Succeed())
		})
		It("should adopt service when MCPServer is recreated with same name and same port", func() {
			const resourceName = "test-orphaned-svc-same-port"
			typeNamespacedName := types.NamespacedName{
				Name:      resourceName,
				Namespace: "default",
			}
			samePort := int32(8080)

			By("Creating first MCPServer")
			oldMCPServer := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, oldMCPServer)).To(Succeed())

			By("Reconciling to create service")
			reconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying service was created with old MCPServer owner")
			service := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.OwnerReferences).To(HaveLen(1))
			oldUID := oldMCPServer.UID
			Expect(service.OwnerReferences[0].UID).To(Equal(oldUID))

			By("Deleting MCPServer but keeping service")
			Expect(k8sClient.Get(ctx, typeNamespacedName, oldMCPServer)).To(Succeed())
			oldMCPServer.Finalizers = nil
			Expect(k8sClient.Update(ctx, oldMCPServer)).To(Succeed())
			Expect(k8sClient.Delete(ctx, oldMCPServer)).To(Succeed())

			// Manually set owner reference to simulate orphaned service
			service.OwnerReferences[0].UID = types.UID("old-deleted-uid")
			Expect(k8sClient.Update(ctx, service)).To(Succeed())

			By("Creating new MCPServer with same name and SAME port")
			newMCPServer := newTestMCPServer(resourceName)
			Expect(k8sClient.Create(ctx, newMCPServer)).To(Succeed())

			By("Reconciling new MCPServer")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying service was adopted by new MCPServer despite same port")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, service)).To(Succeed())
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].UID).To(Equal(newMCPServer.UID), "Owner UID should be updated even when port is the same")
			Expect(service.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(service.OwnerReferences[0].Kind).To(Equal("MCPServer"))

			By("Verifying service port is still the same")
			Expect(service.Spec.Ports[0].Port).To(Equal(samePort))

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, newMCPServer)).To(Succeed())
		})
	})

	Context("When a Deployment is orphaned and MCPServer recreated with same image", func() {
		const resourceName = "test-orphaned-deploy-same-img"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		It("should adopt deployment when MCPServer is recreated with same image", func() {
			sameImage := "docker.io/library/same-image:v1.0"

			By("Creating first MCPServer")
			oldMCPServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: sameImage,
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, oldMCPServer)).To(Succeed())

			By("Reconciling to create deployment")
			reconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment was created with old MCPServer owner")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			oldUID := oldMCPServer.UID
			Expect(deployment.OwnerReferences[0].UID).To(Equal(oldUID))

			By("Deleting MCPServer but keeping deployment")
			Expect(k8sClient.Get(ctx, typeNamespacedName, oldMCPServer)).To(Succeed())
			oldMCPServer.Finalizers = nil
			Expect(k8sClient.Update(ctx, oldMCPServer)).To(Succeed())
			Expect(k8sClient.Delete(ctx, oldMCPServer)).To(Succeed())

			// Manually set owner reference to simulate orphaned deployment
			deployment.OwnerReferences[0].UID = types.UID("old-deleted-uid")
			Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

			By("Creating new MCPServer with same image")
			newMCPServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Source: mcpv1alpha1.Source{
						Type: mcpv1alpha1.SourceTypeContainerImage,
						ContainerImage: &mcpv1alpha1.ContainerImageSource{
							Ref: sameImage, // Same image as before
						},
					},
					Config: mcpv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(k8sClient.Create(ctx, newMCPServer)).To(Succeed())

			By("Reconciling new MCPServer")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deployment was adopted despite same image")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].UID).To(Equal(newMCPServer.UID), "Owner UID should be updated even when image is the same")
			Expect(deployment.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(deployment.OwnerReferences[0].Kind).To(Equal("MCPServer"))

			By("Verifying deployment image is still the same")
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(sameImage))

			By("Cleanup")
			Expect(k8sClient.Delete(ctx, newMCPServer)).To(Succeed())
		})
	})
})
