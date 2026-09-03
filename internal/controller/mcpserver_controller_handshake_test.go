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
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
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

func generateSelfSignedCAPEMOnly() []byte {
	caPEM, _ := generateSelfSignedCAPEM()
	return caPEM
}

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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("connection refused")
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, nil
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				dialerCalled = true
				return nil, nil
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				dialerCalled = true
				return nil, nil
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("MCP protocol error")
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				dialCount++
				if shouldFail {
					return nil, fmt.Errorf("intentional failure")
				}
				return &mcpv1alpha1.MCPServerInfo{
					Name:            "test-server",
					ProtocolVersion: "2025-03-26",
				}, nil
			},
			APIReader: k8sClient,
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

	It("should emit a Normal ServerReady event only when Ready transitions to Available after handshake", func() {
		shouldFail := true
		reconciler, fr := newReconcilerForTestWithFakeEvents(k8sClient, k8sClient.Scheme())
		reconciler.MCPDialer = func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
			if shouldFail {
				return nil, fmt.Errorf("intentional failure")
			}
			return &mcpv1alpha1.MCPServerInfo{
				Name:            "test-server",
				ProtocolVersion: "2025-03-26",
			}, nil
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		drainFakeRecorderEvents(fr)

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

		By("Reconciling with handshake failure — no ServerReady event")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		drainFakeRecorderEvents(fr)

		By("Successful handshake — ServerReady event emitted once")
		shouldFail = false
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		var serverReadyEvent string
		Eventually(fr.Events).Should(Receive(&serverReadyEvent))
		Expect(serverReadyEvent).To(ContainSubstring(corev1.EventTypeNormal))
		Expect(serverReadyEvent).To(ContainSubstring(ReasonAvailable))
		Expect(serverReadyEvent).To(ContainSubstring(resourceName))
		Expect(serverReadyEvent).To(ContainSubstring("Ready=True"))

		By("Second reconcile — no duplicate ServerReady event")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Consistently(fr.Events, 300*time.Millisecond, 20*time.Millisecond).ShouldNot(Receive())
	})

	It("should emit a Warning MCPHandshakeFailed event only when handshake error message changes", func() {
		failMsg := "intentional failure"
		reconciler, fr := newReconcilerForTestWithFakeEvents(k8sClient, k8sClient.Scheme())
		reconciler.MCPDialer = func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
			return nil, fmt.Errorf("%s", failMsg)
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		drainFakeRecorderEvents(fr)

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

		By("First handshake failure — Warning event emitted once")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		var handshakeFailedEvent string
		Eventually(fr.Events).Should(Receive(&handshakeFailedEvent))
		Expect(handshakeFailedEvent).To(ContainSubstring(corev1.EventTypeWarning))
		Expect(handshakeFailedEvent).To(ContainSubstring(ReasonMCPEndpointUnavailable))
		Expect(handshakeFailedEvent).To(ContainSubstring(resourceName))
		Expect(handshakeFailedEvent).To(ContainSubstring(failMsg))

		By("Second reconcile with same error — no duplicate handshake failed event")
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Consistently(fr.Events, 300*time.Millisecond, 20*time.Millisecond).ShouldNot(Receive())

		By("Change error message — second Warning event emitted")
		failMsg = "different failure message"
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		var secondHandshakeFailedEvent string
		Eventually(fr.Events).Should(Receive(&secondHandshakeFailedEvent))
		Expect(secondHandshakeFailedEvent).To(ContainSubstring(corev1.EventTypeWarning))
		Expect(secondHandshakeFailedEvent).To(ContainSubstring(ReasonMCPEndpointUnavailable))
		Expect(secondHandshakeFailedEvent).To(ContainSubstring(resourceName))
		Expect(secondHandshakeFailedEvent).To(ContainSubstring(failMsg))
		Expect(secondHandshakeFailedEvent).NotTo(Equal(handshakeFailedEvent))
	})

	It("should emit MCPHandshakeRetriesExhausted once when max handshake retries is reached", func() {
		reconciler, fr := newReconcilerForTestWithFakeEvents(k8sClient, k8sClient.Scheme())
		reconciler.MCPDialer = func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
			return nil, fmt.Errorf("intentional failure")
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		drainFakeRecorderEvents(fr)

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

		By("Reconciling until handshake retries are exhausted")
		for i := range maxMCPHandshakeRetries {
			result, recErr := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(recErr).NotTo(HaveOccurred())
			if i < maxMCPHandshakeRetries-1 {
				Expect(result.RequeueAfter).To(BeNumerically(">", 0))
			}
		}

		var collected []string
		Eventually(func(g Gomega) {
			collected = drainEvents(fr.Events)
			exhausted := 0
			for _, ev := range collected {
				if strings.Contains(ev, "retries exhausted") {
					exhausted++
				}
			}
			g.Expect(exhausted).To(Equal(1))
		}).Should(Succeed())
		var exhaustedEvent string
		for _, ev := range collected {
			if strings.Contains(ev, "retries exhausted") {
				exhaustedEvent = ev
				break
			}
		}
		Expect(exhaustedEvent).To(ContainSubstring(corev1.EventTypeWarning))
		Expect(exhaustedEvent).To(ContainSubstring(ReasonMCPEndpointUnavailable))
		Expect(exhaustedEvent).To(ContainSubstring(resourceName))

		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		Expect(mcpServer.Status.HandshakeRetryCount).To(BeNumerically(">=", maxMCPHandshakeRetries))

		By("Further reconcile — no duplicate exhausted event")
		drainFakeRecorderEvents(fr)
		result, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
		Consistently(fr.Events, 300*time.Millisecond, 20*time.Millisecond).ShouldNot(Receive())
	})

	It("should pass a context with timeout to the dialer", func() {
		var receivedCtx context.Context
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				receivedCtx = ctx
				return nil, nil
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("connection refused")
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("connection refused")
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				if failHandshake {
					return nil, fmt.Errorf("connection refused")
				}
				return &mcpv1alpha1.MCPServerInfo{Name: "test"}, nil
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("POST %s: Unauthorized", url)
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				return &mcpv1alpha1.MCPServerInfo{
					Name:            "test-mcp-server",
					Version:         "1.2.3",
					ProtocolVersion: "2025-06-18",
					Instructions:    "A test server",
					Capabilities: &mcpv1alpha1.MCPServerCapabilities{
						Tools:     true,
						Resources: true,
						Prompts:   false,
						Logging:   true, //nolint:staticcheck // exercising the deprecated Logging field on purpose
					},
				}, nil
			},
			APIReader: k8sClient,
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
		Expect(mcpServer.Status.ServerInfo.Capabilities.Logging).To(BeTrue()) //nolint:staticcheck // TODO: remove after SEP-2577 deprecation window (mid-2027)
		Expect(mcpServer.Status.ServerInfo.Capabilities.Completions).To(BeFalse())
	})

	It("should carry forward serverInfo when handshake is skipped", func() {
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				return &mcpv1alpha1.MCPServerInfo{
					Name:            "carry-forward-server",
					Version:         "2.0.0",
					ProtocolVersion: "2025-06-18",
				}, nil
			},
			APIReader: k8sClient,
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
			MCPDialer: func(ctx context.Context, url string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				return nil, fmt.Errorf("POST %s: Forbidden", url)
			},
			APIReader: k8sClient,
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

var _ = Describe("capabilityDiffMessage", func() {
	It("should return empty string when both are nil", func() {
		Expect(capabilityDiffMessage(nil, nil)).To(BeEmpty())
	})

	It("should return empty string when capabilities are the same", func() {
		caps := &mcpv1alpha1.MCPServerCapabilities{
			Tools:     true,
			Resources: false,
			Prompts:   true,
		}
		Expect(capabilityDiffMessage(caps, caps.DeepCopy())).To(BeEmpty())
	})

	It("should detect tools added", func() {
		old := &mcpv1alpha1.MCPServerCapabilities{Tools: false}
		new := &mcpv1alpha1.MCPServerCapabilities{Tools: true}
		diff := capabilityDiffMessage(old, new)
		Expect(diff).To(ContainSubstring("tools: false->true"))
	})

	It("should detect multiple changes", func() {
		old := &mcpv1alpha1.MCPServerCapabilities{Tools: true, Prompts: true}
		new := &mcpv1alpha1.MCPServerCapabilities{Tools: false, Resources: true}
		diff := capabilityDiffMessage(old, new)
		Expect(diff).To(ContainSubstring("tools: true->false"))
		Expect(diff).To(ContainSubstring("resources: false->true"))
		Expect(diff).To(ContainSubstring("prompts: true->false"))
	})

	It("should treat old nil as all-false", func() {
		new := &mcpv1alpha1.MCPServerCapabilities{Tools: true, Resources: true}
		diff := capabilityDiffMessage(nil, new)
		Expect(diff).To(ContainSubstring("tools: false->true"))
		Expect(diff).To(ContainSubstring("resources: false->true"))
	})
})

var _ = Describe("capabilityChangeMessage", func() {
	It("should return empty when serverInfo is nil", func() {
		mcpServer := &mcpv1alpha1.MCPServer{
			Status: mcpv1alpha1.MCPServerStatus{
				ServerInfo: &mcpv1alpha1.MCPServerInfo{
					Capabilities: &mcpv1alpha1.MCPServerCapabilities{Tools: true},
				},
			},
		}
		Expect(capabilityChangeMessage(mcpServer, nil)).To(BeEmpty())
	})

	It("should return empty when status serverInfo is nil", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		serverInfo := &mcpv1alpha1.MCPServerInfo{
			Capabilities: &mcpv1alpha1.MCPServerCapabilities{Tools: true},
		}
		Expect(capabilityChangeMessage(mcpServer, serverInfo)).To(BeEmpty())
	})

	It("should return empty when both capabilities are nil", func() {
		mcpServer := &mcpv1alpha1.MCPServer{
			Status: mcpv1alpha1.MCPServerStatus{
				ServerInfo: &mcpv1alpha1.MCPServerInfo{},
			},
		}
		serverInfo := &mcpv1alpha1.MCPServerInfo{}
		Expect(capabilityChangeMessage(mcpServer, serverInfo)).To(BeEmpty())
	})

	It("should return diff when old capabilities are nil and new are non-nil", func() {
		mcpServer := &mcpv1alpha1.MCPServer{
			Status: mcpv1alpha1.MCPServerStatus{
				ServerInfo: &mcpv1alpha1.MCPServerInfo{},
			},
		}
		serverInfo := &mcpv1alpha1.MCPServerInfo{
			Capabilities: &mcpv1alpha1.MCPServerCapabilities{Tools: true, Resources: true},
		}
		diff := capabilityChangeMessage(mcpServer, serverInfo)
		Expect(diff).To(ContainSubstring("tools: false->true"))
		Expect(diff).To(ContainSubstring("resources: false->true"))
	})

	It("should return diff when old capabilities are non-nil and new are nil", func() {
		mcpServer := &mcpv1alpha1.MCPServer{
			Status: mcpv1alpha1.MCPServerStatus{
				ServerInfo: &mcpv1alpha1.MCPServerInfo{
					Capabilities: &mcpv1alpha1.MCPServerCapabilities{Tools: true},
				},
			},
		}
		serverInfo := &mcpv1alpha1.MCPServerInfo{}
		Expect(capabilityChangeMessage(mcpServer, serverInfo)).
			To(ContainSubstring("tools: true->false"))
	})

	It("should return diff when capabilities changed", func() {
		mcpServer := &mcpv1alpha1.MCPServer{
			Status: mcpv1alpha1.MCPServerStatus{
				ServerInfo: &mcpv1alpha1.MCPServerInfo{
					Capabilities: &mcpv1alpha1.MCPServerCapabilities{Tools: false},
				},
			},
		}
		serverInfo := &mcpv1alpha1.MCPServerInfo{
			Capabilities: &mcpv1alpha1.MCPServerCapabilities{Tools: true},
		}
		Expect(capabilityChangeMessage(mcpServer, serverInfo)).To(ContainSubstring("tools: false->true"))
	})
})

