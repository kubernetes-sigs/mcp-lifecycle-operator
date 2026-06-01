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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("MCPServer Predicate Filtering", Ordered, func() {
	var (
		mgrCancel context.CancelFunc
	)

	BeforeAll(func() {
		var mgrCtx context.Context
		mgrCtx, mgrCancel = context.WithCancel(ctx)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
		})
		Expect(err).NotTo(HaveOccurred())

		err = (&MCPServerReconciler{
			Client:    mgr.GetClient(),
			Scheme:    mgr.GetScheme(),
			Recorder:  events.NewFakeRecorder(testRecorderBuffer),
			MCPDialer: testMCPDialerNoop,
		}).SetupWithManager(mgr)
		Expect(err).NotTo(HaveOccurred())

		go func() {
			defer GinkgoRecover()
			err := mgr.Start(mgrCtx)
			Expect(err).NotTo(HaveOccurred())
		}()
	})

	AfterAll(func() {
		mgrCancel()
	})

	It("should not reconcile on status-only updates", func() {
		mcpServer := newTestMCPServer("predicate-status")
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, mcpServer)
		}()

		dep := &appsv1.Deployment{}
		depKey := types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}

		Eventually(func() error {
			return k8sClient.Get(ctx, depKey, dep)
		}, 10*time.Second).Should(Succeed())

		// Wait for Deployment to stabilize
		var stableVersion string
		Eventually(func() bool {
			if err := k8sClient.Get(ctx, depKey, dep); err != nil {
				return false
			}
			if dep.ResourceVersion == stableVersion {
				return true
			}
			stableVersion = dep.ResourceVersion
			return false
		}, 10*time.Second).Should(BeTrue())

		// Perform a status-only update.
		// Re-fetch inside Eventually to avoid conflicts with the running controller.
		serverKey := types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}
		Eventually(func() error {
			if err := k8sClient.Get(ctx, serverKey, mcpServer); err != nil {
				return err
			}
			mcpServer.Status.HandshakeRetryCount = 99
			return k8sClient.Status().Update(ctx, mcpServer)
		}, 10*time.Second).Should(Succeed())

		// Verify no reconciliation: Deployment resourceVersion must not change
		Consistently(func() string {
			Expect(k8sClient.Get(ctx, depKey, dep)).To(Succeed())
			return dep.ResourceVersion
		}, 5*time.Second).Should(Equal(stableVersion))
	})

	It("should reconcile on spec changes", func() {
		mcpServer := newTestMCPServer("predicate-spec")
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, mcpServer)
		}()

		dep := &appsv1.Deployment{}
		depKey := types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}

		Eventually(func() error {
			return k8sClient.Get(ctx, depKey, dep)
		}, 10*time.Second).Should(Succeed())

		// Update the MCPServer spec (change image).
		// Re-fetch inside Eventually to avoid conflicts with the running controller.
		serverKey := types.NamespacedName{Name: mcpServer.Name, Namespace: mcpServer.Namespace}
		Eventually(func() error {
			if err := k8sClient.Get(ctx, serverKey, mcpServer); err != nil {
				return err
			}
			mcpServer.Spec.Source.ContainerImage.Ref = "docker.io/library/updated-image:latest"
			return k8sClient.Update(ctx, mcpServer)
		}, 10*time.Second).Should(Succeed())

		// Verify reconciliation happened: Deployment should reflect the new image
		Eventually(func() string {
			if err := k8sClient.Get(ctx, depKey, dep); err != nil {
				return ""
			}
			if len(dep.Spec.Template.Spec.Containers) == 0 {
				return ""
			}
			return dep.Spec.Template.Spec.Containers[0].Image
		}, 10*time.Second).Should(Equal("docker.io/library/updated-image:latest"))
	})
})
