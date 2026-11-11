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
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	// The agentsandbox provider is registered (and wired) in suite_test.go, so the
	// reconciler can resolve the "agentsandbox" provider these tests select.
)

const (
	toolsetTestNamespace = "default"
	toolsetDispImage     = "quay.io/matzew/execute-code:test"
	toolsetRunnerImage   = "quay.io/matzew/codemode-runner:test"
)

// newToolsetReconciler builds a reconciler wired against the envtest client.
func newToolsetReconciler() *MCPToolsetReconciler {
	return &MCPToolsetReconciler{
		Client:          k8sClient,
		Scheme:          k8sClient.Scheme(),
		DispatcherImage: toolsetDispImage,
		RunnerImage:     toolsetRunnerImage,
	}
}

// createReadyServer creates an MCPServer and publishes a verified endpoint so the
// resolver treats it as reachable.
func createReadyServer(ctx context.Context, name, url string) {
	server := newTestMCPServer(name)
	Expect(k8sClient.Create(ctx, server)).To(Succeed())

	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: toolsetTestNamespace}, server)).To(Succeed())
	server.Status.Address = &mcpv1beta1.MCPServerAddress{URL: url}
	meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
		Type:    ConditionTypeVerified,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonVerified,
		Message: "ready for test",
	})
	Expect(k8sClient.Status().Update(ctx, server)).To(Succeed())
}

// deleteToolsetResources removes a toolset and every dispatcher resource it owns.
// envtest has no garbage collector, so owner-referenced children must be deleted
// explicitly.
func deleteToolsetResources(ctx context.Context, toolset string, servers ...string) {
	name := dispatcherName(toolset)
	objMeta := metav1.ObjectMeta{Name: name, Namespace: toolsetTestNamespace}
	_ = k8sClient.Delete(ctx, &mcpv1alpha1.MCPToolset{ObjectMeta: metav1.ObjectMeta{Name: toolset, Namespace: toolsetTestNamespace}})
	_ = k8sClient.Delete(ctx, &appsv1.Deployment{ObjectMeta: objMeta})
	_ = k8sClient.Delete(ctx, &corev1.Service{ObjectMeta: objMeta})
	_ = k8sClient.Delete(ctx, &corev1.ServiceAccount{ObjectMeta: objMeta})
	_ = k8sClient.Delete(ctx, &rbacv1.Role{ObjectMeta: objMeta})
	_ = k8sClient.Delete(ctx, &rbacv1.RoleBinding{ObjectMeta: objMeta})
	for _, s := range servers {
		_ = k8sClient.Delete(ctx, &mcpv1beta1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: s, Namespace: toolsetTestNamespace}})
	}
}

