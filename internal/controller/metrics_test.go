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
		handshakeTotal.Reset()
		handshakeDuration.Reset()
		capabilityChangesTotal.Reset()
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

		reconciler := &MCPServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient}
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

		reconciler := &MCPServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient}
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

		reconciler := &MCPServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient}
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

		reconciler := &MCPServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		Expect(testutil.CollectAndCount(conditionInfo)).To(BeNumerically(">", 0))
		Expect(testutil.CollectAndCount(validationFailuresTotal)).To(BeNumerically(">", 0))

		// Delete the resource and reconcile again
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		Expect(testutil.CollectAndCount(conditionInfo)).To(Equal(0))
		Expect(testutil.CollectAndCount(validationFailuresTotal)).To(Equal(0))
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

		reconciler := &MCPServerReconciler{Client: interceptedClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient}
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: depFailNN})
		Expect(err).To(HaveOccurred())

		// Deployment failure counter incremented
		Expect(testutil.ToFloat64(deploymentFailuresTotal.WithLabelValues(
			depFailName, namespace, MetricReasonReconcileError,
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

		reconciler := &MCPServerReconciler{Client: interceptedClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient}
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: svcFailNN})
		Expect(err).To(HaveOccurred())

		// Service failure counter incremented
		Expect(testutil.ToFloat64(serviceFailuresTotal.WithLabelValues(
			svcFailName, namespace, MetricReasonReconcileError,
		))).To(Equal(1.0))

		// Ready=False condition recorded
		Expect(testutil.ToFloat64(conditionInfo.WithLabelValues(
			svcFailName, namespace, "Ready", "False", "ServiceUnavailable",
		))).To(Equal(1.0))
	})

	It("should record handshake success metrics when handshake succeeds", func() {
		hsSuccessName := "metrics-hs-ok"
		hsSuccessNN := types.NamespacedName{Name: hsSuccessName, Namespace: namespace}
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hsSuccessName,
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
		defer func() { _ = k8sClient.Delete(ctx, resource) }()

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
			MCPDialer: func(_ context.Context, _ string) (*mcpv1alpha1.MCPServerInfo, error) {
				return &mcpv1alpha1.MCPServerInfo{Name: "test-server", Version: "1.0"}, nil
			},
		}
		// First reconcile creates the Deployment
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: hsSuccessNN})
		Expect(err).NotTo(HaveOccurred())

		simulateDeploymentAvailable(ctx, hsSuccessNN)

		// Second reconcile triggers handshake
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: hsSuccessNN})
		Expect(err).NotTo(HaveOccurred())

		Expect(testutil.ToFloat64(handshakeTotal.WithLabelValues(
			hsSuccessName, namespace, "success",
		))).To(Equal(1.0))
		Expect(testutil.CollectAndCount(handshakeDuration)).To(BeNumerically(">", 0))
	})

	It("should record handshake failure metrics when handshake fails", func() {
		hsFailName := "metrics-hs-fail"
		hsFailNN := types.NamespacedName{Name: hsFailName, Namespace: namespace}
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hsFailName,
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
		defer func() { _ = k8sClient.Delete(ctx, resource) }()

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
			MCPDialer: func(_ context.Context, _ string) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("connection refused")
			},
		}
		// First reconcile creates the Deployment
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: hsFailNN})
		Expect(err).NotTo(HaveOccurred())

		simulateDeploymentAvailable(ctx, hsFailNN)

		// Second reconcile triggers handshake
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: hsFailNN})
		Expect(err).NotTo(HaveOccurred())

		Expect(testutil.ToFloat64(handshakeTotal.WithLabelValues(
			hsFailName, namespace, "failure",
		))).To(Equal(1.0))
		Expect(testutil.CollectAndCount(handshakeDuration)).To(BeNumerically(">", 0))
	})

	It("should record auth_skip metrics when handshake returns HTTP auth error", func() {
		hsAuthName := "metrics-hs-auth"
		hsAuthNN := types.NamespacedName{Name: hsAuthName, Namespace: namespace}
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hsAuthName,
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
		defer func() { _ = k8sClient.Delete(ctx, resource) }()

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
			MCPDialer: func(_ context.Context, _ string) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("POST http://localhost:8080/mcp: Unauthorized")
			},
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: hsAuthNN})
		Expect(err).NotTo(HaveOccurred())

		simulateDeploymentAvailable(ctx, hsAuthNN)

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: hsAuthNN})
		Expect(err).NotTo(HaveOccurred())

		Expect(testutil.ToFloat64(handshakeTotal.WithLabelValues(
			hsAuthName, namespace, "auth_skip",
		))).To(Equal(1.0))
		Expect(testutil.CollectAndCount(handshakeDuration)).To(BeNumerically(">", 0))
	})

	It("should increment capabilityChangesTotal when capabilities change between reconciles", func() {
		capChangeName := "metrics-cap-change"
		capChangeNN := types.NamespacedName{Name: capChangeName, Namespace: namespace}
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      capChangeName,
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
		defer func() { _ = k8sClient.Delete(ctx, resource) }()

		firstCaps := &mcpv1alpha1.MCPServerCapabilities{Tools: true, Resources: false}
		secondCaps := &mcpv1alpha1.MCPServerCapabilities{Tools: true, Resources: true}

		currentCaps := firstCaps
		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
			MCPDialer: func(_ context.Context, _ string) (*mcpv1alpha1.MCPServerInfo, error) {
				return &mcpv1alpha1.MCPServerInfo{
					Name:         "cap-server",
					Version:      "1.0",
					Capabilities: currentCaps.DeepCopy(),
				}, nil
			},
		}

		// First reconcile creates the Deployment
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: capChangeNN})
		Expect(err).NotTo(HaveOccurred())

		simulateDeploymentAvailable(ctx, capChangeNN)

		// Second reconcile triggers handshake and sets initial capabilities
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: capChangeNN})
		Expect(err).NotTo(HaveOccurred())

		// Change capabilities for next handshake
		currentCaps = secondCaps

		// Bump generation by changing a spec field so the handshake re-runs
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, capChangeNN, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Path = "/mcp-v2"
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		// Third reconcile detects the capability change
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: capChangeNN})
		Expect(err).NotTo(HaveOccurred())

		Expect(testutil.ToFloat64(capabilityChangesTotal.WithLabelValues(
			capChangeName, namespace,
		))).To(Equal(1.0))
	})

	It("should record skip metrics when handshake was already verified", func() {
		hsSkipName := "metrics-hs-skip"
		hsSkipNN := types.NamespacedName{Name: hsSkipName, Namespace: namespace}
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hsSkipName,
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
		defer func() { _ = k8sClient.Delete(ctx, resource) }()

		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
			MCPDialer: func(_ context.Context, _ string) (*mcpv1alpha1.MCPServerInfo, error) {
				return &mcpv1alpha1.MCPServerInfo{Name: "test-server", Version: "1.0"}, nil
			},
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: hsSkipNN})
		Expect(err).NotTo(HaveOccurred())

		simulateDeploymentAvailable(ctx, hsSkipNN)

		// Second reconcile: handshake succeeds
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: hsSkipNN})
		Expect(err).NotTo(HaveOccurred())
		Expect(testutil.ToFloat64(handshakeTotal.WithLabelValues(
			hsSkipName, namespace, "success",
		))).To(Equal(1.0))

		// Third reconcile: already verified, hits the skip path
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: hsSkipNN})
		Expect(err).NotTo(HaveOccurred())
		Expect(testutil.ToFloat64(handshakeTotal.WithLabelValues(
			hsSkipName, namespace, "skip",
		))).To(Equal(1.0))
	})

	It("should detect capability change when capabilities go from non-nil to nil", func() {
		capNilName := "metrics-cap-nil"
		capNilNN := types.NamespacedName{Name: capNilName, Namespace: namespace}
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      capNilName,
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
		defer func() { _ = k8sClient.Delete(ctx, resource) }()

		firstCaps := &mcpv1alpha1.MCPServerCapabilities{Tools: true, Resources: true}
		returnCaps := firstCaps
		reconciler := &MCPServerReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
			MCPDialer: func(_ context.Context, _ string) (*mcpv1alpha1.MCPServerInfo, error) {
				info := &mcpv1alpha1.MCPServerInfo{
					Name:    "cap-nil-server",
					Version: "1.0",
				}
				if returnCaps != nil {
					info.Capabilities = returnCaps.DeepCopy()
				}
				return info, nil
			},
		}

		// First reconcile creates the Deployment
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: capNilNN})
		Expect(err).NotTo(HaveOccurred())

		simulateDeploymentAvailable(ctx, capNilNN)

		// Second reconcile triggers handshake and sets initial capabilities
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: capNilNN})
		Expect(err).NotTo(HaveOccurred())

		// Server now returns nil capabilities
		returnCaps = nil

		// Bump generation so the handshake re-runs
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, capNilNN, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Path = "/mcp-v2"
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		// Third reconcile detects the capability change (non-nil to nil)
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: capNilNN})
		Expect(err).NotTo(HaveOccurred())

		Expect(testutil.ToFloat64(capabilityChangesTotal.WithLabelValues(
			capNilName, namespace,
		))).To(Equal(1.0))
	})
})

func simulateDeploymentAvailable(ctx context.Context, nn types.NamespacedName) {
	dep := &appsv1.Deployment{}
	EventuallyWithOffset(1, func() error {
		return k8sClient.Get(ctx, nn, dep)
	}).Should(Succeed())

	dep.Status.Replicas = 1
	dep.Status.ReadyReplicas = 1
	dep.Status.AvailableReplicas = 1
	dep.Status.Conditions = []appsv1.DeploymentCondition{
		{
			Type:   appsv1.DeploymentAvailable,
			Status: corev1.ConditionTrue,
		},
	}
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, dep)).To(Succeed())
}
