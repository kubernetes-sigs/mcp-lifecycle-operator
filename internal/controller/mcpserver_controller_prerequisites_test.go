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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Prerequisites", func() {
	ctx := context.Background()

	Context("Without the auto-create-prerequisites annotation", func() {
		const resourceName = "test-prereq-no-annotation"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Runtime.Security = mcpv1alpha1.SecurityConfig{
				ServiceAccountName: "should-not-be-created",
			}
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "should-not-be-created-cm",
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

		It("should NOT create prerequisites when annotation is absent", func() {
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// Reconcile will fail validation (missing CM/SA) — that's expected
			Expect(err).NotTo(HaveOccurred())

			// ServiceAccount should not exist
			sa := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name: "should-not-be-created", Namespace: "default",
			}, sa)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"ServiceAccount should not be created without annotation")

			// ConfigMap should not exist
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name: "should-not-be-created-cm", Namespace: "default",
			}, cm)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"ConfigMap should not be created without annotation")
		})
	})

	Context("With the auto-create-prerequisites annotation", func() {
		const resourceName = "test-prereq-with-annotation"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		AfterEach(func() {
			resource := &mcpv1alpha1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			// Clean up any created prerequisites (owner references should handle
			// this via GC, but envtest doesn't run GC)
			sa := &corev1.ServiceAccount{}
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name: "auto-created-sa", Namespace: "default",
			}, sa); err == nil {
				_ = k8sClient.Delete(ctx, sa)
			}
			cm := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Name: "auto-created-cm", Namespace: "default",
			}, cm); err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}
		})

		It("should create a missing ServiceAccount", func() {
			resource := newTestMCPServer(resourceName)
			resource.Annotations = map[string]string{
				AnnotationAutoCreatePrerequisites: "true",
			}
			resource.Spec.Runtime.Security = mcpv1alpha1.SecurityConfig{
				ServiceAccountName: "auto-created-sa",
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			sa := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name: "auto-created-sa", Namespace: "default",
			}, sa)
			Expect(err).NotTo(HaveOccurred(), "ServiceAccount should be auto-created")

			// Verify owner reference
			ownerRef := metav1.GetControllerOf(sa)
			Expect(ownerRef).NotTo(BeNil(), "ServiceAccount should have a controller owner reference")
			Expect(ownerRef.Name).To(Equal(resourceName))
			Expect(ownerRef.Kind).To(Equal("MCPServer"))
		})

		It("should create a missing ConfigMap", func() {
			resource := newTestMCPServer(resourceName)
			resource.Annotations = map[string]string{
				AnnotationAutoCreatePrerequisites: "true",
			}
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "auto-created-cm",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name: "auto-created-cm", Namespace: "default",
			}, cm)
			Expect(err).NotTo(HaveOccurred(), "ConfigMap should be auto-created")
			Expect(cm.Data).To(BeEmpty(), "Auto-created ConfigMap should have empty data")

			// Verify owner reference
			ownerRef := metav1.GetControllerOf(cm)
			Expect(ownerRef).NotTo(BeNil(), "ConfigMap should have a controller owner reference")
			Expect(ownerRef.Name).To(Equal(resourceName))
			Expect(ownerRef.Kind).To(Equal("MCPServer"))
		})

		It("should not modify existing resources", func() {
			// Pre-create the SA and CM with specific data
			existingSA := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auto-created-sa",
					Namespace: "default",
					Labels:    map[string]string{"preexisting": "true"},
				},
			}
			Expect(k8sClient.Create(ctx, existingSA)).To(Succeed())

			existingCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auto-created-cm",
					Namespace: "default",
					Labels:    map[string]string{"preexisting": "true"},
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(ctx, existingCM)).To(Succeed())

			resource := newTestMCPServer(resourceName)
			resource.Annotations = map[string]string{
				AnnotationAutoCreatePrerequisites: "true",
			}
			resource.Spec.Runtime.Security = mcpv1alpha1.SecurityConfig{
				ServiceAccountName: "auto-created-sa",
			}
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "auto-created-cm",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify SA was not modified
			sa := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name: "auto-created-sa", Namespace: "default",
			}, sa)
			Expect(err).NotTo(HaveOccurred())
			Expect(sa.Labels).To(HaveKeyWithValue("preexisting", "true"),
				"Existing ServiceAccount should not be modified")

			// Verify CM was not modified
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name: "auto-created-cm", Namespace: "default",
			}, cm)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Labels).To(HaveKeyWithValue("preexisting", "true"),
				"Existing ConfigMap should not be modified")
			Expect(cm.Data).To(HaveKeyWithValue("key", "value"),
				"Existing ConfigMap data should be preserved")
		})

		It("should emit events when creating prerequisites", func() {
			resource := newTestMCPServer(resourceName)
			resource.Annotations = map[string]string{
				AnnotationAutoCreatePrerequisites: "true",
			}
			resource.Spec.Runtime.Security = mcpv1alpha1.SecurityConfig{
				ServiceAccountName: "auto-created-sa",
			}
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path: "/config",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "auto-created-cm",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler, fr := newReconcilerForTestWithFakeEvents(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Collect all events
			events := drainEvents(fr.Events)

			// Should have at least the CreatedPrerequisite events
			saEventFound := false
			cmEventFound := false
			for _, ev := range events {
				if containsAll(ev, "CreatedPrerequisite", "ServiceAccount", "auto-created-sa") {
					saEventFound = true
				}
				if containsAll(ev, "CreatedPrerequisite", "ConfigMap", "auto-created-cm") {
					cmEventFound = true
				}
			}
			Expect(saEventFound).To(BeTrue(), "Should emit event for ServiceAccount creation, got events: %v", events)
			Expect(cmEventFound).To(BeTrue(), "Should emit event for ConfigMap creation, got events: %v", events)
		})

		It("should skip non-ConfigMap storage types", func() {
			resource := newTestMCPServer(resourceName)
			resource.Annotations = map[string]string{
				AnnotationAutoCreatePrerequisites: "true",
			}
			resource.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
				{
					Path:        "/tmp",
					Permissions: mcpv1alpha1.MountPermissionsReadWrite,
					Source: mcpv1alpha1.StorageSource{
						Type:     mcpv1alpha1.StorageTypeEmptyDir,
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle annotation set to a value other than true", func() {
			resource := newTestMCPServer(resourceName)
			resource.Annotations = map[string]string{
				AnnotationAutoCreatePrerequisites: "false",
			}
			resource.Spec.Runtime.Security = mcpv1alpha1.SecurityConfig{
				ServiceAccountName: "auto-created-sa",
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// Will fail validation because SA doesn't exist — that's expected
			Expect(err).NotTo(HaveOccurred())

			sa := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name: "auto-created-sa", Namespace: "default",
			}, sa)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"ServiceAccount should NOT be created when annotation is not 'true'")
		})
	})
})

// containsAll returns true if s contains all of the given substrings.
func containsAll(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
