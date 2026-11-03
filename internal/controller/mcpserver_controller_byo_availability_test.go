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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

var _ = Describe("BYO availability from workload status", func() {
	daemonSet := func(status appsv1.DaemonSetStatus) client.Object {
		return &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "status-ds", Namespace: "default", Generation: 2}, Status: status}
	}
	statefulSet := func(replicas int32, status appsv1.StatefulSetStatus) client.Object {
		return &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "status-sts", Namespace: "default", Generation: 2}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}, Status: status}
	}
	deployment := func(replicas int32, observed int64) client.Object {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "status-deployment", Namespace: "default", Generation: 2},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			Status: appsv1.DeploymentStatus{
				ObservedGeneration: observed, ReadyReplicas: 1, AvailableReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}},
			},
		}
	}
	DescribeTable("uses availability rather than requiring a completed rollout", func(obj client.Object, kind mcpv1beta1.WorkloadKind, expected metav1.ConditionStatus, reason, message string) {
		ctx := context.Background()
		reader := fake.NewClientBuilder().WithScheme(k8sClient.Scheme()).WithObjects(obj).Build()
		workload, err := getWorkload(ctx, reader, obj.GetName(), kind, obj.GetNamespace())
		Expect(err).NotTo(HaveOccurred())
		server := newTestBYOMCPServer("status-server", &mcpv1beta1.WorkloadReference{Name: obj.GetName(), Kind: kind}, nil)
		r := newReconcilerForTest(reader, k8sClient.Scheme())
		observation, err := r.observeWorkload(ctx, server, workload, metav1.Condition{Status: metav1.ConditionTrue})
		Expect(err).NotTo(HaveOccurred())
		condition := observation.availability
		Expect(condition.Status).To(Equal(expected))
		Expect(condition.Reason).To(Equal(reason))
		Expect(condition.Message).To(ContainSubstring(message))
	},
		Entry("DaemonSet with partial availability and incomplete scheduling", daemonSet(appsv1.DaemonSetStatus{
			ObservedGeneration: 2, DesiredNumberScheduled: 3, CurrentNumberScheduled: 2, NumberReady: 2, NumberAvailable: 1, NumberUnavailable: 2,
		}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionTrue, ReasonAvailable, "2 unavailable; 2 of 3 instances scheduled"),
		Entry("DaemonSet ready pods have not met minReadySeconds", daemonSet(appsv1.DaemonSetStatus{
			ObservedGeneration: 2, DesiredNumberScheduled: 3, CurrentNumberScheduled: 3, NumberReady: 3, NumberAvailable: 0, NumberUnavailable: 3,
		}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "0 of 3 instances available (3 ready)"),
		Entry("DaemonSet with no status", daemonSet(appsv1.DaemonSetStatus{}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionUnknown, ReasonInitializing, "report status"),
		Entry("observed DaemonSet with no matching nodes", daemonSet(appsv1.DaemonSetStatus{ObservedGeneration: 2}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "no nodes matching"),
		Entry("DaemonSet status predates spec update", daemonSet(appsv1.DaemonSetStatus{
			ObservedGeneration: 1, DesiredNumberScheduled: 3, NumberReady: 3, NumberAvailable: 3,
		}), mcpv1beta1.WorkloadKindDaemonSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "processing spec update"),
		Entry("StatefulSet with partial availability during rollout", statefulSet(3, appsv1.StatefulSetStatus{
			ObservedGeneration: 2, ReadyReplicas: 2, AvailableReplicas: 1, UpdatedReplicas: 2,
		}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionTrue, ReasonAvailable, "rolling update in progress"),
		Entry("StatefulSet ready pods have not met minReadySeconds", statefulSet(3, appsv1.StatefulSetStatus{
			ObservedGeneration: 2, ReadyReplicas: 3, AvailableReplicas: 0, UpdatedReplicas: 3,
		}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "0 of 3 instances available (3 ready)"),
		Entry("StatefulSet scaled to zero with lingering replicas", statefulSet(0, appsv1.StatefulSetStatus{
			ObservedGeneration: 1, ReadyReplicas: 3, AvailableReplicas: 3,
		}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionTrue, ReasonScaledToZero, "scaled to zero"),
		Entry("StatefulSet with no status", statefulSet(3, appsv1.StatefulSetStatus{}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionUnknown, ReasonInitializing, "report status"),
		Entry("StatefulSet status predates spec update", statefulSet(3, appsv1.StatefulSetStatus{
			ObservedGeneration: 1, ReadyReplicas: 3, AvailableReplicas: 3,
		}), mcpv1beta1.WorkloadKindStatefulSet, metav1.ConditionFalse, ReasonWorkloadUnavailable, "processing spec update"),
		Entry("Deployment uses managed availability semantics for partial availability", deployment(3, 2), mcpv1beta1.WorkloadKindDeployment, metav1.ConditionTrue, ReasonAvailable, "1 of 3 instances healthy"),
		Entry("Deployment status predates spec update", deployment(3, 1), mcpv1beta1.WorkloadKindDeployment, metav1.ConditionFalse, ReasonWorkloadUnavailable, "processing spec update"),
		Entry("Deployment scaled to zero with lingering replicas", deployment(0, 2), mcpv1beta1.WorkloadKindDeployment, metav1.ConditionTrue, ReasonScaledToZero, "scaled to 0"),
	)
})
