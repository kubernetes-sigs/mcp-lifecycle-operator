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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("extractDeploymentState", func() {
	It("should extract available condition", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
				},
			},
		}
		state := extractDeploymentState(deployment)
		Expect(state.available).To(BeTrue())
		Expect(state.progressing).To(BeFalse())
		Expect(state.replicaFailure).To(BeFalse())
	})

	It("should extract progressing condition", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
				},
			},
		}
		state := extractDeploymentState(deployment)
		Expect(state.progressing).To(BeTrue())
	})

	It("should capture message from failed progressing condition", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Message: "timed out"},
				},
			},
		}
		state := extractDeploymentState(deployment)
		Expect(state.progressing).To(BeFalse())
		Expect(state.message).To(Equal("timed out"))
	})

	It("should extract replica failure with message", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionTrue, Message: "quota exceeded"},
				},
			},
		}
		state := extractDeploymentState(deployment)
		Expect(state.replicaFailure).To(BeTrue())
		Expect(state.message).To(Equal("quota exceeded"))
	})

	It("should prefer ReplicaFailure message over Progressing message", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Message: "timed out"},
					{Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionTrue, Message: "quota exceeded"},
				},
			},
		}
		state := extractDeploymentState(deployment)
		Expect(state.replicaFailure).To(BeTrue())
		Expect(state.message).To(Equal("quota exceeded"))
	})

	It("should keep Progressing message when ReplicaFailure message is empty", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Message: "timed out"},
					{Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionTrue, Message: ""},
				},
			},
		}
		state := extractDeploymentState(deployment)
		Expect(state.replicaFailure).To(BeTrue())
		Expect(state.message).To(Equal("timed out"))
	})

	It("should return zero state for empty conditions", func() {
		deployment := &appsv1.Deployment{}
		state := extractDeploymentState(deployment)
		Expect(state.available).To(BeFalse())
		Expect(state.progressing).To(BeFalse())
		Expect(state.replicaFailure).To(BeFalse())
		Expect(state.message).To(BeEmpty())
	})
})

var _ = Describe("analyzePodFailures", func() {
	It("should detect ImagePullBackOff", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  WaitingReasonImagePullBackOff,
							Message: "Back-off pulling image \"ghcr.io/user/image:tag\"",
						},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(ContainSubstring("Image pull failed"))
		Expect(result).To(ContainSubstring("ghcr.io/user/image:tag"))
		Expect(result).To(ContainSubstring("my-pod-abc123"))
	})

	It("should detect ErrImagePull", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  WaitingReasonErrImagePull,
							Message: "manifest unknown",
						},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(ContainSubstring("Image pull failed"))
		Expect(result).To(ContainSubstring("my-pod-abc123"))
	})

	It("should detect CrashLoopBackOff with exit code from last termination", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					RestartCount: 5,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: WaitingReasonCrashLoopBackOff,
						},
					},
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 1,
						},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(ContainSubstring("Container crashing"))
		Expect(result).To(ContainSubstring("exit code 1"))
		Expect(result).To(ContainSubstring("restarts: 5"))
		Expect(result).To(ContainSubstring("my-pod-abc123"))
	})

	It("should detect CrashLoopBackOff without last termination state", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					RestartCount: 3,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: WaitingReasonCrashLoopBackOff,
						},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(ContainSubstring("Container crashing"))
		Expect(result).To(ContainSubstring("restarts: 3"))
		Expect(result).To(ContainSubstring("my-pod-abc123"))
	})

	It("should detect CreateContainerConfigError", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  WaitingReasonCreateContainerConfigError,
							Message: "configmap \"my-config\" not found",
						},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(ContainSubstring("Container config error"))
		Expect(result).To(ContainSubstring("my-config"))
		Expect(result).To(ContainSubstring("my-pod-abc123"))
	})

	It("should detect OOMKilled", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					RestartCount: 2,
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   TerminatedReasonOOMKilled,
							ExitCode: 137,
						},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(ContainSubstring("OOMKilled"))
		Expect(result).To(ContainSubstring("exit code 137"))
		Expect(result).To(ContainSubstring("restarts: 2"))
		Expect(result).To(ContainSubstring("my-pod-abc123"))
	})

	It("should detect init container failures", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				InitContainerStatuses: []corev1.ContainerStatus{{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  WaitingReasonImagePullBackOff,
							Message: "Back-off pulling image \"init-image:latest\"",
						},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(ContainSubstring("Image pull failed"))
		Expect(result).To(ContainSubstring("init-image:latest"))
	})

	It("should detect probe failure (running but not ready)", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					Ready:        false,
					RestartCount: 4,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(ContainSubstring("not passing health checks"))
		Expect(result).To(ContainSubstring("restarts: 4"))
		Expect(result).To(ContainSubstring("my-pod-abc123"))
	})

	It("should not flag running container with zero restarts as probe failure", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					Ready:        false,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(BeEmpty())
	})

	It("should return empty string when no failures detected", func() {
		pods := []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pod-abc123"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					Ready: true,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				}},
			},
		}}
		result := analyzePodFailures(pods)
		Expect(result).To(BeEmpty())
	})

	It("should return empty string for empty pod list", func() {
		result := analyzePodFailures(nil)
		Expect(result).To(BeEmpty())
	})
})

