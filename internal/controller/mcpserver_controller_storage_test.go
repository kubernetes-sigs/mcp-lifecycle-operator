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
	stderrors "errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Controller - Storage Mounts", func() {
	ctx := context.Background()

	Context("When reconciling a resource with ConfigMap storage", func() {
		const resourceName = "test-resource-configmap-storage"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create ConfigMap first
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-configmap",
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
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-configmap", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
		})

		It("should create deployment with ConfigMap volume and mount", func() {
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

			// Verify volume is created with auto-generated name
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.Name).To(Equal("vol-0"))
			Expect(volume.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(volume.VolumeSource.ConfigMap.Name).To(Equal("test-configmap"))

			// Verify volume mount is created
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			volumeMount := container.VolumeMounts[0]
			Expect(volumeMount.Name).To(Equal("vol-0"))
			Expect(volumeMount.MountPath).To(Equal("/etc/config"))
			Expect(volumeMount.ReadOnly).To(BeTrue()) // Default is true
		})
	})

	Context("When reconciling a resource with Secret storage", func() {
		const resourceName = "test-resource-secret-storage"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create Secret first
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				StringData: map[string]string{
					"token": "secret-value",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/secret",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeSecret,
						Secret: &corev1.SecretVolumeSource{
							SecretName: "test-secret",
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
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-secret", Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should create deployment with Secret volume and mount", func() {
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

			// Verify volume is created with auto-generated name
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.Name).To(Equal("vol-0"))
			Expect(volume.VolumeSource.Secret).NotTo(BeNil())
			Expect(volume.VolumeSource.Secret.SecretName).To(Equal("test-secret"))

			// Verify volume mount is created
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			volumeMount := container.VolumeMounts[0]
			Expect(volumeMount.Name).To(Equal("vol-0"))
			Expect(volumeMount.MountPath).To(Equal("/etc/secret"))
			Expect(volumeMount.ReadOnly).To(BeTrue()) // Default is true
		})
	})

	Context("When reconciling a resource with multiple storage mounts", func() {
		const resourceName = "test-resource-multi-storage"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create ConfigMap
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multi-configmap",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			// Create Secret
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multi-secret",
					Namespace: "default",
				},
				StringData: map[string]string{
					"token": "secret-value",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-multi-configmap",
							},
						},
					},
				},
				{
					Path: "/etc/secret",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeSecret,
						Secret: &corev1.SecretVolumeSource{
							SecretName: "test-multi-secret",
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
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-multi-configmap", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-multi-secret", Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should create deployment with multiple volumes and mounts with correct names", func() {
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

			// Verify both volumes are created with auto-generated names
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(2))

			volume0 := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume0.Name).To(Equal("vol-0"))
			Expect(volume0.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(volume0.VolumeSource.ConfigMap.Name).To(Equal("test-multi-configmap"))

			volume1 := deployment.Spec.Template.Spec.Volumes[1]
			Expect(volume1.Name).To(Equal("vol-1"))
			Expect(volume1.VolumeSource.Secret).NotTo(BeNil())
			Expect(volume1.VolumeSource.Secret.SecretName).To(Equal("test-multi-secret"))

			// Verify both volume mounts are created
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(2))

			volumeMount0 := container.VolumeMounts[0]
			Expect(volumeMount0.Name).To(Equal("vol-0"))
			Expect(volumeMount0.MountPath).To(Equal("/etc/config"))
			Expect(volumeMount0.ReadOnly).To(BeTrue())

			volumeMount1 := container.VolumeMounts[1]
			Expect(volumeMount1.Name).To(Equal("vol-1"))
			Expect(volumeMount1.MountPath).To(Equal("/etc/secret"))
			Expect(volumeMount1.ReadOnly).To(BeTrue())
		})
	})

	Context("When reconciling a resource with readOnly set to false", func() {
		const resourceName = "test-resource-readonly-false"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create ConfigMap
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap-rw",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path:        "/etc/config",
					Permissions: mcpv1alpha1.MountPermissionsReadWrite, // Explicitly set to read-write
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-configmap-rw",
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
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-configmap-rw", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
		})

		It("should create deployment with readOnly set to false", func() {
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

			// Verify volume mount has ReadOnly set to false
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			volumeMount := container.VolumeMounts[0]
			Expect(volumeMount.Name).To(Equal("vol-0"))
			Expect(volumeMount.MountPath).To(Equal("/etc/config"))
			Expect(volumeMount.ReadOnly).To(BeFalse()) // Explicitly false, not default
		})
	})

	Context("When reconciling a resource with EmptyDir storage", func() {
		const resourceName = "test-resource-emptydir-storage"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path:        "/app/logs",
					Permissions: mcpv1alpha1.MountPermissionsReadWrite,
					Source: mcpv1alpha1.StorageSource{
						Type:     mcpv1alpha1.StorageTypeEmptyDir,
						EmptyDir: &corev1.EmptyDirVolumeSource{},
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

		It("should create deployment with EmptyDir volume and mount", func() {
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

			// Verify volume is created with auto-generated name
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.Name).To(Equal("vol-0"))
			Expect(volume.VolumeSource.EmptyDir).NotTo(BeNil())

			// Verify volume mount is created with ReadWrite permissions
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			volumeMount := container.VolumeMounts[0]
			Expect(volumeMount.Name).To(Equal("vol-0"))
			Expect(volumeMount.MountPath).To(Equal("/app/logs"))
			Expect(volumeMount.ReadOnly).To(BeFalse()) // ReadWrite
		})
	})

	Context("When reconciling a resource with EmptyDir storage with sizeLimit", func() {
		const resourceName = "test-resource-emptydir-sizelimit"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			sizeLimit := resource.MustParse("100Mi")
			mcpServer := newTestMCPServer(resourceName)
			mcpServer.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path:        "/tmp/cache",
					Permissions: mcpv1alpha1.MountPermissionsReadWrite,
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeEmptyDir,
						EmptyDir: &corev1.EmptyDirVolumeSource{
							SizeLimit: &sizeLimit,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create deployment with EmptyDir volume with sizeLimit", func() {
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

			// Verify EmptyDir has sizeLimit set
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.VolumeSource.EmptyDir).NotTo(BeNil())
			Expect(volume.VolumeSource.EmptyDir.SizeLimit).NotTo(BeNil())
			Expect(volume.VolumeSource.EmptyDir.SizeLimit.String()).To(Equal("100Mi"))
		})
	})

	Context("When reconciling a resource with mixed storage types including EmptyDir", func() {
		const resourceName = "test-resource-mixed-storage"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Create ConfigMap
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mixed-configmap",
					Namespace: "default",
				},
				Data: map[string]string{
					"config.yaml": "test: value",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			mcpServer := newTestMCPServer(resourceName)
			mcpServer.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "test-mixed-configmap",
							},
						},
					},
				},
				{
					Path:        "/app/logs",
					Permissions: mcpv1alpha1.MountPermissionsReadWrite,
					Source: mcpv1alpha1.StorageSource{
						Type:     mcpv1alpha1.StorageTypeEmptyDir,
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-mixed-configmap", Namespace: "default"}, configMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			}
		})

		It("should create deployment with both ConfigMap and EmptyDir volumes", func() {
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

			// Verify both volumes are created
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(2))

			volume0 := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume0.Name).To(Equal("vol-0"))
			Expect(volume0.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(volume0.VolumeSource.ConfigMap.Name).To(Equal("test-mixed-configmap"))

			volume1 := deployment.Spec.Template.Spec.Volumes[1]
			Expect(volume1.Name).To(Equal("vol-1"))
			Expect(volume1.VolumeSource.EmptyDir).NotTo(BeNil())

			// Verify both volume mounts
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(2))

			volumeMount0 := container.VolumeMounts[0]
			Expect(volumeMount0.Name).To(Equal("vol-0"))
			Expect(volumeMount0.MountPath).To(Equal("/etc/config"))
			Expect(volumeMount0.ReadOnly).To(BeTrue()) // ConfigMap default

			volumeMount1 := container.VolumeMounts[1]
			Expect(volumeMount1.Name).To(Equal("vol-1"))
			Expect(volumeMount1.MountPath).To(Equal("/app/logs"))
			Expect(volumeMount1.ReadOnly).To(BeFalse()) // ReadWrite
		})
	})

	Context("When ConfigMap reference doesn't exist", func() {
		const resourceName = "test-resource-missing-configmap"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "nonexistent-configmap",
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

		It("should set Accepted=False with 'ConfigMap not found' message", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify MCPServer status has Accepted=False
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-configmap"))
			Expect(acceptedCondition.Message).To(ContainSubstring("not found"))
		})
	})

	Context("When Secret reference doesn't exist", func() {
		const resourceName = "test-resource-missing-secret"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/secret",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeSecret,
						Secret: &corev1.SecretVolumeSource{
							SecretName: "nonexistent-secret",
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

		It("should set Accepted=False with 'Secret not found' message", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify MCPServer status has Accepted=False
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("nonexistent-secret"))
			Expect(acceptedCondition.Message).To(ContainSubstring("not found"))
		})
	})

	Context("When ConfigMap is optional and doesn't exist", func() {
		const resourceName = "test-resource-optional-configmap"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Don't create the ConfigMap - it should be optional
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "optional-configmap",
							},
							Optional: ptr.To(true),
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

		It("should succeed reconciliation even when ConfigMap doesn't exist", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify deployment was created
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify volume is created with optional ConfigMap reference
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.VolumeSource.ConfigMap).NotTo(BeNil())
			Expect(volume.VolumeSource.ConfigMap.Name).To(Equal("optional-configmap"))
			Expect(volume.VolumeSource.ConfigMap.Optional).NotTo(BeNil())
			Expect(*volume.VolumeSource.ConfigMap.Optional).To(BeTrue())
		})
	})

	Context("When Secret is optional and doesn't exist", func() {
		const resourceName = "test-resource-optional-secret"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			// Don't create the Secret - it should be optional
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/secret",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeSecret,
						Secret: &corev1.SecretVolumeSource{
							SecretName: "optional-secret",
							Optional:   ptr.To(true),
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

		It("should succeed reconciliation even when Secret doesn't exist", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify deployment was created
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      resourceName,
				Namespace: "default",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify volume is created with optional Secret reference
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
			volume := deployment.Spec.Template.Spec.Volumes[0]
			Expect(volume.VolumeSource.Secret).NotTo(BeNil())
			Expect(volume.VolumeSource.Secret.SecretName).To(Equal("optional-secret"))
			Expect(volume.VolumeSource.Secret.Optional).NotTo(BeNil())
			Expect(*volume.VolumeSource.Secret.Optional).To(BeTrue())
		})
	})

	Context("When ConfigMap name is empty", func() {
		const resourceName = "test-resource-empty-configmap-name"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "", // Empty name
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

		It("should set Accepted=False when ConfigMap name is empty", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify MCPServer status has Accepted=False
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("ConfigMap name must not be empty"))
			Expect(acceptedCondition.Message).To(ContainSubstring("index 0"))
		})
	})

	Context("When Secret name is empty", func() {
		const resourceName = "test-resource-empty-secret-name"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/etc/secret",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeSecret,
						Secret: &corev1.SecretVolumeSource{
							SecretName: "", // Empty name
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

		It("should set Accepted=False when Secret name is empty", func() {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// No error should be returned - configuration issues are reported via status conditions
			Expect(err).NotTo(HaveOccurred())

			// Verify MCPServer status has Accepted=False
			mcpServer := &mcpv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())

			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
			Expect(acceptedCondition.Message).To(ContainSubstring("Secret name must not be empty"))
			Expect(acceptedCondition.Message).To(ContainSubstring("index 0"))
		})
	})

	Context("validateConfig validation", func() {
		ctx := context.Background()

		It("should reject EmptyDir with nil EmptyDir configuration", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/data",
								Source: mcpv1alpha1.StorageSource{
									Type:     mcpv1alpha1.StorageTypeEmptyDir,
									EmptyDir: nil, // Intentionally nil
								},
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeTrue())
			Expect(validationErr.Reason).To(Equal(ReasonInvalid))
			Expect(validationErr.Message).To(ContainSubstring("EmptyDir must be set"))
		})

		It("should reject unknown storage type", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/data",
								Source: mcpv1alpha1.StorageSource{
									Type: "UnknownType", // Invalid storage type
								},
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeTrue())
			Expect(validationErr.Reason).To(Equal(ReasonInvalid))
			Expect(validationErr.Message).To(ContainSubstring("Unsupported storage type"))
			Expect(validationErr.Message).To(ContainSubstring("UnknownType"))
		})

		It("should reject env valueFrom with missing ConfigMap", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Env: []corev1.EnvVar{
							{
								Name: "MY_VAR",
								ValueFrom: &corev1.EnvVarSource{
									ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "missing-cm"},
										Key:                  "key1",
									},
								},
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeTrue())
			Expect(validationErr.Reason).To(Equal(ReasonInvalid))
			Expect(validationErr.Message).To(ContainSubstring("missing-cm"))
			Expect(validationErr.Message).To(ContainSubstring("MY_VAR"))
		})

		It("should reject env valueFrom with missing Secret", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Env: []corev1.EnvVar{
							{
								Name: "SECRET_VAR",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "missing-secret"},
										Key:                  "key1",
									},
								},
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeTrue())
			Expect(validationErr.Reason).To(Equal(ReasonInvalid))
			Expect(validationErr.Message).To(ContainSubstring("missing-secret"))
			Expect(validationErr.Message).To(ContainSubstring("SECRET_VAR"))
		})

		It("should accept env valueFrom with optional missing ConfigMap", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			optional := true
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Env: []corev1.EnvVar{
							{
								Name: "MY_VAR",
								ValueFrom: &corev1.EnvVarSource{
									ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "missing-cm"},
										Key:                  "key1",
										Optional:             &optional,
									},
								},
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should accept env valueFrom with optional missing Secret", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			optional := true
			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Env: []corev1.EnvVar{
							{
								Name: "SECRET_VAR",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "missing-secret"},
										Key:                  "key1",
										Optional:             &optional,
									},
								},
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should accept env with literal value (no valueFrom)", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Env: []corev1.EnvVar{
							{
								Name:  "SIMPLE_VAR",
								Value: "simple-value",
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return transient error for ConfigMap timeout without marking invalid", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			// Create a fake client that returns timeout error for ConfigMap Get
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.ConfigMap); ok {
							return &errors.StatusError{
								ErrStatus: metav1.Status{
									Status:  metav1.StatusFailure,
									Code:    500,
									Reason:  metav1.StatusReasonInternalError,
									Message: "the server has timed out",
								},
							}
						}
						return client.Get(ctx, key, obj, opts...)
					},
				}).Build()

			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Storage: []mcpv1alpha1.StorageMount{
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
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			// Should NOT be a ValidationError - should be a transient error
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("transient error validating ConfigMap"))
		})

		It("should return transient error for Secret timeout without marking invalid", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			// Create a fake client that returns timeout error for Secret Get
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.Secret); ok {
							return &errors.StatusError{
								ErrStatus: metav1.Status{
									Status:  metav1.StatusFailure,
									Code:    503,
									Reason:  metav1.StatusReasonServiceUnavailable,
									Message: "the server is currently unable to handle the request",
								},
							}
						}
						return client.Get(ctx, key, obj, opts...)
					},
				}).Build()

			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Storage: []mcpv1alpha1.StorageMount{
							{
								Path: "/secret",
								Source: mcpv1alpha1.StorageSource{
									Type: mcpv1alpha1.StorageTypeSecret,
									Secret: &corev1.SecretVolumeSource{
										SecretName: "test-secret",
									},
								},
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			// Should NOT be a ValidationError - should be a transient error
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("transient error validating Secret"))
		})

		It("should return transient error for envFrom ConfigMap timeout", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.ConfigMap); ok {
							return &errors.StatusError{
								ErrStatus: metav1.Status{
									Status:  metav1.StatusFailure,
									Code:    429,
									Reason:  metav1.StatusReasonTooManyRequests,
									Message: "rate limited",
								},
							}
						}
						return client.Get(ctx, key, obj, opts...)
					},
				}).Build()

			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						EnvFrom: []corev1.EnvFromSource{
							{
								ConfigMapRef: &corev1.ConfigMapEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "test-config",
									},
								},
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("transient error validating ConfigMap"))
			Expect(err.Error()).To(ContainSubstring("envFrom"))
		})

		It("should return transient error for env valueFrom Secret timeout", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.Secret); ok {
							return &errors.StatusError{
								ErrStatus: metav1.Status{
									Status:  metav1.StatusFailure,
									Code:    504,
									Reason:  metav1.StatusReasonTimeout,
									Message: "gateway timeout",
								},
							}
						}
						return client.Get(ctx, key, obj, opts...)
					},
				}).Build()

			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Env: []corev1.EnvVar{
							{
								Name: "MY_SECRET",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "test-secret",
										},
										Key: "key",
									},
								},
							},
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("transient error validating Secret"))
			Expect(err.Error()).To(ContainSubstring("env"))
		})

		It("should return transient error for Forbidden", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.ConfigMap); ok {
							return &errors.StatusError{
								ErrStatus: metav1.Status{
									Status:  metav1.StatusFailure,
									Code:    403,
									Reason:  metav1.StatusReasonForbidden,
									Message: "forbidden: User cannot get configmaps",
								},
							}
						}
						return client.Get(ctx, key, obj, opts...)
					},
				}).Build()

			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Storage: []mcpv1alpha1.StorageMount{
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
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			// Forbidden is transient - RBAC changes don't trigger reconciliation
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("transient error validating ConfigMap"))
		})

		It("should return transient error for Unauthorized", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.ConfigMap); ok {
							return &errors.StatusError{
								ErrStatus: metav1.Status{
									Status:  metav1.StatusFailure,
									Code:    401,
									Reason:  metav1.StatusReasonUnauthorized,
									Message: "unauthorized",
								},
							}
						}
						return client.Get(ctx, key, obj, opts...)
					},
				}).Build()

			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Storage: []mcpv1alpha1.StorageMount{
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
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			// Unauthorized is transient - RBAC changes don't trigger reconciliation
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("transient error validating ConfigMap"))
		})

		It("should return ValidationError for BadRequest (permanent error)", func() {
			scheme := runtime.NewScheme()
			Expect(mcpv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*corev1.ConfigMap); ok {
							return &errors.StatusError{
								ErrStatus: metav1.Status{
									Status:  metav1.StatusFailure,
									Code:    400,
									Reason:  metav1.StatusReasonBadRequest,
									Message: "bad request",
								},
							}
						}
						return client.Get(ctx, key, obj, opts...)
					},
				}).Build()

			reconciler := &MCPServerReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			mcpServer := &mcpv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-server",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: mcpv1alpha1.MCPServerSpec{
					Config: mcpv1alpha1.ServerConfig{
						Storage: []mcpv1alpha1.StorageMount{
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
						},
					},
				},
			}

			err := reconciler.validateConfig(ctx, mcpServer)
			Expect(err).To(HaveOccurred())
			// BadRequest is a permanent ValidationError
			var validationErr *ValidationError
			Expect(stderrors.As(err, &validationErr)).To(BeTrue())
			Expect(validationErr.Reason).To(Equal(ReasonInvalid))
			Expect(validationErr.Message).To(ContainSubstring("Invalid ConfigMap"))
		})
	})
})
