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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

		deploy := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, typeNamespacedName, deploy); err == nil {
			Expect(k8sClient.Delete(ctx, deploy)).To(Succeed())
		}

		svc := &corev1.Service{}
		if err := k8sClient.Get(ctx, typeNamespacedName, svc); err == nil {
			Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
		}
	})

	It("should set MCPEndpointUnavailable when handshake fails", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("connection refused")
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

		By("Verifying HandshakeRetryCount is incremented")
		Expect(mcpServer.Status.HandshakeRetryCount).To(Equal(int32(1)))

		By("Verifying requeue is set")
		Expect(result.RequeueAfter).To(Equal(10 * time.Second))
	})

	It("should keep Ready=True when handshake succeeds", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, nil
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
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				dialerCalled = true
				return nil, nil
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
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				dialerCalled = true
				return nil, nil
			},
		}

		By("Setting replicas to 0")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Runtime.Replicas = new(int32(0))
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
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("MCP protocol error")
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
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				dialCount++
				if shouldFail {
					return nil, fmt.Errorf("intentional failure")
				}
				return &mcpv1alpha1.MCPServerInfo{
					Name:            "test-server",
					ProtocolVersion: "2025-03-26",
				}, nil
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
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				receivedCtx = ctx
				return nil, nil
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
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("connection refused")
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

		By("Simulating exhausted retries via HandshakeRetryCount status field")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Reason).To(Equal(ReasonMCPEndpointUnavailable))
		mcpServer.Status.HandshakeRetryCount = int32(maxMCPHandshakeRetries)
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

	It("should increment HandshakeRetryCount on each failed handshake", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("connection refused")
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

		By("First handshake failure sets HandshakeRetryCount to 1")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		Expect(mcpServer.Status.HandshakeRetryCount).To(Equal(int32(1)))

		By("Second handshake failure increments to 2")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		Expect(mcpServer.Status.HandshakeRetryCount).To(Equal(int32(2)))
	})

	It("should reset HandshakeRetryCount to 0 on successful handshake", func() {
		failHandshake := true
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				if failHandshake {
					return nil, fmt.Errorf("connection refused")
				}
				return &mcpv1alpha1.MCPServerInfo{Name: "test"}, nil
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

		By("Failed handshake sets retry count")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		Expect(mcpServer.Status.HandshakeRetryCount).To(Equal(int32(1)))

		By("Successful handshake resets retry count to 0")
		failHandshake = false
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		Expect(mcpServer.Status.HandshakeRetryCount).To(Equal(int32(0)))
	})

	It("should treat 401 Unauthorized as a reachable endpoint", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("POST %s: Unauthorized", url)
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
		Expect(mcpServer.Status.ServerInfo).NotTo(BeNil(), "auth error should set non-nil empty serverInfo to prevent re-dial")
	})

	It("should populate status.serverInfo from successful handshake", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				return &mcpv1alpha1.MCPServerInfo{
					Name:            "test-mcp-server",
					Version:         "1.2.3",
					ProtocolVersion: "2025-06-18",
					Instructions:    "A test server",
					Capabilities: &mcpv1alpha1.MCPServerCapabilities{
						Tools:     true,
						Resources: true,
						Prompts:   false,
						Logging:   true,
					},
				}, nil
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

		By("Reconciling with successful handshake")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Verifying status.serverInfo is populated")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		Expect(mcpServer.Status.ServerInfo).NotTo(BeNil())
		Expect(mcpServer.Status.ServerInfo.Name).To(Equal("test-mcp-server"))
		Expect(mcpServer.Status.ServerInfo.Version).To(Equal("1.2.3"))
		Expect(mcpServer.Status.ServerInfo.ProtocolVersion).To(Equal("2025-06-18"))
		Expect(mcpServer.Status.ServerInfo.Instructions).To(Equal("A test server"))
		Expect(mcpServer.Status.ServerInfo.Capabilities).NotTo(BeNil())
		Expect(mcpServer.Status.ServerInfo.Capabilities.Tools).To(BeTrue())
		Expect(mcpServer.Status.ServerInfo.Capabilities.Resources).To(BeTrue())
		Expect(mcpServer.Status.ServerInfo.Capabilities.Prompts).To(BeFalse())
		Expect(mcpServer.Status.ServerInfo.Capabilities.Logging).To(BeTrue())
		Expect(mcpServer.Status.ServerInfo.Capabilities.Completions).To(BeFalse())
	})

	It("should carry forward serverInfo when handshake is skipped", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				return &mcpv1alpha1.MCPServerInfo{
					Name:            "carry-forward-server",
					Version:         "2.0.0",
					ProtocolVersion: "2025-06-18",
				}, nil
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

		By("First reconcile - handshake runs, serverInfo populated")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		Expect(mcpServer.Status.ServerInfo).NotTo(BeNil())
		Expect(mcpServer.Status.ServerInfo.Name).To(Equal("carry-forward-server"))

		By("Second reconcile - handshake skipped, serverInfo preserved")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		Expect(mcpServer.Status.ServerInfo).NotTo(BeNil())
		Expect(mcpServer.Status.ServerInfo.Name).To(Equal("carry-forward-server"))
		Expect(mcpServer.Status.ServerInfo.Version).To(Equal("2.0.0"))
	})

	It("should treat 403 Forbidden as a reachable endpoint", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("POST %s: Forbidden", url)
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
		Expect(mcpServer.Status.ServerInfo).NotTo(BeNil(), "auth error should set non-nil empty serverInfo to prevent re-dial")
	})
})

