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

package main

import (
	"strings"
	"testing"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
)

func TestOpenPostureStartupAdvisory(t *testing.T) {
	t.Run("open posture returns a warning advisory", func(t *testing.T) {
		got := openPostureStartupAdvisory(controller.PostureOpen)
		if got == "" {
			t.Fatal("expected a non-empty advisory for the open posture")
		}
		if !strings.HasPrefix(got, "WARNING:") {
			t.Errorf("expected advisory to start with WARNING:, got %q", got)
		}
		// The advisory must name the risk and both ways to restrict ingress so an
		// operator reading the startup log can act on it.
		for _, want := range []string{
			"any pod in any namespace",
			"spec.network.ingressFrom",
			"--network-policy-default-posture=restricted",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("expected advisory to mention %q, got %q", want, got)
			}
		}
	})

	t.Run("restricted posture returns no advisory", func(t *testing.T) {
		if got := openPostureStartupAdvisory(controller.PostureRestricted); got != "" {
			t.Errorf("expected no advisory for the restricted posture, got %q", got)
		}
	})
}