var _ = Describe("MCPToolset Controller", func() {
	ctx := context.Background()

	Context("When reconciling a valid toolset (US1, quickstart S1)", func() {
		const (
			toolsetName = "ops-toolset"
			serverName  = "kubernetes"
		)
		toolsetKey := types.NamespacedName{Name: toolsetName, Namespace: toolsetTestNamespace}
		dispKey := types.NamespacedName{Name: dispatcherName(toolsetName), Namespace: toolsetTestNamespace}

		AfterEach(func() {
			deleteToolsetResources(ctx, toolsetName, serverName)
		})

		It("reaches Ready=True, publishes the endpoint, and deploys the dispatcher", func() {
			createReadyServer(ctx, serverName, "http://kubernetes.default.svc:8080/mcp")

			toolset := &mcpv1alpha1.MCPToolset{
				ObjectMeta: metav1.ObjectMeta{Name: toolsetName, Namespace: toolsetTestNamespace},
				Spec: mcpv1alpha1.MCPToolsetSpec{
					Servers:  []string{serverName},
					Provider: "agentsandbox",
				},
			}
			Expect(k8sClient.Create(ctx, toolset)).To(Succeed())

			By("deploying the dispatcher but staying out of Ready until it is available")
			res, err := newToolsetReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: toolsetKey})
			Expect(err).NotTo(HaveOccurred())
			// envtest has no Deployment controller, so the dispatcher never reports
			// Available on its own: readiness must be withheld (gated on availability).
			Expect(res.RequeueAfter).To(BeNumerically(">", 0))

			got := &mcpv1alpha1.MCPToolset{}
			Expect(k8sClient.Get(ctx, toolsetKey, got)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(got.Status.Conditions, ConditionTypeServersResolved)).To(BeTrue())
			progressing := meta.FindStatusCondition(got.Status.Conditions, ConditionTypeReady)
			Expect(progressing).NotTo(BeNil())
			Expect(progressing.Status).To(Equal(metav1.ConditionFalse))
			Expect(progressing.Reason).To(Equal(ReasonDispatcherProgressing))
			Expect(got.Status.ExecuteEndpoint).To(BeEmpty())

			By("creating the dispatcher Deployment with the dispatcher image and runner-image env")
			deploy := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, dispKey, deploy)).To(Succeed())
			container := deploy.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal(toolsetDispImage))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: envDispRunnerImage, Value: toolsetRunnerImage}))
			Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: envDispProvider, Value: "agentsandbox"}))
			Expect(container.ReadinessProbe).NotTo(BeNil())
			Expect(deploy.Spec.Template.Spec.ServiceAccountName).To(Equal(dispKey.Name))

			By("becoming Ready once the dispatcher Deployment reports Available")
			deploy.Status.ObservedGeneration = deploy.Generation
			deploy.Status.Replicas = 1
			deploy.Status.AvailableReplicas = 1
			deploy.Status.ReadyReplicas = 1
			deploy.Status.Conditions = []appsv1.DeploymentCondition{{
				Type:    appsv1.DeploymentAvailable,
				Status:  corev1.ConditionTrue,
				Reason:  "MinimumReplicasAvailable",
				Message: "available for test",
			}}
			Expect(k8sClient.Status().Update(ctx, deploy)).To(Succeed())

			_, err = newToolsetReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: toolsetKey})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, toolsetKey, got)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(got.Status.Conditions, ConditionTypeReady)).To(BeTrue())
			Expect(got.Status.ResolvedServers).To(ConsistOf(serverName))
			Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
			wantEndpoint := fmt.Sprintf("http://%s.%s.svc:%d/mcp", dispKey.Name, toolsetTestNamespace, dispatcherPort)
			Expect(got.Status.ExecuteEndpoint).To(Equal(wantEndpoint))

			By("creating the dispatcher Service on the execute port")
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, dispKey, svc)).To(Succeed())
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(dispatcherPort)))

			By("creating the ServiceAccount, Role, and RoleBinding the dispatcher needs")
			Expect(k8sClient.Get(ctx, dispKey, &corev1.ServiceAccount{})).To(Succeed())
			Expect(k8sClient.Get(ctx, dispKey, &rbacv1.Role{})).To(Succeed())
			Expect(k8sClient.Get(ctx, dispKey, &rbacv1.RoleBinding{})).To(Succeed())
		})
	})

	Context("When a toolset cannot become ready (US3, quickstart S1 negative)", func() {
		// reconcileToolset creates a toolset, reconciles it once, and returns the
		// refreshed object. It registers cleanup for the toolset and any dispatcher
		// resources it might own.
		reconcileToolset := func(name string, spec mcpv1alpha1.MCPToolsetSpec) *mcpv1alpha1.MCPToolset {
			toolset := &mcpv1alpha1.MCPToolset{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: toolsetTestNamespace},
				Spec:       spec,
			}
			Expect(k8sClient.Create(ctx, toolset)).To(Succeed())
			DeferCleanup(func() { deleteToolsetResources(ctx, name) })

			_, err := newToolsetReconciler().Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: name, Namespace: toolsetTestNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			got := &mcpv1alpha1.MCPToolset{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: toolsetTestNamespace}, got)).To(Succeed())
			return got
		}

		expectNoDispatcher := func(toolset string) {
			key := types.NamespacedName{Name: dispatcherName(toolset), Namespace: toolsetTestNamespace}
			err := k8sClient.Get(ctx, key, &appsv1.Deployment{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no dispatcher must be deployed for a not-ready toolset")
		}

		It("reports ServersResolved=False and Ready=False naming a missing server (SC-007)", func() {
			const name = "toolset-missing-server"
			got := reconcileToolset(name, mcpv1alpha1.MCPToolsetSpec{
				Servers:  []string{"ghost-server"},
				Provider: "agentsandbox",
			})

			resolved := meta.FindStatusCondition(got.Status.Conditions, ConditionTypeServersResolved)
			Expect(resolved).NotTo(BeNil())
			Expect(resolved.Status).To(Equal(metav1.ConditionFalse))
			Expect(resolved.Reason).To(Equal(ReasonServersMissing))
			Expect(resolved.Message).To(ContainSubstring("ghost-server"))

			ready := meta.FindStatusCondition(got.Status.Conditions, ConditionTypeReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonServersNotResolved))

			Expect(got.Status.ExecuteEndpoint).To(BeEmpty())
			expectNoDispatcher(name)
		})

		It("reports Ready=False reason UnknownProvider for an unregistered provider", func() {
			const name = "toolset-unknown-provider"
			got := reconcileToolset(name, mcpv1alpha1.MCPToolsetSpec{
				Servers:  []string{"ghost-server"},
				Provider: "does-not-exist",
			})

			ready := meta.FindStatusCondition(got.Status.Conditions, ConditionTypeReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonUnknownProvider))

			Expect(got.Status.ExecuteEndpoint).To(BeEmpty())
			expectNoDispatcher(name)
		})

		It("reports Ready=False reason InsufficientIsolation when the provider is too weak", func() {
			const name = "toolset-weak-isolation"
			// The pod provider guarantees only baseline isolation, so requiring
			// egress isolation must be rejected before any dispatcher is deployed.
			got := reconcileToolset(name, mcpv1alpha1.MCPToolsetSpec{
				Servers:  []string{"ghost-server"},
				Provider: "agentsandbox",
				Policy:   mcpv1alpha1.MCPToolsetPolicy{MinIsolation: mcpv1alpha1.IsolationEgress},
			})

			ready := meta.FindStatusCondition(got.Status.Conditions, ConditionTypeReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ReasonInsufficientIsolation))

			Expect(got.Status.ExecuteEndpoint).To(BeEmpty())
			expectNoDispatcher(name)
		})
	})
})
