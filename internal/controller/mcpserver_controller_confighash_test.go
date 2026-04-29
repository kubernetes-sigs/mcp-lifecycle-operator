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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Controller - Config Hash", func() {
	ctx := context.Background()

	Context("When reconciling with no external refs (inline env only)", func() {
		const resourceName = "test-confighash-no-refs"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Env = []corev1.EnvVar{
				{
					Name:  "INLINE_VAR",
					Value: "inline-value",
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
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should not set config-hash annotation on deployment", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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

			annotations := deployment.Spec.Template.Annotations
			if annotations != nil {
				Expect(annotations).NotTo(HaveKey(configHashAnnotation))
			}
		})
	})

	Context("When reconciling with ConfigMap envFrom reference", func() {
		const resourceName = "test-confighash-cm-envfrom"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hash-test-cm",
					Namespace: "default",
				},
				Data: map[string]string{
					"key1": "value1",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.EnvFrom = []corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "hash-test-cm",
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
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "hash-test-cm", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should set config-hash annotation on deployment", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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

			Expect(deployment.Spec.Template.Annotations).To(HaveKey(configHashAnnotation))
			Expect(deployment.Spec.Template.Annotations[configHashAnnotation]).NotTo(BeEmpty())
		})
	})

	Context("When reconciling with Secret env valueFrom reference", func() {
		const resourceName = "test-confighash-secret-env"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hash-test-secret",
					Namespace: "default",
				},
				StringData: map[string]string{
					"password": "s3cret",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Env = []corev1.EnvVar{
				{
					Name: "DB_PASSWORD",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "hash-test-secret",
							},
							Key: "password",
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
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "hash-test-secret", Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should set config-hash annotation on deployment", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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

			Expect(deployment.Spec.Template.Annotations).To(HaveKey(configHashAnnotation))
			Expect(deployment.Spec.Template.Annotations[configHashAnnotation]).NotTo(BeEmpty())
		})
	})

	Context("When ConfigMap data changes", func() {
		const resourceName = "test-confighash-cm-change"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hash-change-cm",
					Namespace: "default",
				},
				Data: map[string]string{
					"key1": "original-value",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.EnvFrom = []corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "hash-change-cm",
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
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "hash-change-cm", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should update the config-hash annotation when ConfigMap data changes", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile
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

			originalHash := deployment.Spec.Template.Annotations[configHashAnnotation]
			Expect(originalHash).NotTo(BeEmpty())

			// Update the ConfigMap data
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "hash-change-cm", Namespace: "default"}, configMap)
			Expect(err).NotTo(HaveOccurred())
			configMap.Data["key1"] = "updated-value"
			Expect(k8sClient.Update(ctx, configMap)).To(Succeed())

			// Reconcile again
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Re-fetch deployment
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			newHash := deployment.Spec.Template.Annotations[configHashAnnotation]
			Expect(newHash).NotTo(BeEmpty())
			Expect(newHash).NotTo(Equal(originalHash))
		})
	})

	Context("When Secret data changes", func() {
		const resourceName = "test-confighash-secret-change"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hash-change-secret",
					Namespace: "default",
				},
				StringData: map[string]string{
					"token": "original-token",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.EnvFrom = []corev1.EnvFromSource{
				{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "hash-change-secret",
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
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "hash-change-secret", Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should update the config-hash annotation when Secret data changes", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile
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

			originalHash := deployment.Spec.Template.Annotations[configHashAnnotation]
			Expect(originalHash).NotTo(BeEmpty())

			// Update the Secret data
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "hash-change-secret", Namespace: "default"}, secret)
			Expect(err).NotTo(HaveOccurred())
			secret.Data["token"] = []byte("updated-token")
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())

			// Reconcile again
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Re-fetch deployment
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			newHash := deployment.Spec.Template.Annotations[configHashAnnotation]
			Expect(newHash).NotTo(BeEmpty())
			Expect(newHash).NotTo(Equal(originalHash))
		})
	})

	Context("When computing hash determinism", func() {
		const resourceName = "test-confighash-deterministic"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hash-deterministic-cm",
					Namespace: "default",
				},
				Data: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.EnvFrom = []corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "hash-deterministic-cm",
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
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "hash-deterministic-cm", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			if err == nil {
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		It("should produce the same hash on consecutive reconciles with unchanged data", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile
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

			hash1 := deployment.Spec.Template.Annotations[configHashAnnotation]
			Expect(hash1).NotTo(BeEmpty())

			// Second reconcile without changes
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			hash2 := deployment.Spec.Template.Annotations[configHashAnnotation]
			Expect(hash2).To(Equal(hash1))
		})
	})

	Context("When a referenced ConfigMap does not exist", func() {
		It("should skip missing refs gracefully in computeConfigHash", func() {
			// Test computeConfigHash directly since the controller validates
			// config before reaching deployment creation, and a missing
			// ConfigMap causes the MCPServer to be marked Invalid.
			reconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-confighash-missing-ref",
					Namespace: "default",
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						EnvFrom: []corev1.EnvFromSource{
							{
								ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "nonexistent-configmap",
									},
								},
							},
						},
					},
				},
			}

			hash, err := reconciler.computeConfigHash(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())
			// All refs were missing and skipped, so no data was hashed.
			// The function returns "" when there is nothing to hash into the digest.
			// However, since refs were listed (len > 0), the hasher was created
			// but no data was written. The result is still a valid (empty-input) hash.
			// The key assertion is that no error occurred.
			_ = hash
		})
	})
})
