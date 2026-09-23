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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

var _ = Describe("MCPServer Controller - Error Recovery", func() {
	ctx := context.Background()

	Context("When missing envFrom ConfigMap is created after failure", func() {
		const resourceName = "test-recovery-cm"
		const configMapName = "recovery-configmap"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.EnvFrom = []corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: "default"}, cm)
			if err == nil {
				Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
			}
		})

		It("should recover from Failed to Pending when missing ConfigMap is created", func() {
			controllerReconciler := &MCPServerReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				APIReader: k8sClient,
			}

			By("First reconcile fails due to missing ConfigMap")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status is Failed")
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal("Invalid"))
			Expect(acceptedCondition.Message).To(ContainSubstring("recovery-configmap"))
			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCondition.Reason).To(Equal("ConfigurationInvalid"))

			By("Verifying no Deployment was created")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("Creating the missing ConfigMap")
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "default"},
				Data:       map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			By("Second reconcile succeeds after ConfigMap is available")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status recovered to Pending")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(acceptedCondition.Reason).To(Equal("Valid"))
			availableCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Reason).To(Equal("Initializing"))

			By("Verifying Deployment was created on recovery")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		})
	})

	Context("When missing envFrom Secret is created after failure", func() {
		const resourceName = "test-recovery-secret"
		const secretName = "recovery-secret"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.EnvFrom = []corev1.EnvFromSource{
				{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should recover from Failed to Pending when missing Secret is created", func() {
			controllerReconciler := &MCPServerReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				APIReader: k8sClient,
			}

			By("First reconcile fails due to missing Secret")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status is Failed")
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal("Invalid"))
			Expect(acceptedCondition.Message).To(ContainSubstring("recovery-secret"))
			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCondition.Reason).To(Equal("ConfigurationInvalid"))

			By("Verifying no Deployment was created")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("Creating the missing Secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
				Data:       map[string][]byte{"key": []byte("value")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("Second reconcile succeeds after Secret is available")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status recovered to Pending")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(acceptedCondition.Reason).To(Equal("Valid"))
			availableCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Reason).To(Equal("Initializing"))

			By("Verifying Deployment was created on recovery")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		})
	})

	Context("When missing storage ConfigMap is created after failure", func() {
		const resourceName = "test-recovery-storage"
		const configMapName = "recovery-storage-cm"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Storage = []mcpv1beta1.StorageMount{
				{
					Path: "/etc/config",
					Source: mcpv1beta1.StorageSource{
						Type: mcpv1beta1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: "default"}, cm)
			if err == nil {
				Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
			}
		})

		It("should recover from Failed to Pending when missing storage ConfigMap is created", func() {
			controllerReconciler := &MCPServerReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				APIReader: k8sClient,
			}

			By("First reconcile fails due to missing storage ConfigMap")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status is Failed")
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal("Invalid"))
			Expect(acceptedCondition.Message).To(ContainSubstring("recovery-storage-cm"))
			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCondition.Reason).To(Equal("ConfigurationInvalid"))

			By("Creating the missing ConfigMap")
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "default"},
				Data:       map[string]string{"config.yaml": "data: value"},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			By("Second reconcile succeeds after ConfigMap is available")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status recovered to Pending")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(acceptedCondition.Reason).To(Equal("Valid"))
			availableCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Reason).To(Equal("Initializing"))

			By("Verifying Deployment was created on recovery")
			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		})
	})

	Context("When missing env valueFrom ConfigMap is created after failure", func() {
		const resourceName = "test-recovery-env-valuefrom-cm"
		const configMapName = "recovery-env-valuefrom-cm"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Env = []corev1.EnvVar{
				{
					Name: "RECOVERY_VAR",
					ValueFrom: &corev1.EnvVarSource{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
							Key:                  "some-key",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: "default"}, cm)
			if err == nil {
				Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
			}
		})

		It("should recover from Failed to Pending when missing env valueFrom ConfigMap is created", func() {
			controllerReconciler := &MCPServerReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				APIReader: k8sClient,
			}

			By("First reconcile fails due to missing ConfigMap")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status is Failed")
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal("Invalid"))
			Expect(acceptedCondition.Message).To(ContainSubstring(configMapName))
			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCondition.Reason).To(Equal("ConfigurationInvalid"))

			By("Verifying no Deployment was created")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("Creating the missing ConfigMap")
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "default"},
				Data:       map[string]string{"some-key": "value"},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			By("Second reconcile succeeds after ConfigMap is available")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status recovered to Pending")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(acceptedCondition.Reason).To(Equal("Valid"))
			availableCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Reason).To(Equal("Initializing"))

			By("Verifying Deployment was created on recovery")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		})
	})

	Context("When missing env valueFrom Secret is created after failure", func() {
		const resourceName = "test-recovery-env-valuefrom-secret"
		const secretName = "recovery-env-valuefrom-secret"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := newTestMCPServer(resourceName)
			resource.Spec.Config.Env = []corev1.EnvVar{
				{
					Name: "SECRET_RECOVERY_VAR",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
							Key:                  "some-key",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &mcpv1beta1.MCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: secretName, Namespace: "default"}, secret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			}
		})

		It("should recover from Failed to Pending when missing env valueFrom Secret is created", func() {
			controllerReconciler := &MCPServerReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				APIReader: k8sClient,
			}

			By("First reconcile fails due to missing Secret")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status is Failed")
			mcpServer := &mcpv1beta1.MCPServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(acceptedCondition.Reason).To(Equal("Invalid"))
			Expect(acceptedCondition.Message).To(ContainSubstring(secretName))
			availableCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(availableCondition.Reason).To(Equal("ConfigurationInvalid"))

			By("Verifying no Deployment was created")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("Creating the missing Secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
				Data:       map[string][]byte{"some-key": []byte("value")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("Second reconcile succeeds after Secret is available")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status recovered to Pending")
			Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
			acceptedCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
			Expect(acceptedCondition).NotTo(BeNil())
			Expect(acceptedCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(acceptedCondition.Reason).To(Equal("Valid"))
			availableCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Available")
			Expect(availableCondition).NotTo(BeNil())
			Expect(availableCondition.Reason).To(Equal("Initializing"))

			By("Verifying Deployment was created on recovery")
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		})
	})
})

