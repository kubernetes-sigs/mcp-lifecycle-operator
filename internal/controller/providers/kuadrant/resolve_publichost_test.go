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

package kuadrant

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
)

// TestResolvePublicHostnameSectionNameMatching exercises the sectionName-aware
// matching in resolvePublicHostname using a fake client. Unlike the envtest
// suite, the fake client does not enforce the CRD schema, so it can create an
// MCPGatewayExtension with an empty (whole-gateway) sectionName.
func TestResolvePublicHostnameSectionNameMatching(t *testing.T) {
	ext := func(name, sectionName, publicHost string) *kuadrantapi.MCPGatewayExtension {
		return &kuadrantapi.MCPGatewayExtension{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "gateway-ns"},
			Spec: kuadrantapi.MCPGatewayExtensionSpec{
				PublicHost: publicHost,
				TargetRef: kuadrantapi.TargetReference{
					Kind:        "Gateway",
					Name:        "my-gateway",
					Namespace:   "gateway-ns",
					SectionName: sectionName,
				},
			},
		}
	}

	tests := []struct {
		name        string
		exts        []*kuadrantapi.MCPGatewayExtension
		sectionName string
		wantHost    string
		wantErr     string
	}{
		{
			name:        "empty sectionName targets whole gateway and matches any listener",
			exts:        []*kuadrantapi.MCPGatewayExtension{ext("ext-whole", "", "whole.example.com")},
			sectionName: "mcps",
			wantHost:    "whole.example.com",
		},
		{
			name: "listener-specific extension wins over whole-gateway default",
			exts: []*kuadrantapi.MCPGatewayExtension{
				ext("ext-whole", "", "whole.example.com"),
				ext("ext-mcps", "mcps", "mcps.example.com"),
			},
			sectionName: "mcps",
			wantHost:    "mcps.example.com",
		},
		{
			name: "extension targeting a Gateway in a different group does not match",
			exts: []*kuadrantapi.MCPGatewayExtension{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ext-wrong-group", Namespace: "gateway-ns"},
					Spec: kuadrantapi.MCPGatewayExtensionSpec{
						PublicHost: "wrong.example.com",
						TargetRef: kuadrantapi.TargetReference{
							Group:       "example.com",
							Kind:        "Gateway",
							Name:        "my-gateway",
							Namespace:   "gateway-ns",
							SectionName: "mcps",
						},
					},
				},
			},
			sectionName: "mcps",
			wantErr:     "no MCPGatewayExtension targets gateway",
		},
		{
			name: "non-matching sectionNames yield no match",
			exts: []*kuadrantapi.MCPGatewayExtension{
				ext("ext-a", "listener-a", "a.example.com"),
				ext("ext-b", "listener-b", "b.example.com"),
			},
			sectionName: "mcps",
			wantErr:     "no MCPGatewayExtension targets gateway",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := kuadrantapi.AddToScheme(scheme); err != nil {
				t.Fatalf("adding kuadrant types to scheme: %v", err)
			}
			builder := fake.NewClientBuilder().WithScheme(scheme)
			for _, e := range tc.exts {
				builder = builder.WithObjects(e)
			}
			r := &Reconciler{Client: builder.Build(), Scheme: scheme}

			host, err := r.resolvePublicHostname(context.Background(), "my-gateway", "gateway-ns", tc.sectionName)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (host=%q)", tc.wantErr, host)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if host != tc.wantHost {
				t.Fatalf("expected host %q, got %q", tc.wantHost, host)
			}
		})
	}
}
