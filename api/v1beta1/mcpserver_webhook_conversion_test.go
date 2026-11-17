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

package v1beta1_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	apix "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	webhookconversion "sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	v1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1"
	v1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	v1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func TestMCPServerWebhookReturnsDestinationVersion(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{v1.AddToScheme, v1alpha1.AddToScheme, v1beta1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	handler := webhookconversion.NewWebhookHandler(scheme, webhookconversion.NewRegistry())

	for _, tc := range []struct{ source, target string }{
		{v1beta1.GroupVersion.String(), v1.GroupVersion.String()},
		{v1.GroupVersion.String(), v1beta1.GroupVersion.String()},
		{v1alpha1.GroupVersion.String(), v1.GroupVersion.String()},
		{v1.GroupVersion.String(), v1alpha1.GroupVersion.String()},
		{v1beta1.GroupVersion.String(), v1alpha1.GroupVersion.String()},
	} {
		t.Run(tc.source+"_to_"+tc.target, func(t *testing.T) {
			obj, err := json.Marshal(map[string]any{
				"apiVersion": tc.source, "kind": "MCPServer",
				"metadata": map[string]string{"name": "example", "namespace": "default"},
			})
			if err != nil {
				t.Fatal(err)
			}
			review := apix.ConversionReview{Request: &apix.ConversionRequest{
				UID: types.UID("test"), DesiredAPIVersion: tc.target,
				Objects: []runtime.RawExtension{{Raw: obj}},
			}}
			body, err := json.Marshal(review)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("POST", "/convert", bytes.NewReader(body)))
			if response.Code != 200 {
				t.Fatalf("webhook returned HTTP %d: %s", response.Code, response.Body.String())
			}
			if err := json.Unmarshal(response.Body.Bytes(), &review); err != nil {
				t.Fatal(err)
			}
			if review.Response == nil || review.Response.Result.Status != metav1.StatusSuccess {
				t.Fatalf("conversion failed: %+v", review.Response)
			}
			if len(review.Response.ConvertedObjects) != 1 {
				t.Fatalf("expected one converted object, got %d", len(review.Response.ConvertedObjects))
			}
			var converted metav1.TypeMeta
			if err := json.Unmarshal(review.Response.ConvertedObjects[0].Raw, &converted); err != nil {
				t.Fatal(err)
			}
			if converted.APIVersion != tc.target || converted.Kind != "MCPServer" {
				t.Fatalf("webhook returned %s %s, want %s MCPServer", converted.APIVersion, converted.Kind, tc.target)
			}
		})
	}
}