var _ = Describe("newReadyCondition", func() {
	It("should preserve LastTransitionTime when status hasn't changed", func() {
		pastTime := metav1.NewTime(metav1.Now().Add(-5 * time.Minute))
		existing := []metav1.Condition{{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonDeploymentUnavailable,
			LastTransitionTime: pastTime,
		}}
		condition := newReadyCondition(metav1.ConditionFalse, ReasonDeploymentUnavailable,
			"some message", 1, existing)
		Expect(condition.LastTransitionTime).To(Equal(pastTime))
	})

	It("should update LastTransitionTime when status changes", func() {
		pastTime := metav1.NewTime(metav1.Now().Add(-5 * time.Minute))
		existing := []metav1.Condition{{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: pastTime,
		}}
		condition := newReadyCondition(metav1.ConditionTrue, ReasonAvailable,
			"ready", 1, existing)
		Expect(condition.LastTransitionTime).NotTo(Equal(pastTime))
	})

	It("should set the correct fields", func() {
		condition := newReadyCondition(metav1.ConditionTrue, ReasonAvailable,
			"all good", 42, nil)
		Expect(condition.Type).To(Equal(ConditionTypeReady))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(ReasonAvailable))
		Expect(condition.Message).To(Equal("all good"))
		Expect(condition.ObservedGeneration).To(Equal(int64(42)))
	})
})

var _ = Describe("reconcileReadyCondition", func() {
	var generation int64 = 1
	var acceptedCondition metav1.Condition
	var reconciler *MCPServerReconciler

	BeforeEach(func() {
		acceptedCondition = metav1.Condition{
			Type:   ConditionTypeAccepted,
			Status: metav1.ConditionTrue,
			Reason: ReasonValid,
		}
		reconciler = &MCPServerReconciler{Client: k8sClient}
	})

	It("should return Initializing when deployment has no conditions and no ready replicas", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Reason).To(Equal(ReasonInitializing))
		Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
	})

	It("should return Available when deployment is available with ready replicas", func() {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
				},
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Reason).To(Equal(ReasonAvailable))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})

	It("should return ConfigurationInvalid when configuration is not accepted", func() {
		invalidAccepted := metav1.Condition{
			Type:   ConditionTypeAccepted,
			Status: metav1.ConditionFalse,
			Reason: ReasonInvalid,
		}
		deployment := &appsv1.Deployment{}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, invalidAccepted, generation, nil)
		Expect(condition.Reason).To(Equal(ReasonConfigurationInvalid))
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("should return Ready=True with ScaledToZero when deployment is scaled to 0", func() {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](0),
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Reason).To(Equal(ReasonScaledToZero))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Message).To(ContainSubstring("scaled to 0 replicas"))
	})

	It("should return DeploymentUnavailable when deployment is progressing", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
				},
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
	})

	It("should surface pod failure when progressing with zero ready replicas and ImagePullBackOff", func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-prog-imgpull-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		labels := map[string]string{"test": "progressing-imgpull"}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "imgpull-pod",
				Namespace: ns.Name,
				Labels:    labels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "ghcr.io/bad/image:v1"}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name:  "c",
			Image: "ghcr.io/bad/image:v1",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  WaitingReasonImagePullBackOff,
					Message: "Back-off pulling image \"ghcr.io/bad/image:v1\"",
				},
			},
		}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: labels},
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 0,
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
				},
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(condition.Message).To(ContainSubstring("Image pull failed"))
		Expect(condition.Message).To(ContainSubstring("ghcr.io/bad/image:v1"))
	})

	It("should surface pod failure when progressing with zero ready replicas and CrashLoopBackOff", func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-prog-crash-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		labels := map[string]string{"test": "progressing-crash"}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "crash-pod",
				Namespace: ns.Name,
				Labels:    labels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "myimage:latest"}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name:         "c",
			RestartCount: 5,
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason: WaitingReasonCrashLoopBackOff,
				},
			},
			LastTerminationState: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{ExitCode: 1},
			},
		}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: labels},
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 0,
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
				},
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(condition.Message).To(ContainSubstring("Container crashing"))
		Expect(condition.Message).To(ContainSubstring("exit code 1"))
		Expect(condition.Message).To(ContainSubstring("restarts: 5"))
	})

	It("should return waiting message when progressing with zero ready replicas but no pod failures", func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-prog-nofail-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		labels := map[string]string{"test": "progressing-nofail"}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "healthy-pod",
				Namespace: ns.Name,
				Labels:    labels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "myimage:latest"}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name:  "c",
			Ready: false,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{},
			},
		}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: labels},
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 0,
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue},
				},
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(condition.Message).To(ContainSubstring("Waiting for instances to become healthy"))
	})

	It("should return DeploymentUnavailable with fallback message on replica failure", func() {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "no-such-pods"},
				},
			},
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:    appsv1.DeploymentReplicaFailure,
						Status:  corev1.ConditionTrue,
						Message: "quota exceeded",
					},
				},
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(condition.Message).To(ContainSubstring("quota exceeded"))
	})

	It("should return DeploymentUnavailable when deployment spec is not yet observed", func() {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 3,
			},
			Status: appsv1.DeploymentStatus{
				ObservedGeneration: 2,
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Message).To(ContainSubstring("processing spec update"))
	})

	It("should return DeploymentUnavailable when not progressing and not available", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:    appsv1.DeploymentProgressing,
						Status:  corev1.ConditionFalse,
						Message: "deadline exceeded",
					},
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("should handle nil replicas gracefully when deployment is available", func() {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: nil,
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
				},
			},
		}
		condition := reconciler.reconcileReadyCondition(ctx, deployment, acceptedCondition, generation, nil)
		Expect(condition.Reason).To(Equal(ReasonAvailable))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Message).To(ContainSubstring("1 of 1 instances healthy"))
	})
})
