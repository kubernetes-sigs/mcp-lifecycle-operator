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
	"testing"
)

func TestParseAdmissionFlags(t *testing.T) {
	t.Setenv("IMAGE_ALLOWLIST", "")
	t.Setenv("REQUIRE_IMAGE_DIGEST", "")
	t.Setenv("MAX_STORAGE_MOUNTS", "")
	t.Setenv("REQUIRED_LABELS", "")

	t.Run("no flags produces empty policy", func(t *testing.T) {
		p := parseAdmissionFlags("", false, -1, "")
		if len(p.ImageAllowlist) != 0 {
			t.Errorf("expected empty allowlist, got %v", p.ImageAllowlist)
		}
		if p.RequireImageDigest {
			t.Error("expected RequireImageDigest=false")
		}
		if p.MaxStorageMounts != nil {
			t.Errorf("expected nil MaxStorageMounts, got %d", *p.MaxStorageMounts)
		}
		if len(p.RequiredLabels) != 0 {
			t.Errorf("expected empty required labels, got %v", p.RequiredLabels)
		}
	})

	t.Run("all flags set", func(t *testing.T) {
		p := parseAdmissionFlags("ghcr.io/trusted/", true, 5, "team,env")
		if len(p.ImageAllowlist) != 1 || p.ImageAllowlist[0] != "ghcr.io/trusted/" {
			t.Errorf("unexpected allowlist: %v", p.ImageAllowlist)
		}
		if !p.RequireImageDigest {
			t.Error("expected RequireImageDigest=true")
		}
		if p.MaxStorageMounts == nil || *p.MaxStorageMounts != 5 {
			t.Errorf("expected MaxStorageMounts=5, got %v", p.MaxStorageMounts)
		}
		if len(p.RequiredLabels) != 2 {
			t.Errorf("expected 2 required labels, got %v", p.RequiredLabels)
		}
	})

	t.Run("env var fallback", func(t *testing.T) {
		t.Setenv("IMAGE_ALLOWLIST", "quay.io/")
		t.Setenv("REQUIRE_IMAGE_DIGEST", "true")
		t.Setenv("MAX_STORAGE_MOUNTS", "3")
		t.Setenv("REQUIRED_LABELS", "app")
		p := parseAdmissionFlags("", false, -1, "")
		if len(p.ImageAllowlist) != 1 || p.ImageAllowlist[0] != "quay.io/" {
			t.Errorf("expected env fallback allowlist, got %v", p.ImageAllowlist)
		}
		if !p.RequireImageDigest {
			t.Error("expected RequireImageDigest=true from env")
		}
		if p.MaxStorageMounts == nil || *p.MaxStorageMounts != 3 {
			t.Errorf("expected MaxStorageMounts=3 from env, got %v", p.MaxStorageMounts)
		}
		if len(p.RequiredLabels) != 1 || p.RequiredLabels[0] != "app" {
			t.Errorf("expected required labels from env, got %v", p.RequiredLabels)
		}
	})
}
