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

var _ = Describe("determineReadyCondition", func() {
	var generation int64 = 1
	var acceptedCondition metav1.Condition

	BeforeEach(func() {
		// Default to valid configuration
		acceptedCondition = metav1.Condition{
			Type:   ConditionTypeAccepted,
			Status: metav1.ConditionTrue,
			Reason: ReasonValid,
		}
	})

	It("should return Initializing when deployment has no conditions and no ready replicas", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
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
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonAvailable))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	})

	It("should return DeploymentUnavailable when deployment has replica failure", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:    appsv1.DeploymentReplicaFailure,
						Status:  corev1.ConditionTrue,
						Message: "replica failed",
					},
				},
			},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
		Expect(condition.Message).To(ContainSubstring("replica failed"))
	})

	It("should return DeploymentUnavailable when deployment is progressing", func() {
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentProgressing,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonDeploymentUnavailable))
	})

	It("should return ConfigurationInvalid when configuration is not accepted", func() {
		invalidAcceptedCondition := metav1.Condition{
			Type:   ConditionTypeAccepted,
			Status: metav1.ConditionFalse,
			Reason: ReasonInvalid,
		}
		deployment := &appsv1.Deployment{}
		condition := determineReadyCondition(deployment, invalidAcceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonConfigurationInvalid))
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("should return Ready=True with ScaledToZero reason when deployment is scaled to 0 replicas", func() {
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](0),
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 0,
			},
		}
		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))
		Expect(condition.Reason).To(Equal(ReasonScaledToZero))
		// Ready=True following Kubernetes Deployment semantics: replicas=0 is a valid desired state
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Message).To(ContainSubstring("scaled to 0 replicas"))
	})

	It("should preserve LastTransitionTime when condition status hasn't changed", func() {
		// Create an existing condition with a specific timestamp
		pastTime := metav1.NewTime(metav1.Now().Add(-5 * time.Minute))
		existingConditions := []metav1.Condition{
			{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonDeploymentUnavailable,
				LastTransitionTime: pastTime,
			},
		}

		// Create a deployment that would result in the same condition
		deployment := &appsv1.Deployment{
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentProgressing,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		condition := determineReadyCondition(deployment, acceptedCondition, generation, existingConditions)

		// The LastTransitionTime should be preserved from the existing condition
		Expect(condition.LastTransitionTime).To(Equal(pastTime))
	})

	It("should update LastTransitionTime when condition status changes", func() {
		// Create an existing condition with Status=False
		pastTime := metav1.NewTime(metav1.Now().Add(-5 * time.Minute))
		existingConditions := []metav1.Condition{
			{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonInitializing,
				LastTransitionTime: pastTime,
			},
		}

		// Create a deployment that would result in Status=True (different status)
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		condition := determineReadyCondition(deployment, acceptedCondition, generation, existingConditions)

		// The LastTransitionTime should be NEW (not the past time)
		Expect(condition.LastTransitionTime).NotTo(Equal(pastTime))
	})

	It("should handle nil replicas gracefully when deployment is available", func() {
		// Create a deployment with nil replicas (tests the ptr.Deref fix)
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: nil, // nil replicas should default to 1 in the message
			},
			Status: appsv1.DeploymentStatus{
				ReadyReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		condition := determineReadyCondition(deployment, acceptedCondition, generation, make([]metav1.Condition, 0))

		// Should succeed without panicking
		Expect(condition.Reason).To(Equal(ReasonAvailable))
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		// Message should use default value of 1 for nil replicas
		Expect(condition.Message).To(ContainSubstring("1 of 1 instances healthy"))
	})
})

var _ = Describe("analyzeDeploymentFailure", func() {
	It("should identify ImagePullBackOff errors", func() {
		message := "Back-off pulling image \"nonexistent:latest\": ImagePullBackOff"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("ImagePullBackOff"))
	})

	It("should identify ErrImagePull errors", func() {
		message := "Failed to pull image: ErrImagePull"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("ImagePullBackOff"))
	})

	It("should identify OOMKilled errors", func() {
		message := "Container was OOMKilled"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("OOMKilled"))
	})

	It("should identify CrashLoopBackOff errors", func() {
		message := "Back-off restarting failed container: CrashLoopBackOff"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("CrashLoopBackOff"))
	})

	It("should identify CreateContainerConfigError errors", func() {
		message := "Error: container has runAsNonRoot and image will run as root: CreateContainerConfigError"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("CreateContainerConfigError"))
	})

	It("should identify probe failures", func() {
		message := "Liveness probe failed: HTTP probe failed"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("Probe failed"))
	})

	It("should handle empty message", func() {
		message := ""
		result := analyzeDeploymentFailure(message)
		Expect(result).To(Equal("No healthy instances available"))
	})

	It("should handle generic failures", func() {
		message := "Some unknown error occurred"
		result := analyzeDeploymentFailure(message)
		Expect(result).To(ContainSubstring("No healthy instances"))
		Expect(result).To(ContainSubstring("Some unknown error occurred"))
	})
})
