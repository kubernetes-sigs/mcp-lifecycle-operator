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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// Field ownership coexistence tests.
//
// Another controller may legitimately co-manage the Deployment this operator
// generates, adding workload labels and pod template annotations of its own to
// trigger sidecar injection. This operator reconciles the fields it generates
// and leaves everything else alone: it must neither delete foreign metadata nor
// treat the presence of foreign metadata as drift that needs correcting.
var _ = Describe("MCPServer Controller - foreign field ownership", func() {
	const (
		foreignTypeLabel       = "kagenti.io/type"
		foreignConfigHashAnnot = "kagenti.io/config-hash"
		foreignMTLSModeAnnot   = "kagenti.io/mtls-mode"
	)

	ctx := context.Background()

	get := func(name string) *appsv1.Deployment {
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx,
			types.NamespacedName{Name: name, Namespace: "default"}, deployment)).To(Succeed())
		return deployment
	}

	getMCPServer := func(name string) *mcpv1alpha1.MCPServer {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx,
			types.NamespacedName{Name: name, Namespace: "default"}, mcpServer)).To(Succeed())
		return mcpServer
	}

	// writeForeignMetadata simulates the peer controller: a read-modify-write
	// that only sets its own keys, exactly as the AgentRuntime controller does.
	writeForeignMetadata := func(name string) {
		deployment := get(name)
		if deployment.Labels == nil {
			deployment.Labels = map[string]string{}
		}
		deployment.Labels[foreignTypeLabel] = "tool"
		if deployment.Spec.Template.Labels == nil {
			deployment.Spec.Template.Labels = map[string]string{}
		}
		deployment.Spec.Template.Labels[foreignTypeLabel] = "tool"
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Spec.Template.Annotations[foreignConfigHashAnnot] = "abc123def456"
		deployment.Spec.Template.Annotations[foreignMTLSModeAnnot] = "disabled"
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())
	}

	newReconciler := func() *MCPServerReconciler {
		return &MCPServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), APIReader: k8sClient}
	}

	createServer := func(name string, mutate func(*mcpv1alpha1.MCPServer)) *mcpv1alpha1.MCPServer {
		resource := newTestMCPServer(name)
		if mutate != nil {
			mutate(resource)
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			mcpServer := &mcpv1alpha1.MCPServer{}
			key := types.NamespacedName{Name: name, Namespace: "default"}
			if err := k8sClient.Get(ctx, key, mcpServer); err == nil {
				Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
			}
		})
		return getMCPServer(name)
	}

	setup := func(name string) (*mcpv1alpha1.MCPServer, *MCPServerReconciler) {
		mcpServer := createServer(name, nil)
		reconciler := newReconciler()
		_, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		return mcpServer, reconciler
	}

	It("preserves foreign labels and annotations written by another controller", func() {
		const name = "test-foreign-preserved"
		mcpServer, reconciler := setup(name)

		By("a peer controller adding its own workload and pod template metadata")
		writeForeignMetadata(name)

		By("reconciling again")
		_, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		deployment := get(name)
		Expect(deployment.Labels).To(HaveKeyWithValue(foreignTypeLabel, "tool"))
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(foreignTypeLabel, "tool"))
		Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(foreignConfigHashAnnot, "abc123def456"))
		Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(foreignMTLSModeAnnot, "disabled"))

		By("this operator's own fields still being present")
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(LabelKeyApp, ManagedWorkloadName))
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(LabelKeyMCPServer, name))
	})

	It("does not churn the deployment when foreign metadata is present", func() {
		const name = "test-foreign-no-churn"
		mcpServer, reconciler := setup(name)

		By("a peer controller adding its own metadata")
		writeForeignMetadata(name)

		settled := get(name)
		generation, resourceVersion := settled.Generation, settled.ResourceVersion

		By("reconciling repeatedly with no MCPServer change")
		for i := range 3 {
			_, err := reconciler.reconcileDeployment(ctx, mcpServer)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reconcile %d failed", i))
		}

		current := get(name)
		Expect(current.Generation).To(Equal(generation),
			"reconciling must be a no-op when only foreign metadata differs; "+
				"a bumped generation means the operator and the peer are fighting")
		Expect(current.ResourceVersion).To(Equal(resourceVersion),
			"the operator must not write at all in steady state")
	})

	It("still propagates MCPServer changes while preserving foreign metadata", func() {
		const name = "test-foreign-with-update"
		_, reconciler := setup(name)

		writeForeignMetadata(name)

		By("changing a field this operator owns")
		mcpServer := getMCPServer(name)
		mcpServer.Spec.Source.ContainerImage.Ref = "docker.io/library/updated-image:v2"
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		_, err := reconciler.reconcileDeployment(ctx, getMCPServer(name))
		Expect(err).NotTo(HaveOccurred())

		deployment := get(name)
		Expect(deployment.Spec.Template.Spec.Containers).NotTo(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("docker.io/library/updated-image:v2"))

		By("the peer controller's fields still surviving")
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(foreignTypeLabel, "tool"))
		Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(foreignConfigHashAnnot, "abc123def456"))
		Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(foreignMTLSModeAnnot, "disabled"))
	})

	It("restores its own managed labels when another actor removes them", func() {
		const name = "test-managed-labels-restored"
		mcpServer, reconciler := setup(name)

		// Only the app label is stripped: the mcp-server label is part of
		// spec.selector, and the API server rejects a template that no longer
		// matches its own selector.
		By("another actor stripping a label this operator owns")
		deployment := get(name)
		delete(deployment.Spec.Template.Labels, LabelKeyApp)
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())
		Expect(get(name).Spec.Template.Labels).NotTo(HaveKey(LabelKeyApp))

		_, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())

		Expect(get(name).Spec.Template.Labels).To(HaveKeyWithValue(LabelKeyApp, ManagedWorkloadName))
	})

	It("removes an extraLabel the user removed, leaving the peer's label alone", func() {
		const name = "test-extra-label-removal"
		mcpServer := createServer(name, func(m *mcpv1alpha1.MCPServer) {
			m.Spec.ExtraLabels = map[string]string{"team": "platform"}
		})
		reconciler := newReconciler()
		_, err := reconciler.reconcileDeployment(ctx, mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(get(name).Spec.Template.Labels).To(HaveKeyWithValue("team", "platform"))

		By("a peer controller adding its own metadata alongside it")
		writeForeignMetadata(name)

		By("removing the extra label from the MCPServer")
		mcpServer = getMCPServer(name)
		mcpServer.Spec.ExtraLabels = nil
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		_, err = reconciler.reconcileDeployment(ctx, getMCPServer(name))
		Expect(err).NotTo(HaveOccurred())

		deployment := get(name)
		Expect(deployment.Spec.Template.Labels).NotTo(HaveKey("team"))
		Expect(deployment.Labels).NotTo(HaveKey("team"))
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(foreignTypeLabel, "tool"))
	})

	// Deployments already in the field were created by an earlier release. This
	// operator keeps writing them with Update, so there is no ownership handover
	// to get wrong - but the removal path is worth pinning down all the same.
	It("removes fields on a Deployment created before the upgrade", func() {
		const name = "test-preexisting-removal"
		mcpServer := createServer(name, func(m *mcpv1alpha1.MCPServer) {
			m.Spec.ExtraLabels = map[string]string{"team": "platform"}
			m.Spec.Runtime = mcpv1alpha1.RuntimeConfig{
				Security: mcpv1alpha1.SecurityConfig{ServiceAccountName: "my-sa"},
			}
		})
		reconciler := newReconciler()

		By("an earlier release having created the Deployment")
		legacy, err := reconciler.createDeployment(mcpServer)
		Expect(err).NotTo(HaveOccurred())
		Expect(reconciler.applyConfigHash(ctx, mcpServer, legacy)).To(Succeed())
		Expect(applyCustomDeploymentMetadata(mcpServer, legacy)).To(Succeed())
		Expect(controllerutil.SetControllerReference(mcpServer, legacy, k8sClient.Scheme())).To(Succeed())
		Expect(k8sClient.Create(ctx, legacy)).To(Succeed())

		By("a peer controller adding its own metadata")
		writeForeignMetadata(name)

		By("the user clearing both settings")
		mcpServer = getMCPServer(name)
		mcpServer.Spec.ExtraLabels = nil
		mcpServer.Spec.Runtime = mcpv1alpha1.RuntimeConfig{}
		Expect(k8sClient.Update(ctx, mcpServer)).To(Succeed())

		_, err = reconciler.reconcileDeployment(ctx, getMCPServer(name))
		Expect(err).NotTo(HaveOccurred())

		deployment := get(name)
		Expect(deployment.Spec.Template.Labels).NotTo(HaveKey("team"))
		Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(BeEmpty())
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(foreignTypeLabel, "tool"))
	})
})