var _ = Describe("MCPServer Controller - Optimistic Locking Conflicts", func() {
	const resourceName = "test-conflict"

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
		resource := &mcpv1beta1.MCPServer{}
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

	It("should requeue without a reconcile error on a transient deployment update conflict", func() {
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

		By("Updating MCPServer spec to trigger a deployment update")
		mcpServer := &mcpv1beta1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Env = []corev1.EnvVar{{Name: "CONFLICT_VAR", Value: "value"}}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Creating interceptor that returns conflict on deployment Update")
		updateCallCount := 0
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					updateCallCount++
					return errors.NewConflict(
						schema.GroupResource{Group: "apps", Resource: "deployments"},
						obj.GetName(),
						fmt.Errorf("the object has been modified"),
					)
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		conflictReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling with conflict interceptor")
		result, err := conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred(),
			"a transient conflict must not surface as a reconcile error (avoids false alerts, issue #87)")
		Expect(result.RequeueAfter).To(BeNumerically(">", 0), "conflict must trigger a requeue")
		Expect(updateCallCount).To(Equal(1))
	})

	It("should succeed on retry after conflict is resolved", func() {
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

		By("Updating MCPServer spec to trigger a deployment update")
		mcpServer := &mcpv1beta1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Env = []corev1.EnvVar{{Name: "RETRY_VAR", Value: "value"}}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Creating interceptor that returns conflict only on first Update")
		updateCallCount := 0
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					updateCallCount++
					if updateCallCount == 1 {
						return errors.NewConflict(
							schema.GroupResource{Group: "apps", Resource: "deployments"},
							obj.GetName(),
							fmt.Errorf("the object has been modified"),
						)
					}
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		conflictReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("First reconcile hits the conflict and requeues without error")
		result, err := conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred(),
			"a transient conflict must not surface as a reconcile error (avoids false alerts, issue #87)")
		Expect(result.RequeueAfter).To(BeNumerically(">", 0), "conflict must trigger a requeue")

		By("Second reconcile succeeds (conflict resolved)")
		_, err = conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying deployment was updated with the new env var")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: "default"}, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
			corev1.EnvVar{Name: "RETRY_VAR", Value: "value"},
		))
	})

	It("should requeue without a reconcile error on a transient service update conflict", func() {
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

		By("Updating MCPServer port to trigger a service update")
		mcpServer := &mcpv1beta1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Config.Port = 9090
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Creating interceptor that returns conflict on service Update")
		updateCallCount := 0
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())

		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*corev1.Service); ok {
					updateCallCount++
					return errors.NewConflict(
						schema.GroupResource{Group: "", Resource: "services"},
						obj.GetName(),
						fmt.Errorf("the object has been modified"),
					)
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		conflictReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling with conflict interceptor")
		result, err := conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred(),
			"a transient conflict must not surface as a reconcile error (avoids false alerts, issue #87)")
		Expect(result.RequeueAfter).To(BeNumerically(">", 0), "conflict must trigger a requeue")
		Expect(updateCallCount).To(Equal(1))
	})

	It("should not flicker the Available condition to False on a transient deployment update conflict", func() {
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

		By("Capturing the baseline Available condition after a successful reconcile")
		mcpServer := &mcpv1beta1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		baseline := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
		Expect(baseline).NotTo(BeNil())
		Expect(baseline.Status).NotTo(Equal(metav1.ConditionFalse))
		baselineReason := baseline.Reason
		baselineMessage := baseline.Message
		baselineTransition := baseline.LastTransitionTime

		By("Updating MCPServer spec to trigger a deployment update")
		mcpServer.Spec.Config.Env = []corev1.EnvVar{{Name: "FLICKER_VAR", Value: "value"}}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Creating interceptor that returns conflict on deployment Update")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return errors.NewConflict(
						schema.GroupResource{Group: "apps", Resource: "deployments"},
						obj.GetName(),
						fmt.Errorf("the object has been modified"),
					)
				}
				return c.Update(ctx, obj, opts...)
			},
		})
		conflictReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling with the conflict interceptor")
		result, err := conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred(),
			"a transient conflict must not surface as a reconcile error (avoids false alerts, issue #87)")
		Expect(result.RequeueAfter).To(BeNumerically(">", 0), "conflict must trigger a requeue")

		By("Verifying the Available condition was preserved (no flicker to False)")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		after := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
		Expect(after).NotTo(BeNil())
		Expect(after.Reason).NotTo(Equal(ReasonDeploymentUnavailable),
			"transient optimistic-lock conflict must not flip Available to DeploymentUnavailable")
		Expect(after.Status).NotTo(Equal(metav1.ConditionFalse))
		Expect(after.Reason).To(Equal(baselineReason))
		Expect(after.Message).To(Equal(baselineMessage))
		Expect(after.LastTransitionTime).To(Equal(baselineTransition))
	})

	It("should not flicker the Available condition to False on a transient service update conflict", func() {
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

		By("Capturing the baseline Available condition after a successful reconcile")
		mcpServer := &mcpv1beta1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		baseline := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
		Expect(baseline).NotTo(BeNil())
		Expect(baseline.Status).NotTo(Equal(metav1.ConditionFalse))
		baselineReason := baseline.Reason

		By("Updating MCPServer port to trigger a service update")
		mcpServer.Spec.Config.Port = 9090
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Creating interceptor that returns conflict on service Update")
		wrappedClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		interceptedClient := interceptor.NewClient(wrappedClient, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*corev1.Service); ok {
					return errors.NewConflict(
						schema.GroupResource{Group: "", Resource: "services"},
						obj.GetName(),
						fmt.Errorf("the object has been modified"),
					)
				}
				return c.Update(ctx, obj, opts...)
			},
		})
		conflictReconciler := &MCPServerReconciler{
			Client:    interceptedClient,
			Scheme:    k8sClient.Scheme(),
			APIReader: k8sClient,
		}

		By("Reconciling with the conflict interceptor")
		result, err := conflictReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred(),
			"a transient conflict must not surface as a reconcile error (avoids false alerts, issue #87)")
		Expect(result.RequeueAfter).To(BeNumerically(">", 0), "conflict must trigger a requeue")

		By("Verifying the Available condition was preserved (no flicker to False)")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		after := meta.FindStatusCondition(mcpServer.Status.Conditions, ConditionTypeAvailable)
		Expect(after).NotTo(BeNil())
		Expect(after.Reason).NotTo(Equal(ReasonServiceUnavailable),
			"transient optimistic-lock conflict must not flip Available to ServiceUnavailable")
		Expect(after.Status).NotTo(Equal(metav1.ConditionFalse))
		Expect(after.Reason).To(Equal(baselineReason))
	})
})
