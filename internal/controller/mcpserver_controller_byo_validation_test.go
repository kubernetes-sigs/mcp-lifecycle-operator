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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

var _ = Describe("BYO workload fetch errors", func() {
	DescribeTable("preserves API error classification and workload identity", func(kind mcpv1beta1.WorkloadKind) {
		reader := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).Build()
		workload, err := getWorkload(context.Background(), reader, "missing-workload", kind, "default")
		Expect(workload).To(BeNil())
		Expect(errors.IsNotFound(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring(string(kind)))
		Expect(err.Error()).To(ContainSubstring("missing-workload"))
	},
		Entry("Deployment", mcpv1beta1.WorkloadKindDeployment),
		Entry("DaemonSet", mcpv1beta1.WorkloadKindDaemonSet),
		Entry("StatefulSet", mcpv1beta1.WorkloadKindStatefulSet),
	)
	It("rejects an unsupported kind as a configuration error", func() {
		reader := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).Build()
		_, err := getWorkload(context.Background(), reader, "anything", mcpv1beta1.WorkloadKind("CronJob"), "default")
		Expect(err).To(BeAssignableToTypeOf(&ValidationError{}))
		Expect(err.Error()).To(ContainSubstring("unsupported workloadRef kind: CronJob"))
	})
})

var _ = Describe("BYO port admission", func() {
	It("should require a container port for a managed workload even with serviceRef", func() {
		server := newTestBYOMCPServer("managed-byo-service-no-port", nil, &mcpv1beta1.ServiceReference{Name: "external"})
		server.Spec.Config.Port = 0
		err := k8sClient.Create(context.Background(), server)
		Expect(errors.IsInvalid(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("config.port is required"))
	})
})

var _ = Describe("BYO workload mode admission", func() {
	DescribeTable("should preserve workload management mode on updates", func(byo bool) {
		ctx := context.Background()
		name := "immutable-managed-mode"
		var ref *mcpv1beta1.WorkloadReference
		if byo {
			name = "immutable-byo-mode"
			ref = &mcpv1beta1.WorkloadReference{Name: "external", Kind: mcpv1beta1.WorkloadKindDeployment}
		}
		server := newTestBYOMCPServer(name, ref, nil)
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, server)).To(Succeed()) })

		// Ordinary updates and changing an existing BYO reference remain valid.
		server.Labels = map[string]string{"team": "platform"}
		if byo {
			server.Spec.WorkloadRef.Name = "another-workload"
			server.Spec.WorkloadRef.Kind = mcpv1beta1.WorkloadKindStatefulSet
		}
		Expect(k8sClient.Update(ctx, server)).To(Succeed())

		if byo {
			server.Spec.WorkloadRef = nil
			server.Spec.Source = newTestBYOMCPServer("unused", nil, nil).Spec.Source
		} else {
			server.Spec.Source = mcpv1beta1.Source{}
			server.Spec.WorkloadRef = &mcpv1beta1.WorkloadReference{Name: "external", Kind: mcpv1beta1.WorkloadKindDeployment}
		}
		err := k8sClient.Update(ctx, server)
		Expect(errors.IsInvalid(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("workloadRef cannot be added or removed after creation"))
	},
		Entry("managed to BYO", false),
		Entry("BYO to managed", true),
	)
})

var _ = Describe("BYO workload mixed selector", func() {
	It("should require serviceRef rather than broaden a mixed workload selector", func() {
		ctx := context.Background()
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "mixed-selector-workload", Namespace: "default"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels:      map[string]string{"app": "mcp"},
					MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "version", Operator: metav1.LabelSelectorOpIn, Values: []string{"v2", "v3"}}},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "mcp", "version": "v2"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "server", Image: "mcp:latest"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, dep)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, dep)).To(Succeed()) })
		server := newTestBYOMCPServer("mixed-selector-server", &mcpv1beta1.WorkloadReference{Name: dep.Name, Kind: mcpv1beta1.WorkloadKindDeployment}, nil)
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, server)).To(Succeed()) })
		r := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(server)}
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, req.NamespacedName, server)).To(Succeed())
		accepted := meta.FindStatusCondition(server.Status.Conditions, ConditionTypeAccepted)
		Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
		Expect(accepted.Message).To(ContainSubstring("matchExpressions"))
		Expect(accepted.Message).To(ContainSubstring("serviceRef"))
		Expect(errors.IsNotFound(k8sClient.Get(ctx, req.NamespacedName, &corev1.Service{}))).To(BeTrue())

		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "mixed-selector-service", Namespace: "default"}, Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "mcp", "version": "v2"}, Ports: []corev1.ServicePort{{Port: 8080}},
		}}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, svc)).To(Succeed()) })
		server.Spec.ServiceRef = &mcpv1beta1.ServiceReference{Name: svc.Name}
		Expect(k8sClient.Update(ctx, server)).To(Succeed())
		_, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, req.NamespacedName, server)).To(Succeed())
		Expect(meta.FindStatusCondition(server.Status.Conditions, ConditionTypeAccepted).Status).To(Equal(metav1.ConditionTrue))
	})
})
