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
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPolicyEnforcedCondition(t *testing.T) {
	tests := []struct {
		name           string
		webhookEnabled bool
		policyActive   bool
		wantStatus     metav1.ConditionStatus
		wantReason     string
		wantMsgSubstr  string
	}{
		{
			name:           "webhook disabled surfaces unenforced policy",
			webhookEnabled: false,
			policyActive:   false,
			wantStatus:     metav1.ConditionFalse,
			wantReason:     ReasonWebhookDisabled,
			wantMsgSubstr:  "NOT",
		},
		{
			name:           "webhook enabled without rules is not enforced",
			webhookEnabled: true,
			policyActive:   false,
			wantStatus:     metav1.ConditionFalse,
			wantReason:     ReasonNoPolicyConfigured,
			wantMsgSubstr:  "without validation",
		},
		{
			name:           "webhook enabled with rules reports policy enforced",
			webhookEnabled: true,
			policyActive:   true,
			wantStatus:     metav1.ConditionTrue,
			wantReason:     ReasonWebhookEnabled,
			wantMsgSubstr:  "enforced",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &MCPServerReconciler{WebhookEnabled: tt.webhookEnabled, AdmissionPolicyActive: tt.policyActive}

			c := r.policyEnforcedCondition(3, nil)

			if c.Type != ConditionTypePolicyEnforced {
				t.Errorf("Type = %q, want %q", c.Type, ConditionTypePolicyEnforced)
			}
			if c.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", c.Status, tt.wantStatus)
			}
			if c.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", c.Reason, tt.wantReason)
			}
			if c.ObservedGeneration != 3 {
				t.Errorf("ObservedGeneration = %d, want 3", c.ObservedGeneration)
			}
			if !strings.Contains(c.Message, tt.wantMsgSubstr) {
				t.Errorf("Message = %q, want it to contain %q", c.Message, tt.wantMsgSubstr)
			}
		})
	}
}

// TestPolicyEnforcedConditionPreservesTransitionTime verifies the condition reuses
// an existing LastTransitionTime when the status is unchanged, matching how the
// other informational conditions behave.
func TestPolicyEnforcedConditionPreservesTransitionTime(t *testing.T) {
	r := &MCPServerReconciler{WebhookEnabled: false}
	earlier := metav1.NewTime(time.Now().Add(-time.Hour))
	existing := []metav1.Condition{
		{
			Type:               ConditionTypePolicyEnforced,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonWebhookDisabled,
			LastTransitionTime: earlier,
		},
	}

	c := r.policyEnforcedCondition(1, existing)
	if !c.LastTransitionTime.Equal(&earlier) {
		t.Errorf("LastTransitionTime = %v, want preserved %v", c.LastTransitionTime, earlier)
	}

	// A status change (webhook now enabled with active rules) must reset the timestamp.
	r.WebhookEnabled = true
	r.AdmissionPolicyActive = true
	c = r.policyEnforcedCondition(1, existing)
	if c.LastTransitionTime.Equal(&earlier) {
		t.Errorf("LastTransitionTime should reset on status change, got preserved %v", earlier)
	}
}