var _ = Describe("extractServerInfo", func() {
	It("should return nil for nil input", func() {
		Expect(extractServerInfo(nil)).To(BeNil())
	})

	It("should extract protocol version and instructions", func() {
		result := &mcp.InitializeResult{
			ProtocolVersion: "2025-03-26",
			Instructions:    "A test server",
		}
		info := extractServerInfo(result)
		Expect(info).NotTo(BeNil())
		Expect(info.ProtocolVersion).To(Equal("2025-03-26"))
		Expect(info.Instructions).To(Equal("A test server"))
		Expect(info.Name).To(BeEmpty())
		Expect(info.Version).To(BeEmpty())
		Expect(info.Capabilities).To(BeNil())
	})

	It("should extract server name and version from ServerInfo", func() {
		result := &mcp.InitializeResult{
			ProtocolVersion: "2025-03-26",
			ServerInfo: &mcp.Implementation{
				Name:    "my-server",
				Version: "1.2.3",
			},
		}
		info := extractServerInfo(result)
		Expect(info).NotTo(BeNil())
		Expect(info.Name).To(Equal("my-server"))
		Expect(info.Version).To(Equal("1.2.3"))
	})

	It("should handle nil ServerInfo", func() {
		result := &mcp.InitializeResult{
			ProtocolVersion: "2025-03-26",
		}
		info := extractServerInfo(result)
		Expect(info).NotTo(BeNil())
		Expect(info.Name).To(BeEmpty())
		Expect(info.Version).To(BeEmpty())
	})

	It("should detect all capabilities when present", func() {
		result := &mcp.InitializeResult{
			ProtocolVersion: "2025-03-26",
			Capabilities: &mcp.ServerCapabilities{
				Tools:       &mcp.ToolCapabilities{},
				Resources:   &mcp.ResourceCapabilities{},
				Prompts:     &mcp.PromptCapabilities{},
				Logging:     &mcp.LoggingCapabilities{},
				Completions: &mcp.CompletionCapabilities{},
			},
		}
		info := extractServerInfo(result)
		Expect(info).NotTo(BeNil())
		Expect(info.Capabilities).NotTo(BeNil())
		Expect(info.Capabilities.Tools).To(BeTrue())
		Expect(info.Capabilities.Resources).To(BeTrue())
		Expect(info.Capabilities.Prompts).To(BeTrue())
		Expect(info.Capabilities.Logging).To(BeTrue())
		Expect(info.Capabilities.Completions).To(BeTrue())
	})

	It("should detect partial capabilities", func() {
		result := &mcp.InitializeResult{
			ProtocolVersion: "2025-03-26",
			Capabilities: &mcp.ServerCapabilities{
				Tools: &mcp.ToolCapabilities{},
			},
		}
		info := extractServerInfo(result)
		Expect(info).NotTo(BeNil())
		Expect(info.Capabilities).NotTo(BeNil())
		Expect(info.Capabilities.Tools).To(BeTrue())
		Expect(info.Capabilities.Resources).To(BeFalse())
		Expect(info.Capabilities.Prompts).To(BeFalse())
		Expect(info.Capabilities.Logging).To(BeFalse())
		Expect(info.Capabilities.Completions).To(BeFalse())
	})

	It("should handle nil Capabilities", func() {
		result := &mcp.InitializeResult{
			ProtocolVersion: "2025-03-26",
		}
		info := extractServerInfo(result)
		Expect(info).NotTo(BeNil())
		Expect(info.Capabilities).To(BeNil())
	})
})
