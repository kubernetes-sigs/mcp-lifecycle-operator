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

package providers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

func TestMatchesProvider(t *testing.T) {
	pred := MatchesProvider("httproute")

	tests := []struct {
		name string
		obj  event.CreateEvent
		want bool
	}{
		{
			name: "matching provider",
			obj: event.CreateEvent{Object: &mcpv1alpha1.MCPGatewayBinding{
				Spec: mcpv1alpha1.MCPGatewayBindingSpec{Provider: "httproute"},
			}},
			want: true,
		},
		{
			name: "non-matching provider",
			obj: event.CreateEvent{Object: &mcpv1alpha1.MCPGatewayBinding{
				Spec: mcpv1alpha1.MCPGatewayBindingSpec{Provider: "custom-vendor"},
			}},
			want: false,
		},
		{
			name: "non-binding object",
			obj: event.CreateEvent{Object: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pred.Create(tt.obj); got != tt.want {
				t.Errorf("MatchesProvider().Create() = %v, want %v", got, tt.want)
			}
		})
	}
}
