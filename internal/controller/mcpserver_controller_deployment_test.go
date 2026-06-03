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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Controller - reconcileDeployment", func() {
	const resourceName = "test-reconcile-deployment"

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
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})

	It("should create a deployment when none exists", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		deployment, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())
		Expect(deployment.Name).To(Equal(resourceName))
	})

	It("should return existing deployment without error on second call", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		deployment, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())
	})

	// Defensive guard: createDeployment always produces a container today, so
	// this scenario cannot be triggered through reconcileDeployment. We test
	// deploymentNeedsUpdate directly to ensure the len(newPodSpec.Containers)==0
	// guard prevents an index-out-of-bounds panic if a future refactor (e.g. a
	// new source type) produces a desired deployment with no containers.
	It("should return false from deploymentNeedsUpdate when desired deployment has empty containers list", func() {
		By("Setting up a valid existing deployment and an empty desired deployment")
		mcpServer := newTestMCPServer("test-empty-desired")

		existingDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  ManagedWorkloadName,
								Image: "docker.io/library/test-image:latest",
							},
						},
					},
				},
			},
		}

		desiredDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: nil,
					},
				},
			},
		}

		By("Calling deploymentNeedsUpdate should not panic and should return false")
		Expect(deploymentNeedsUpdate(mcpServer, existingDeployment, desiredDeployment, false)).To(BeFalse())
	})

	It("should detect drift when base managed labels are removed from template", func() {
		mcpServer := newTestMCPServer("test-label-drift")
		labels := managedWorkloadLabels("test-label-drift")

		existing := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"leftover": "true"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: ManagedWorkloadName, Image: "docker.io/library/test-image:latest"},
						},
					},
				},
			},
		}

		desired := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: ManagedWorkloadName, Image: "docker.io/library/test-image:latest"},
						},
					},
				},
			},
		}

		Expect(deploymentNeedsUpdate(mcpServer, existing, desired, false)).To(BeTrue())
	})

	It("should not detect drift when base managed labels match", func() {
		mcpServer := newTestMCPServer("test-label-match")
		labels := managedWorkloadLabels("test-label-match")

		existing := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: ManagedWorkloadName, Image: "docker.io/library/test-image:latest"},
						},
					},
				},
			},
		}

		desired := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: ManagedWorkloadName, Image: "docker.io/library/test-image:latest"},
						},
					},
				},
			},
		}

		Expect(deploymentNeedsUpdate(mcpServer, existing, desired, false)).To(BeFalse())
	})

	It("should recover when existing deployment has empty containers list", func() {
		By("Setting up a fake client with a deployment that has no containers")
		mcpServer := newTestMCPServer("test-empty-containers")
		mcpServer.UID = "fake-uid"

		brokenDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-empty-containers",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "mcp.x-k8s.io/v1alpha1",
						Kind:       "MCPServer",
						Name:       "test-empty-containers",
						UID:        "fake-uid",
						Controller: new(true),
					},
				},
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: managedWorkloadSelector("test-empty-containers"),
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: managedWorkloadLabels("test-empty-containers"),
					},
					Spec: corev1.PodSpec{
						Containers: nil,
					},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(k8sClient.Scheme()).
			WithObjects(mcpServer, brokenDeployment).
			Build()

		reconciler := &MCPServerReconciler{
			Client:    fakeClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling should not panic and should restore the containers")
		deployment, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("docker.io/library/test-image:latest"))
	})
})

