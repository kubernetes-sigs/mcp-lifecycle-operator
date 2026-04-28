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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var _ = Describe("MCPServer Controller - MCP Handshake Validation", func() {
	const resourceName = "test-handshake"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	BeforeEach(func() {
		resource := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
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

	It("should set MCPEndpointUnavailable when handshake fails", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				return fmt.Errorf("connection refused")
			},
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Simulating deployment becoming available")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name: resourceName, Namespace: "default",
		}, deployment)).To(Succeed())

		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		By("Reconciling with MCP handshake failure")
		result, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying Ready=False with reason MCPEndpointUnavailable")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonMCPEndpointUnavailable))
		Expect(readyCondition.Message).To(ContainSubstring("MCP endpoint is not serving a valid MCP protocol"))
		Expect(readyCondition.Message).To(ContainSubstring("connection refused"))

		By("Verifying requeue is set")
		Expect(result.RequeueAfter).To(Equal(10 * time.Second))
	})

	It("should keep Ready=True when handshake succeeds", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				return nil
			},
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Simulating deployment becoming available")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name: resourceName, Namespace: "default",
		}, deployment)).To(Succeed())

		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		By("Reconciling with MCP handshake success")
		result, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying Ready=True with reason Available")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(readyCondition.Reason).To(Equal(ReasonAvailable))

		By("Verifying no requeue")
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("should not attempt handshake when deployment is unavailable", func() {
		dialerCalled := false
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				dialerCalled = true
				return nil
			},
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Simulating deployment being unavailable (no ready replicas)")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name: resourceName, Namespace: "default",
		}, deployment)).To(Succeed())

		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 0
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{
				Type:   appsv1.DeploymentProgressing,
				Status: corev1.ConditionTrue,
				Reason: "NewReplicaSetCreated",
			},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		By("Resetting dialer call tracking before unavailable reconcile")
		dialerCalled = false

		By("Reconciling with unavailable deployment")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying MCPDialer was not called during unavailable reconcile")
		Expect(dialerCalled).To(BeFalse())
	})

	It("should not attempt handshake when scaled to zero", func() {
		dialerCalled := false
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				dialerCalled = true
				return nil
			},
		}

		By("Setting replicas to 0")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Runtime.Replicas = ptr.To(int32(0))
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Reconciling again")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying MCPDialer was not called")
		Expect(dialerCalled).To(BeFalse())

		By("Verifying Ready=True with ScaledToZero reason")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(readyCondition.Reason).To(Equal(ReasonScaledToZero))
	})

	It("should requeue on handshake failure", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				return fmt.Errorf("MCP protocol error")
			},
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Simulating deployment becoming available")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name: resourceName, Namespace: "default",
		}, deployment)).To(Succeed())

		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		By("Reconciling with MCP handshake failure")
		result, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying RequeueAfter is set to 10 seconds")
		Expect(result.RequeueAfter).To(Equal(10 * time.Second))
	})

	It("should skip handshake when already verified for current generation", func() {
		dialCount := 0
		shouldFail := true
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				dialCount++
				if shouldFail {
					return fmt.Errorf("intentional failure")
				}
				return nil
			},
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Simulating deployment becoming available")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name: resourceName, Namespace: "default",
		}, deployment)).To(Succeed())

		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		By("Reconciling with handshake failure to ensure Ready!=Available")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonMCPEndpointUnavailable))

		By("Switching to successful handshake - should run because Ready is not yet Available")
		shouldFail = false
		dialCount = 0
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(dialCount).To(Equal(1))

		By("Verifying Ready=True/Available is set")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(readyCondition.Reason).To(Equal(ReasonAvailable))

		By("Second reconcile - handshake should be skipped (already verified)")
		dialCount = 0
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(dialCount).To(Equal(0))
	})

	It("should pass a context with timeout to the dialer", func() {
		var receivedCtx context.Context
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				receivedCtx = ctx
				return nil
			},
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Simulating deployment becoming available")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name: resourceName, Namespace: "default",
		}, deployment)).To(Succeed())

		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		By("Reconciling to trigger handshake")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying the dialer received a context with a deadline")
		Expect(receivedCtx).NotTo(BeNil())
		_, ok := receivedCtx.Deadline()
		Expect(ok).To(BeTrue(), "context should have a deadline")
	})

	It("should stop requeuing after max retries are exhausted", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				return fmt.Errorf("connection refused")
			},
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Simulating deployment becoming available")
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name: resourceName, Namespace: "default",
		}, deployment)).To(Succeed())

		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		By("First reconciliation with handshake failure")
		result, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).NotTo(BeZero(), "should requeue on first failure")

		By("Simulating exhausted retries by backdating the condition's LastTransitionTime")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Reason).To(Equal(ReasonMCPEndpointUnavailable))
		// Backdate to 20 minutes ago - well past the total backoff budget
		readyCondition.LastTransitionTime = metav1.NewTime(time.Now().Add(-20 * time.Minute))
		meta.SetStatusCondition(&mcpServer.Status.Conditions, *readyCondition)
		Expect(k8sClient.Status().Update(ctx, mcpServer)).To(Succeed())

		By("Reconciling after retries exhausted")
		result, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying no requeue (retries exhausted)")
		Expect(result.RequeueAfter).To(BeZero(), "should not requeue after max retries")

		By("Verifying status is still MCPEndpointUnavailable")
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition = meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition.Reason).To(Equal(ReasonMCPEndpointUnavailable))
	})

	It("should use exponential backoff for handshake requeue delays", func() {
		By("Verifying backoff schedule")
		Expect(mcpHandshakeBackoff(0)).To(Equal(10 * time.Second))
		Expect(mcpHandshakeBackoff(1)).To(Equal(20 * time.Second))
		Expect(mcpHandshakeBackoff(2)).To(Equal(40 * time.Second))
		Expect(mcpHandshakeBackoff(3)).To(Equal(80 * time.Second))
		Expect(mcpHandshakeBackoff(4)).To(Equal(2 * time.Minute))
		Expect(mcpHandshakeBackoff(5)).To(Equal(2 * time.Minute))
		Expect(mcpHandshakeBackoff(100)).To(Equal(2 * time.Minute))
	})

	It("should count retries from condition timestamp", func() {
		By("No existing condition returns 0")
		Expect(mcpHandshakeRetryCount(nil)).To(Equal(0))

		By("Condition with different reason returns 0")
		conditions := []metav1.Condition{
			{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonDeploymentUnavailable,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
		}
		Expect(mcpHandshakeRetryCount(conditions)).To(Equal(0))

		By("Recently transitioned condition returns 0")
		conditions = []metav1.Condition{
			{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonMCPEndpointUnavailable,
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(mcpHandshakeRetryCount(conditions)).To(Equal(0))

		By("Condition old enough for several retries returns correct count")
		// After 75s (10+20+40=70s for 3 retries), count should be 3
		conditions = []metav1.Condition{
			{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonMCPEndpointUnavailable,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-75 * time.Second)),
			},
		}
		Expect(mcpHandshakeRetryCount(conditions)).To(Equal(3))

		By("Very old condition returns max retries")
		conditions = []metav1.Condition{
			{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonMCPEndpointUnavailable,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
			},
		}
		Expect(mcpHandshakeRetryCount(conditions)).To(Equal(maxMCPHandshakeRetries))
	})

	It("should treat 401 Unauthorized as a reachable endpoint", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				return fmt.Errorf("POST %s: Unauthorized", url)
			},
		}

		By("Creating deployment and marking it available")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)).To(Succeed())
		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.AvailableReplicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(readyCondition.Reason).To(Equal(ReasonAvailable))
	})

	It("should treat 403 Forbidden as a reachable endpoint", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) error {
				return fmt.Errorf("POST %s: Forbidden", url)
			},
		}

		By("Creating deployment and marking it available")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: "default",
		}, deployment)).To(Succeed())
		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.AvailableReplicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(readyCondition.Reason).To(Equal(ReasonAvailable))
	})
})
