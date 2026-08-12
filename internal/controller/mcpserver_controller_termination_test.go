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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("MCPServer Controller - Namespace Termination", func() {
	ctx := context.Background()

	Context("When the MCPServer is being deleted", func() {
		const resourceName = "test-deletion-skip"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		It("should skip reconciliation when the MCPServer has a DeletionTimestamp", func() {
			By("Creating an MCPServer with a finalizer")
			resource := newTestMCPServer(resourceName)
			resource.Finalizers = []string{"test.finalizer/block-deletion"}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Deleting the MCPServer to set DeletionTimestamp")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Reconciling the deleting MCPServer")
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			By("Verifying no Deployment was created")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, typeNamespacedName, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("Cleaning up the finalizer")
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Finalizers = nil
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())
		})
	})

	Context("When the namespace is being terminated", func() {
		const namespaceName = "test-ns-terminating"
		const resourceName = "test-ns-term-skip"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespaceName,
		}

		It("should skip reconciliation when the namespace is terminating", func() {
			By("Creating a namespace with a finalizer")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:       namespaceName,
					Finalizers: []string{"test.finalizer/block-deletion"},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			By("Creating an MCPServer in the namespace")
			resource := newTestMCPServer(resourceName)
			resource.Namespace = namespaceName
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Deleting the namespace to set its DeletionTimestamp")
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())

			By("Reconciling the MCPServer in the terminating namespace")
			controllerReconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			By("Verifying no Deployment was created")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, typeNamespacedName, deployment)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("Cleaning up the namespace finalizer")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, ns)).To(Succeed())
			ns.Finalizers = nil
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())
		})
	})
})