var _ = Describe("MCPServer Controller - Deployment Reconciliation Failures", func() {
	const resourceName = "test-deployment-failure"

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
	})

	It("should update status with DeploymentUnavailable when deployment creation fails", func() {
		By("Creating interceptor that returns error on Deployment Create")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return fmt.Errorf("simulated deployment creation failure")
				}
				return c.Create(ctx, obj, opts...)
			},
		})

		deploymentFailureReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling with deployment creation failure")
		_, err = deploymentFailureReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated deployment creation failure"))

		By("Verifying status is updated with DeploymentUnavailable")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(acceptedCondition.Reason).To(Equal("Valid"))

		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(readyCondition.Message).To(ContainSubstring("Failed to reconcile Deployment"))
		Expect(readyCondition.Message).To(ContainSubstring("simulated deployment creation failure"))
	})

	It("should update status with DeploymentUnavailable when deployment update fails", func() {
		By("Initial reconcile to create resources")
		initialReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}
		_, err := initialReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying deployment was created")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)).To(Succeed())

		By("Creating interceptor that returns error on Deployment Update")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return fmt.Errorf("simulated deployment update failure")
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		deploymentFailureReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Updating MCPServer spec to trigger deployment reconciliation")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Env = []corev1.EnvVar{{Name: "TEST_VAR", Value: "test_value"}}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling with deployment update failure")
		_, err = deploymentFailureReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated deployment update failure"))

		By("Verifying status is updated with DeploymentUnavailable")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(acceptedCondition.Reason).To(Equal("Valid"))

		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(readyCondition.Message).To(ContainSubstring("Failed to reconcile Deployment"))
		Expect(readyCondition.Message).To(ContainSubstring("simulated deployment update failure"))
	})
})

var _ = Describe("MCPServer Controller - Transient Validation Errors", func() {
	const resourceName = "test-transient-validation"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := newTestMCPServer(resourceName)
		resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
			{
				Path: "/data",
				Source: mcpv1alpha1.StorageSource{
					Type: mcpv1alpha1.StorageTypeConfigMap,
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-config",
						},
					},
				},
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

	It("should return error and not update status on transient ConfigMap validation failure", func() {
		By("Creating interceptor that returns 500 on ConfigMap Get")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.ConfigMap); ok && key.Name == "test-config" {
					return &errors.StatusError{
						ErrStatus: metav1.Status{
							Status:  metav1.StatusFailure,
							Code:    500,
							Reason:  metav1.StatusReasonInternalError,
							Message: "simulated API server error",
						},
					}
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})

		transientReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling with transient ConfigMap validation failure")
		_, err = transientReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("transient error validating ConfigMap"))

		By("Verifying status conditions are NOT updated")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

		// Status should have no conditions set - the transient path preserves
		// existing status and does not write new conditions
		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).To(BeNil())

		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).To(BeNil())
	})

	It("should preserve existing status conditions on transient error after prior successful reconcile", func() {
		By("First reconcile succeeds with ConfigMap present")
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: "default",
			},
			Data: map[string]string{"key": "value"},
		}
		Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

		initialReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}
		_, err := initialReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying Accepted=True was set")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))

		By("Creating interceptor that returns 500 on ConfigMap Get for subsequent reconcile")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.ConfigMap); ok && key.Name == "test-config" {
					return &errors.StatusError{
						ErrStatus: metav1.Status{
							Status:  metav1.StatusFailure,
							Code:    500,
							Reason:  metav1.StatusReasonInternalError,
							Message: "simulated API server error",
						},
					}
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})

		transientReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling with transient failure")
		_, err = transientReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("transient error validating ConfigMap"))

		By("Verifying previous Accepted=True condition is preserved")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(acceptedCondition.Reason).To(Equal("Valid"))

		By("Cleaning up ConfigMap")
		Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
	})
})

var _ = Describe("MCPServer Controller - Resource Requirements", func() {
	const resourceName = "test-resource-resources"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		mcpServer := newTestMCPServer(resourceName)
		mcpServer.Spec.Runtime.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
	})

	AfterEach(func() {
		res := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, res)
		if err == nil {
			Expect(k8sClient.Delete(ctx, res)).To(Succeed())
		}
	})

	It("should create deployment with resources", func() {
		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("256Mi")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("500m")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("512Mi")))
		// Verify replicas defaults to 1 even when runtime is specified with other fields
		Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
	})

	It("should update deployment when resources change", func() {
		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling to create the initial deployment with resources")
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))

		By("Updating resources")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Runtime.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling again to pick up the change")
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("200m")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("512Mi")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("1")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("1Gi")))
	})

	It("should update deployment when resources are removed", func() {
		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling to create the initial deployment with resources")
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))

		By("Removing resources")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Runtime.Resources = nil
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling again to pick up the change")
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(BeEmpty())
	})

	It("should handle resources with only requests (no limits)", func() {
		mcpServer := newTestMCPServer("test-only-requests")
		mcpServer.Spec.Runtime.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-requests", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-only-requests", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-only-requests",
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("128Mi")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(BeEmpty())
	})

	It("should handle resources with only limits (no requests)", func() {
		mcpServer := newTestMCPServer("test-only-limits")
		mcpServer.Spec.Runtime.Resources = &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-limits", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-only-limits", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-only-limits",
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("500m")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("512Mi")))
	})

	It("should handle resources with only CPU (no memory)", func() {
		mcpServer := newTestMCPServer("test-only-cpu")
		mcpServer.Spec.Runtime.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("100m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("200m"),
			},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-cpu", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-only-cpu", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-only-cpu",
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("100m")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).NotTo(HaveKey(corev1.ResourceMemory))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceCPU, resource.MustParse("200m")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).NotTo(HaveKey(corev1.ResourceMemory))
	})

	It("should handle resources with only memory (no CPU)", func() {
		mcpServer := newTestMCPServer("test-only-memory")
		mcpServer.Spec.Runtime.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-memory", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-only-memory", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-only-memory",
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("256Mi")))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(corev1.ResourceMemory, resource.MustParse("512Mi")))
	})
})

