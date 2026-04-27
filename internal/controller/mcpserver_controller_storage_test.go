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
	"k8s.io/apimachinery/pkg/util/intstr"
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

	Context("When reconciling a resource with resources", func() {
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
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create deployment with resources", func() {
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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

	Context("Server-side apply for status updates", func() {
		const resourceName = "test-ssa-status"
		const subResourceStatus = "status"

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

		It("should use SubResourceApply for all status updates and never SubResourceUpdate or SubResourcePatch", func() {
			applyCallCount := 0
			updateCalled := false
			patchCalled := false

			wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())

			interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
				SubResourceApply: func(ctx context.Context, c client.Client, subResourceName string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
					if subResourceName == subResourceStatus {
						applyCallCount++
					}
					return c.SubResource(subResourceName).Apply(ctx, obj, opts...)
				},
				SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					if subResourceName == subResourceStatus {
						updateCalled = true
					}
					return c.SubResource(subResourceName).Update(ctx, obj, opts...)
				},
				SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					if subResourceName == subResourceStatus {
						patchCalled = true
					}
					return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			})

			controllerReconciler := &MCPServerReconciler{
				Client: interceptedClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(applyCallCount).To(BeNumerically(">", 0), "expected status updates to use SubResourceApply (SSA)")
			Expect(updateCalled).To(BeFalse(), "status should not use SubResourceUpdate")
			Expect(patchCalled).To(BeFalse(), "status should not use SubResourcePatch")
		})
	})

	Context("When reconciling a resource with health probes", func() {
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).To(BeNil())
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
			Expect(deployment.Spec.Template.Spec.Containers[0].ReadinessProbe).To(BeNil())
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
	})
})
