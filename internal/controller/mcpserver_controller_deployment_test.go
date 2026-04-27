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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
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
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		deployment, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())
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
						Controller: ptr.To(true),
					},
				},
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"mcp-server": "test-empty-containers"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":        "mcp-server",
							"mcp-server": "test-empty-containers",
						},
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
			Client: fakeClient,
			Scheme: k8sClient.Scheme(),
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
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
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
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
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
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
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
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
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
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
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
			Client: interceptedClient,
			Scheme: k8sClient.Scheme(),
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