var _ = Describe("MCPServer Controller - Health Probes", func() {
	const resourceName = "test-resource-probes"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		mcpServer := newTestMCPServer(resourceName)
		mcpServer.Spec.Runtime.Health = mcpv1alpha1.HealthConfig{
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/health",
						Port: intstr.FromInt(8080),
					},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       30,
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(8080),
					},
				},
				InitialDelaySeconds: 5,
				PeriodSeconds:       10,
			},
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
	})

	AfterEach(func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, mcpServer)
		if err == nil {
			Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
		}
	})

	It("should create deployment with health probes", func() {
		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))

		// Verify liveness probe
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Path).To(Equal("/health"))
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Port.IntVal).To(Equal(int32(8080)))
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds).To(Equal(int32(10)))
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds).To(Equal(int32(30)))

		// Verify readiness probe
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port.IntVal).To(Equal(int32(8080)))
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds).To(Equal(int32(5)))
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.PeriodSeconds).To(Equal(int32(10)))
	})

	It("should update deployment when probes change", func() {
		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling to create the initial deployment with probes")
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds).To(Equal(int32(10)))

		By("Updating probes")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Runtime.Health.LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 15,
			PeriodSeconds:       60,
		}
		mcpServer.Spec.Runtime.Health.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/ready",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 3,
			PeriodSeconds:       5,
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling again to pick up the change")
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())

		// Verify updated liveness probe
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Path).To(Equal("/healthz"))
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds).To(Equal(int32(15)))
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds).To(Equal(int32(60)))

		// Verify updated readiness probe
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Path).To(Equal("/ready"))
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds).To(Equal(int32(3)))
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.PeriodSeconds).To(Equal(int32(5)))
	})

	It("should update deployment when probes are removed", func() {
		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling to create the initial deployment with probes")
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).NotTo(BeNil())

		By("Removing probes")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Runtime.Health.LivenessProbe = nil
		mcpServer.Spec.Runtime.Health.ReadinessProbe = nil
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling again to pick up the change")
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).To(BeNil())
		// With no user-specified readiness probe, the default TCP socket readiness probe is injected
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port.IntVal).To(Equal(int32(8080)))
	})

	It("should handle only liveness probe (no readiness)", func() {
		mcpServer := newTestMCPServer("test-only-liveness")
		mcpServer.Spec.Runtime.Health.LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 10,
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-liveness", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-only-liveness", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-only-liveness",
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet).NotTo(BeNil())
		// With no user-specified readiness probe, the default TCP socket readiness probe is injected
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port.IntVal).To(Equal(int32(8080)))
	})

	It("should handle only readiness probe (no liveness)", func() {
		mcpServer := newTestMCPServer("test-only-readiness")
		mcpServer.Spec.Runtime.Health.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 5,
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-only-readiness", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-only-readiness", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-only-readiness",
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).To(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket).NotTo(BeNil())
	})

	It("should inject default MCP readiness probe when no health config is specified", func() {
		mcpServer := newTestMCPServer("test-default-probe")
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-default-probe", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-default-probe", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-default-probe",
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())

		// Default readiness probe should be a TCP socket probe on port 8080
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port.IntVal).To(Equal(int32(8080)))
		// No liveness probe should be set
		Expect(deployment.Spec.Template.Spec.Containers[0].LivenessProbe).To(BeNil())
	})

	It("should use custom port in default readiness probe", func() {
		mcpServer := newTestMCPServer("test-default-probe-custom-port")
		mcpServer.Spec.Config.Port = 9090
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-default-probe-custom-port", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-default-probe-custom-port", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-default-probe-custom-port",
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())

		// Default readiness probe should use the custom port
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port.IntVal).To(Equal(int32(9090)))
	})

	It("should create TCP socket probe with given port using Kubernetes default timing", func() {
		probe := defaultMCPReadinessProbe(8080)
		Expect(probe).NotTo(BeNil())
		Expect(probe.TCPSocket).NotTo(BeNil())
		Expect(probe.TCPSocket.Port.IntVal).To(Equal(int32(8080)))
		Expect(probe.InitialDelaySeconds).To(BeZero())
		Expect(probe.PeriodSeconds).NotTo(BeZero())
		Expect(probe.TimeoutSeconds).NotTo(BeZero())
		Expect(probe.SuccessThreshold).NotTo(BeZero())
		Expect(probe.FailureThreshold).NotTo(BeZero())
	})

	It("should fill zero-valued timing fields on user-provided probes", func() {
		probe := withProbeDefaults(&corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt(8080)},
			},
		})
		Expect(probe.FailureThreshold).NotTo(BeZero())
		Expect(probe.PeriodSeconds).NotTo(BeZero())
		Expect(probe.SuccessThreshold).NotTo(BeZero())
		Expect(probe.TimeoutSeconds).NotTo(BeZero())
		Expect(probe.HTTPGet.Path).To(Equal("/healthz"))
	})

	It("should preserve user-set timing fields on probes", func() {
		probe := withProbeDefaults(&corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(8080)},
			},
			PeriodSeconds:       30,
			FailureThreshold:    5,
			InitialDelaySeconds: 10,
		})
		Expect(probe.PeriodSeconds).To(Equal(int32(30)))
		Expect(probe.FailureThreshold).To(Equal(int32(5)))
		Expect(probe.InitialDelaySeconds).To(Equal(int32(10)))
		Expect(probe.SuccessThreshold).NotTo(BeZero())
		Expect(probe.TimeoutSeconds).NotTo(BeZero())
	})

	It("should use provided port in default readiness probe", func() {
		probe := defaultMCPReadinessProbe(9090)
		Expect(probe).NotTo(BeNil())
		Expect(probe.TCPSocket).NotTo(BeNil())
		Expect(probe.TCPSocket.Port.IntVal).To(Equal(int32(9090)))
	})

	It("should not inject default readiness probe when custom readiness probe is specified", func() {
		mcpServer := newTestMCPServer("test-custom-overrides-default")
		mcpServer.Spec.Runtime.Health.ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(3000),
				},
			},
			InitialDelaySeconds: 15,
			PeriodSeconds:       20,
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-custom-overrides-default", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		controllerReconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "test-custom-overrides-default", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-custom-overrides-default",
			Namespace: "default",
		}, deployment)
		Expect(err).NotTo(HaveOccurred())

		// Custom readiness probe should be used, not the default
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket).NotTo(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.TCPSocket.Port.IntVal).To(Equal(int32(3000)))
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds).To(Equal(int32(15)))
		Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.PeriodSeconds).To(Equal(int32(20)))
	})

	It("should apply ExtraLabels and ExtraAnnotations on initial Deployment creation", func() {
		mcpServer := newTestMCPServer("test-extra-metadata-create")
		mcpServer.Spec.ExtraLabels = map[string]string{
			"team": "platform",
			"env":  "staging",
		}
		mcpServer.Spec.ExtraAnnotations = map[string]string{
			"example.com/owner": "team-a",
		}
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "test-extra-metadata-create", Namespace: "default"}, mcpServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		}()

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		deployment, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())

		createdDeployment := &appsv1.Deployment{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      "test-extra-metadata-create",
			Namespace: "default",
		}, createdDeployment)
		Expect(err).NotTo(HaveOccurred())

		By("Verifying ExtraLabels on Deployment metadata")
		Expect(createdDeployment.Labels).To(HaveKeyWithValue("team", "platform"))
		Expect(createdDeployment.Labels).To(HaveKeyWithValue("env", "staging"))

		By("Verifying ExtraLabels on PodTemplate metadata")
		Expect(createdDeployment.Spec.Template.Labels).To(HaveKeyWithValue("team", "platform"))
		Expect(createdDeployment.Spec.Template.Labels).To(HaveKeyWithValue("env", "staging"))

		By("Verifying ExtraAnnotations on Deployment metadata")
		Expect(createdDeployment.Annotations).To(HaveKeyWithValue("example.com/owner", "team-a"))

		By("Verifying ExtraAnnotations on PodTemplate metadata")
		Expect(createdDeployment.Spec.Template.Annotations).To(HaveKeyWithValue("example.com/owner", "team-a"))

		By("Verifying tracking annotations are set")
		Expect(createdDeployment.Annotations).To(HaveKey(managedExtraLabels))
		Expect(createdDeployment.Annotations).To(HaveKey(managedExtraAnnotations))
	})

	It("should default PodSecurityContext to empty when not specified", func() {
		mcpServer := newTestMCPServer("test-pod-sc-default")
		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		deployment, err := reconciler.createDeployment(mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.SecurityContext).NotTo(BeNil())
		Expect(*deployment.Spec.Template.Spec.SecurityContext).To(Equal(corev1.PodSecurityContext{}))
	})

	It("should use provided PodSecurityContext when specified", func() {
		mcpServer := newTestMCPServer("test-pod-sc-custom")
		mcpServer.Spec.Runtime.Security.PodSecurityContext = &corev1.PodSecurityContext{
			RunAsNonRoot: new(true),
		}
		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		deployment, err := reconciler.createDeployment(mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.SecurityContext).NotTo(BeNil())
		Expect(*deployment.Spec.Template.Spec.SecurityContext.RunAsNonRoot).To(BeTrue())
	})
})

