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

package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

func FuzzConversionRoundTrip(f *testing.F) {
	f.Add("test-server", "default", "ghcr.io/example/server:v1", int32(8080), "/mcp", true, false, "arg1")
	f.Add("", "", "", int32(0), "", false, true, "")
	f.Add("my-server", "prod", "registry.io/img@sha256:abc123", int32(65535), "/api/mcp", false, false, "--verbose")

	f.Fuzz(func(t *testing.T, name, namespace, imageRef string, port int32, path string, stateless, hasNetwork bool, arg string) {
		if port < 0 || port > 65535 {
			return
		}

		original := &MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: MCPServerSpec{
				Source: Source{
					Type: SourceTypeContainerImage,
				},
				Config: ServerConfig{
					Port: port,
					Path: path,
				},
			},
		}

		if imageRef != "" {
			original.Spec.Source.ContainerImage = &ContainerImageSource{Ref: imageRef}
		}

		if arg != "" {
			original.Spec.Config.Arguments = []string{arg}
		}

		if stateless {
			original.Spec.MCP.Stateless = ptr.To(true) //nolint:modernize // ptr.To(true) != new(bool)
		}

		if hasNetwork {
			original.Spec.Network = &NetworkConfig{}
		}

		hub := &v1beta1.MCPServer{}
		if err := original.ConvertTo(hub); err != nil {
			t.Fatalf("ConvertTo failed: %v", err)
		}

		roundTripped := &MCPServer{}
		if err := roundTripped.ConvertFrom(hub); err != nil {
			t.Fatalf("ConvertFrom failed: %v", err)
		}

		if diff := cmp.Diff(original, roundTripped); diff != "" {
			t.Errorf("round-trip mismatch:\n%s", diff)
		}
	})
}
