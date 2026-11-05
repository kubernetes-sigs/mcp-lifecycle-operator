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
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

var _ = Describe("Deployment rollout strategy", func() {
	ctx := context.Background()

	It("applies changes, removals, and drift corrections without repeated updates", func() {
		server := newTestMCPServer("strategy-reconciliation")
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, server)).To(Succeed()) })
		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		deployment := &appsv1.Deployment{}
		DeferCleanup(func() {
			if deployment != nil && deployment.Name != "" {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, deployment))).To(Succeed())
			}
		})
		defaults := appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
			RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxSurge: new(intstr.FromString("25%")), MaxUnavailable: new(intstr.FromString("25%")),
			},
		}
		var originalTemplate *corev1.PodTemplateSpec
		for _, step := range []struct {
			name     string
			strategy *appsv1.DeploymentStrategy
			want     appsv1.DeploymentStrategy
		}{
			{"omitted", nil, defaults},
			{"custom rolling update", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxSurge: new(intstr.FromInt32(2)), MaxUnavailable: new(intstr.FromInt32(0)),
			}}, appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType, RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxSurge: new(intstr.FromInt32(2)), MaxUnavailable: new(intstr.FromInt32(0)),
			}}},
			{"remove one rolling update parameter", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxUnavailable: new(intstr.FromInt32(0)),
			}}, appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType, RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxSurge: new(intstr.FromString("25%")), MaxUnavailable: new(intstr.FromInt32(0)),
			}}},
			{"remove rolling update parameters", &appsv1.DeploymentStrategy{}, defaults},
			{"switch to recreate", &appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
				appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}},
			{"remove strategy", nil, defaults},
		} {
			By(step.name)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(server), server)).To(Succeed())
			server.Spec.Runtime.Strategy = step.strategy.DeepCopy()
			Expect(k8sClient.Update(ctx, server)).To(Succeed())
			strategyBefore := server.Spec.Runtime.Strategy.DeepCopy()
			var err error
			deployment, err = reconciler.reconcileDeployment(ctx, server)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
			Expect(deployment.Spec.Strategy).To(Equal(step.want))
			if originalTemplate == nil {
				originalTemplate = deployment.Spec.Template.DeepCopy()
			}
			Expect(deployment.Spec.Template).To(Equal(*originalTemplate), "strategy-only changes must not change the pod template")
			version := deployment.ResourceVersion
			deployment, err = reconciler.reconcileDeployment(ctx, server)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.ResourceVersion).To(Equal(version), "unchanged strategy must not cause updates")
			Expect(server.Spec.Runtime.Strategy).To(Equal(strategyBefore), "defaulting must not mutate the MCPServer")
		}
		By("correcting external strategy drift on a managed Deployment")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(server), deployment)).To(Succeed())
		deployment.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
		Expect(k8sClient.Update(ctx, deployment)).To(Succeed())
		var err error
		deployment, err = reconciler.reconcileDeployment(ctx, server)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Strategy).To(Equal(defaults))
	})

	DescribeTable("creates a Deployment with the requested strategy", func(strategy appsv1.DeploymentStrategy) {
		server := newTestMCPServer("strategy-create")
		server.Spec.Runtime.Strategy = strategy.DeepCopy()
		Expect(k8sClient.Create(ctx, server)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, server)).To(Succeed()) })
		reconciler := newReconcilerForTest(k8sClient, k8sClient.Scheme())
		deployment, err := reconciler.reconcileDeployment(ctx, server)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, deployment)).To(Succeed()) })
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
		Expect(deployment.Spec.Strategy).To(Equal(strategy))
	},
		Entry("Recreate", appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}),
		Entry("RollingUpdate with percentages", appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
			RollingUpdate: &appsv1.RollingUpdateDeployment{
				MaxSurge: new(intstr.FromString("50%")), MaxUnavailable: new(intstr.FromString("10%")),
			},
		}),
	)

	Context("admission", func() {
		DescribeTable("validates rollout settings", func(strategy *appsv1.DeploymentStrategy, message string) {
			server := newTestMCPServer("strategy-admission")
			server.Spec.Runtime.Strategy = strategy
			err := k8sClient.Create(ctx, server)
			if message != "" {
				Expect(errors.IsInvalid(err)).To(BeTrue(), "%v", err)
				Expect(err.Error()).To(ContainSubstring(message))
			} else {
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Delete(ctx, server)).To(Succeed())
			}
		},
			Entry("empty settings", &appsv1.DeploymentStrategy{}, ""),
			Entry("recreate", &appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}, ""),
			Entry("unknown strategy", &appsv1.DeploymentStrategy{Type: "BlueGreen"}, "strategy.type"),
			Entry("recreate with rolling settings", &appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType, RollingUpdate: &appsv1.RollingUpdateDeployment{}}, "rollingUpdate must not"),
			Entry("negative surge", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxSurge: new(intstr.FromInt32(-1))}}, "maxSurge must"),
			Entry("negative unavailable", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable: new(intstr.FromInt32(-1))}}, "maxUnavailable must"),
			Entry("malformed percentage", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxSurge: new(intstr.FromString("1"))}}, "maxSurge must"),
			Entry("unavailable over 100 percent", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable: new(intstr.FromString("101%"))}}, "maxUnavailable must"),
			Entry("surge over 100 percent", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxSurge: new(intstr.FromString("150%"))}}, ""),
			Entry("both integer zero", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxSurge: new(intstr.FromInt32(0)), MaxUnavailable: new(intstr.FromInt32(0))}}, "must not both be zero"),
			Entry("mixed zero", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxSurge: new(intstr.FromString("0%")), MaxUnavailable: new(intstr.FromInt32(0))}}, "must not both be zero"),
			Entry("percentage zero", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxSurge: new(intstr.FromString("0%")), MaxUnavailable: new(intstr.FromString("0%"))}}, "must not both be zero"),
			Entry("one zero with default", &appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxSurge: new(intstr.FromInt32(0))}}, ""),
		)
	})

	DescribeTable("rejects strategy for BYO workloads", func(kind mcpv1beta1.WorkloadKind) {
		server := newTestBYOMCPServer("strategy-byo", &mcpv1beta1.WorkloadReference{Name: "external", Kind: kind}, nil)
		server.Spec.Runtime.Strategy = &appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
		err := k8sClient.Create(ctx, server)
		Expect(errors.IsInvalid(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("runtime.strategy must not be set when workloadRef is set"))
	},
		Entry("Deployment", mcpv1beta1.WorkloadKindDeployment),
		Entry("StatefulSet", mcpv1beta1.WorkloadKindStatefulSet),
		Entry("DaemonSet", mcpv1beta1.WorkloadKindDaemonSet),
	)
})