var _ = Describe("MCPServer Controller - Deployment Reconcile Events", func() {
	const resourceName = "test-deployment-events"

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
	})

	It("should emit a Warning DeploymentReconcileFailed event only when deployment error message changes", func() {
		failMsg := "simulated deployment creation failure"
		reconciler, fr := newReconcilerForTestWithFakeEvents(k8sClient, k8sClient.Scheme())

		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return fmt.Errorf("%s", failMsg)
				}
				return c.Create(ctx, obj, opts...)
			},
		})
		reconciler.Client = interceptedClient

		By("First deployment reconcile failure — Warning event emitted once")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())

		var deploymentFailedEvent string
		Eventually(func(g Gomega) {
			for _, ev := range drainEvents(fr.Events) {
				if strings.Contains(ev, corev1.EventTypeWarning) && strings.Contains(ev, ReasonDeploymentUnavailable) {
					deploymentFailedEvent = ev
					break
				}
			}
			g.Expect(deploymentFailedEvent).NotTo(BeEmpty())
			g.Expect(deploymentFailedEvent).To(ContainSubstring(resourceName))
			g.Expect(deploymentFailedEvent).To(ContainSubstring("Failed to reconcile Deployment"))
			g.Expect(deploymentFailedEvent).To(ContainSubstring(failMsg))
		}).Should(Succeed())

		By("Second reconcile with same error — no duplicate deployment failed event")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())
		Consistently(fr.Events, 300*time.Millisecond, 20*time.Millisecond).ShouldNot(Receive())

		By("Change error message — second Warning event emitted")
		failMsg = "simulated deployment ownership failure"
		interceptedClient = interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return fmt.Errorf("%s", failMsg)
				}
				return c.Create(ctx, obj, opts...)
			},
		})
		reconciler.Client = interceptedClient

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).To(HaveOccurred())

		var secondDeploymentFailedEvent string
		Eventually(func(g Gomega) {
			for _, ev := range drainEvents(fr.Events) {
				if strings.Contains(ev, corev1.EventTypeWarning) && strings.Contains(ev, ReasonDeploymentUnavailable) {
					secondDeploymentFailedEvent = ev
					break
				}
			}
			g.Expect(secondDeploymentFailedEvent).NotTo(BeEmpty())
			g.Expect(secondDeploymentFailedEvent).To(ContainSubstring(resourceName))
			g.Expect(secondDeploymentFailedEvent).To(ContainSubstring(failMsg))
			g.Expect(secondDeploymentFailedEvent).NotTo(Equal(deploymentFailedEvent))
		}).Should(Succeed())
	})
})