var _ = Describe("emitCapabilityChangeDetected", func() {
	It("should not panic when Recorder is nil", func() {
		r := &MCPServerReconciler{}
		mcpServer := &mcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}
		Expect(func() { r.emitCapabilityChangeDetected(mcpServer, "tools: false->true") }).NotTo(Panic())
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
				Logging:     &mcp.LoggingCapabilities{}, //nolint:staticcheck // TODO: remove after SEP-2577 deprecation window (mid-2027)
				Completions: &mcp.CompletionCapabilities{},
			},
		}
		info := extractServerInfo(result)
		Expect(info).NotTo(BeNil())
		Expect(info.Capabilities).NotTo(BeNil())
		Expect(info.Capabilities.Tools).To(BeTrue())
		Expect(info.Capabilities.Resources).To(BeTrue())
		Expect(info.Capabilities.Prompts).To(BeTrue())
		Expect(info.Capabilities.Logging).To(BeTrue()) //nolint:staticcheck // TODO: remove after SEP-2577 deprecation window (mid-2027)
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
		Expect(info.Capabilities.Logging).To(BeFalse()) //nolint:staticcheck // TODO: remove after SEP-2577 deprecation window (mid-2027)
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

var _ = Describe("MCPServer Controller - TLS Handshake", func() {
	const resourceName = "test-tls"

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

	It("should set ConfigurationInvalid when TLS CA Secret is missing", func() {
		By("Updating MCPServer with TLS config referencing a nonexistent Secret")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Transport = &mcpv1alpha1.TransportConfig{
			TLS: &mcpv1alpha1.TLSClientConfig{
				Enabled: true,
				CABundleSecret: &mcpv1alpha1.SecretReference{
					Name: "nonexistent-ca",
				},
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
		Expect(acceptedCondition.Message).To(ContainSubstring("TLS CA bundle Secret not found"))
	})

	It("should pass InsecureSkipVerify transport to the dialer with https URL", func() {
		By("Updating MCPServer with InsecureSkipVerify TLS config")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Transport = &mcpv1alpha1.TransportConfig{
			TLS: &mcpv1alpha1.TLSClientConfig{
				Enabled:            true,
				InsecureSkipVerify: true,
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		var capturedTransport *http.Transport
		var capturedURL string
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(_ context.Context, url string, transport *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				capturedTransport = transport
				capturedURL = url
				return &mcpv1alpha1.MCPServerInfo{Name: "test"}, nil
			},
			APIReader: k8sClient,
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Making deployment available and re-reconciling")
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

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(capturedTransport).NotTo(BeNil())
		Expect(capturedTransport.TLSClientConfig).NotTo(BeNil())
		Expect(capturedTransport.TLSClientConfig.InsecureSkipVerify).To(BeTrue())
		Expect(capturedURL).To(HavePrefix("https://"))
	})

	It("should apply TLSProfile to the handshake transport", func() {
		By("Updating MCPServer with InsecureSkipVerify TLS config")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Transport = &mcpv1alpha1.TransportConfig{
			TLS: &mcpv1alpha1.TLSClientConfig{
				Enabled:            true,
				InsecureSkipVerify: true,
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		var capturedTransport *http.Transport
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(_ context.Context, _ string, transport *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				capturedTransport = transport
				return &mcpv1alpha1.MCPServerInfo{Name: "test"}, nil
			},
			APIReader: k8sClient,
			TLSProfile: func(c *tls.Config) {
				c.MinVersion = tls.VersionTLS13
			},
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Making deployment available and re-reconciling")
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

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(capturedTransport).NotTo(BeNil())
		Expect(capturedTransport.TLSClientConfig).NotTo(BeNil())
		Expect(capturedTransport.TLSClientConfig.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
	})

	It("should not allow TLSProfile to lower MinVersion below TLS 1.2", func() {
		By("Updating MCPServer with InsecureSkipVerify TLS config")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Transport = &mcpv1alpha1.TransportConfig{
			TLS: &mcpv1alpha1.TLSClientConfig{
				Enabled:            true,
				InsecureSkipVerify: true,
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		var capturedTransport *http.Transport
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(_ context.Context, _ string, transport *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				capturedTransport = transport
				return &mcpv1alpha1.MCPServerInfo{Name: "test"}, nil
			},
			APIReader: k8sClient,
			TLSProfile: func(c *tls.Config) {
				c.MinVersion = tls.VersionTLS10
			},
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Making deployment available and re-reconciling")
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

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(capturedTransport).NotTo(BeNil())
		Expect(capturedTransport.TLSClientConfig).NotTo(BeNil())
		Expect(capturedTransport.TLSClientConfig.MinVersion).To(Equal(uint16(tls.VersionTLS12)),
			"TLS 1.2 floor must not be lowered by TLSProfile")
	})

	It("should set Accepted=False when CA Secret has invalid PEM data", func() {
		By("Creating a Secret with invalid PEM data")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bad-ca",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca.crt": []byte("not-a-valid-pem"),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		By("Updating MCPServer with TLS config referencing the bad Secret")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Transport = &mcpv1alpha1.TransportConfig{
			TLS: &mcpv1alpha1.TLSClientConfig{
				Enabled:        true,
				CABundleSecret: &mcpv1alpha1.SecretReference{Name: "bad-ca"},
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
		Expect(acceptedCondition.Message).To(ContainSubstring("no valid PEM certificates"))

		By("Cleaning up Secret")
		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
	})

	It("should set Accepted=False when CA Secret is missing ca.crt key", func() {
		By("Creating a Secret without the ca.crt key")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "no-cacrt",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"tls.crt": []byte("some-data"),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		By("Updating MCPServer with TLS config referencing the Secret")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Transport = &mcpv1alpha1.TransportConfig{
			TLS: &mcpv1alpha1.TLSClientConfig{
				Enabled:        true,
				CABundleSecret: &mcpv1alpha1.SecretReference{Name: "no-cacrt"},
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
		Expect(acceptedCondition.Message).To(ContainSubstring("ca.crt"))

		By("Cleaning up Secret")
		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
	})

	It("should set Accepted=False when CA Secret contains non-certificate PEM", func() {
		By("Creating a Secret with a private key PEM instead of a certificate")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "privkey-ca",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca.crt": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not-a-cert")}),
			},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		By("Updating MCPServer with TLS config referencing the Secret")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Transport = &mcpv1alpha1.TransportConfig{
			TLS: &mcpv1alpha1.TLSClientConfig{
				Enabled:        true,
				CABundleSecret: &mcpv1alpha1.SecretReference{Name: "privkey-ca"},
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
		Expect(acceptedCondition.Message).To(ContainSubstring("no valid PEM certificates"))

		By("Cleaning up Secret")
		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
	})

	It("should pass nil transport when no TLS is configured", func() {
		var capturedTransport *http.Transport
		transportCaptured := false

		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(_ context.Context, _ string, transport *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				capturedTransport = transport
				transportCaptured = true
				return &mcpv1alpha1.MCPServerInfo{Name: "test"}, nil
			},
			APIReader: k8sClient,
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Making deployment available and re-reconciling")
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

		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(transportCaptured).To(BeTrue())
		Expect(capturedTransport).To(BeNil())

		By("Verifying URL uses http:// scheme")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		Expect(mcpServer.Status.Address).NotTo(BeNil())
		Expect(mcpServer.Status.Address.URL).To(HavePrefix("http://"))
	})

	It("should reject insecureSkipVerify combined with caBundleSecret", func() {
		By("Creating a CA Secret")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "conflict-ca",
				Namespace: "default",
			},
			Data: map[string][]byte{"ca.crt": []byte("dummy")},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		By("Updating MCPServer with both insecureSkipVerify and caBundleSecret")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Transport = &mcpv1alpha1.TransportConfig{
			TLS: &mcpv1alpha1.TLSClientConfig{
				Enabled:            true,
				InsecureSkipVerify: true,
				CABundleSecret:     &mcpv1alpha1.SecretReference{Name: "conflict-ca"},
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		acceptedCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Accepted")
		Expect(acceptedCondition).NotTo(BeNil())
		Expect(acceptedCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(acceptedCondition.Reason).To(Equal(ReasonInvalid))
		Expect(acceptedCondition.Message).To(ContainSubstring("mutually exclusive"))

		By("Cleaning up Secret")
		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
	})

	It("should re-run handshake when CA bundle Secret content changes", func() {
		By("Creating a CA bundle Secret with valid PEM")
		originalPEM := generateSelfSignedCAPEMOnly()
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rotation-ca",
				Namespace: "default",
			},
			Data: map[string][]byte{"ca.crt": originalPEM},
		}
		Expect(k8sClient.Create(ctx, caSecret)).To(Succeed())

		By("Updating MCPServer with TLS caBundleSecret config")
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		mcpServer.Spec.Transport = &mcpv1alpha1.TransportConfig{
			TLS: &mcpv1alpha1.TLSClientConfig{
				Enabled:        true,
				CABundleSecret: &mcpv1alpha1.SecretReference{Name: "rotation-ca"},
			},
		}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		dialCount := 0
		reconciler := &MCPServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			MCPDialer: func(_ context.Context, _ string, _ *http.Transport) (*mcpv1alpha1.MCPServerInfo, error) {
				dialCount++
				return &mcpv1alpha1.MCPServerInfo{Name: "test"}, nil
			},
			APIReader: k8sClient,
		}

		By("Initial reconciliation creates deployment")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("Making deployment available")
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

		By("Reconciling to establish Ready=True with handshake")
		dialCount = 0
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(dialCount).To(Equal(1))

		Expect(k8sClient.Get(ctx, typeNamespacedName, mcpServer)).To(Succeed())
		readyCondition := meta.FindStatusCondition(mcpServer.Status.Conditions, "Ready")
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		hashKey := mcpServer.Namespace + "/" + mcpServer.Name
		storedVal, ok := reconciler.tlsCABundleHashes.Load(hashKey)
		Expect(ok).To(BeTrue())
		originalHash := storedVal.(string)
		Expect(originalHash).NotTo(BeEmpty())

		By("Reconciling again without changes - handshake should be skipped")
		dialCount = 0
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(dialCount).To(Equal(0))

		By("Rotating the CA bundle Secret content")
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name: "rotation-ca", Namespace: "default",
		}, caSecret)).To(Succeed())
		rotatedPEM := generateSelfSignedCAPEMOnly()
		caSecret.Data["ca.crt"] = rotatedPEM
		Expect(k8sClient.Update(ctx, caSecret)).To(Succeed())

		By("Reconciling after rotation - handshake must re-run (hash changed)")
		dialCount = 0
		_, err = reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(dialCount).To(Equal(1))

		storedVal, ok = reconciler.tlsCABundleHashes.Load(hashKey)
		Expect(ok).To(BeTrue())
		Expect(storedVal.(string)).NotTo(Equal(originalHash))

		By("Cleaning up Secret")
		Expect(k8sClient.Delete(ctx, caSecret)).To(Succeed())
	})
})
