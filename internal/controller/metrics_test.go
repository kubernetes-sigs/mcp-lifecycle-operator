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
	"github.com/prometheus/client_golang/prometheus/testutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Metrics", func() {
	const resourceName = "metrics-test"
	const namespace = "default"

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: namespace,
	}

	AfterEach(func() {
		resource := &mcpv1alpha1.MCPServer{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		if err == nil {
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		}
		// Reset metrics between tests
		conditionInfo.Reset()
		validationFailuresTotal.Reset()
		deploymentFailuresTotal.Reset()
		serviceFailuresTotal.Reset()
		reconcileDuration.Reset()
	})

	It("should record Accepted and Ready condition metrics on successful reconcile", func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{Port: 8080},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconciler := &MCPServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		// Accepted=True should be recorded
		Expect(testutil.ToFloat64(conditionInfo.WithLabelValues(
			resourceName, namespace, "Accepted", "True", "Valid",
		))).To(Equal(1.0))

		// Ready condition should be recorded (at least Accepted + Ready)
		count := testutil.CollectAndCount(conditionInfo)
		Expect(count).To(BeNumerically(">=", 2))
	})

	It("should record validation failure metrics when config is invalid", func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
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
					Storage: []mcpv1alpha1.StorageMount{
						{
							Path: "/config",
							Source: mcpv1alpha1.StorageSource{
								Type: mcpv1alpha1.StorageTypeConfigMap,
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "nonexistent-configmap",
									},
								},
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconciler := &MCPServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		// Accepted=False and Ready=False should both be recorded
		Expect(testutil.ToFloat64(conditionInfo.WithLabelValues(
			resourceName, namespace, "Accepted", "False", "Invalid",
		))).To(Equal(1.0))
		Expect(testutil.ToFloat64(conditionInfo.WithLabelValues(
			resourceName, namespace, "Ready", "False", "ConfigurationInvalid",
		))).To(Equal(1.0))

		// Validation failure counter incremented
		Expect(testutil.ToFloat64(validationFailuresTotal.WithLabelValues(
			resourceName, namespace, "Invalid",
		))).To(Equal(1.0))
	})

	It("should record reconcile phase durations", func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{Port: 8080},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconciler := &MCPServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		// Verify the metric has observations
		count := testutil.CollectAndCount(reconcileDuration)
		Expect(count).To(BeNumerically(">", 0))
	})

	It("should cleanup metrics when MCPServer is deleted", func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{Port: 8080},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())

		reconciler := &MCPServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		Expect(testutil.CollectAndCount(conditionInfo)).To(BeNumerically(">", 0))

		// Delete the resource and reconcile again
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		Expect(testutil.CollectAndCount(conditionInfo)).To(Equal(0))
	})

	It("should record deployment failure metrics and Ready condition when deployment reconciliation fails", func() {
		depFailName := "metrics-dep-fail"
		depFailNN := types.NamespacedName{Name: depFailName, Namespace: namespace}
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      depFailName,
				Namespace: namespace,
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{Port: 8080},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return fmt.Errorf("simulated deployment failure")
				}
				return c.Create(ctx, obj, opts...)
			},
		})

		reconciler := &MCPServerReconciler{Client: interceptedClient, Scheme: k8sClient.Scheme()}
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: depFailNN})
		Expect(err).To(HaveOccurred())

		// Deployment failure counter incremented
		Expect(testutil.ToFloat64(deploymentFailuresTotal.WithLabelValues(
			depFailName, namespace, "ReconcileError",
		))).To(Equal(1.0))

		// Ready=False condition recorded
		Expect(testutil.ToFloat64(conditionInfo.WithLabelValues(
			depFailName, namespace, "Ready", "False", "DeploymentUnavailable",
		))).To(Equal(1.0))
	})

	It("should record service failure metrics and Ready condition when service reconciliation fails", func() {
		svcFailName := "metrics-svc-fail"
		svcFailNN := types.NamespacedName{Name: svcFailName, Namespace: namespace}
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcFailName,
				Namespace: namespace,
			},
			Spec: mcpv1alpha1.MCPServerSpec{
				Source: mcpv1alpha1.Source{
					Type: mcpv1alpha1.SourceTypeContainerImage,
					ContainerImage: &mcpv1alpha1.ContainerImageSource{
						Ref: "docker.io/library/test-image:latest",
					},
				},
				Config: mcpv1alpha1.ServerConfig{Port: 8080},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*corev1.Service); ok {
					return fmt.Errorf("simulated service failure")
				}
				return c.Create(ctx, obj, opts...)
			},
		})

		reconciler := &MCPServerReconciler{Client: interceptedClient, Scheme: k8sClient.Scheme()}
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: svcFailNN})
		Expect(err).To(HaveOccurred())

		// Service failure counter incremented
		Expect(testutil.ToFloat64(serviceFailuresTotal.WithLabelValues(
			svcFailName, namespace, "ReconcileError",
		))).To(Equal(1.0))

		// Ready=False condition recorded
		Expect(testutil.ToFloat64(conditionInfo.WithLabelValues(
			svcFailName, namespace, "Ready", "False", "ServiceUnavailable",
		))).To(Equal(1.0))
	})
})
