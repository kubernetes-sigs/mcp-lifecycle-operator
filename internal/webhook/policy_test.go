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

package webhook

import (
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAdmissionPolicy(t *testing.T) {
	log := testr.New(t)

	t.Setenv("IMAGE_ALLOWLIST", "")
	t.Setenv("REQUIRE_IMAGE_DIGEST", "")
	t.Setenv("MAX_STORAGE_MOUNTS", "")
	t.Setenv("REQUIRED_LABELS", "")

	t.Run("empty allowlist", func(t *testing.T) {
		p := ParseAdmissionPolicy(log, PolicyFlags{MaxStorageMounts: -1})
		assert.Empty(t, p.ImageAllowlist)
		assert.False(t, p.RequireImageDigest)
	})

	t.Run("flag takes precedence over env", func(t *testing.T) {
		t.Setenv("IMAGE_ALLOWLIST", "env.io/")
		p := ParseAdmissionPolicy(log, PolicyFlags{ImageAllowlist: "flag.io/", MaxStorageMounts: -1})
		require.Len(t, p.ImageAllowlist, 1)
		assert.Equal(t, "flag.io/", p.ImageAllowlist[0])
	})

	t.Run("env var fallback", func(t *testing.T) {
		t.Setenv("IMAGE_ALLOWLIST", "ghcr.io/mcp/")
		p := ParseAdmissionPolicy(log, PolicyFlags{MaxStorageMounts: -1})
		require.Len(t, p.ImageAllowlist, 1)
		assert.Equal(t, "ghcr.io/mcp/", p.ImageAllowlist[0])
	})

	t.Run("multiple prefixes", func(t *testing.T) {
		p := ParseAdmissionPolicy(log, PolicyFlags{ImageAllowlist: "ghcr.io/mcp/,registry.redhat.io/", MaxStorageMounts: -1})
		require.Len(t, p.ImageAllowlist, 2)
		assert.Equal(t, "ghcr.io/mcp/", p.ImageAllowlist[0])
		assert.Equal(t, "registry.redhat.io/", p.ImageAllowlist[1])
	})

	t.Run("trims whitespace", func(t *testing.T) {
		p := ParseAdmissionPolicy(log, PolicyFlags{ImageAllowlist: " ghcr.io/ , quay.io/ ", MaxStorageMounts: -1})
		require.Len(t, p.ImageAllowlist, 2)
		assert.Equal(t, "ghcr.io/", p.ImageAllowlist[0])
		assert.Equal(t, "quay.io/", p.ImageAllowlist[1])
	})

	t.Run("skips empty entries", func(t *testing.T) {
		p := ParseAdmissionPolicy(log, PolicyFlags{ImageAllowlist: "ghcr.io/,,quay.io/", MaxStorageMounts: -1})
		require.Len(t, p.ImageAllowlist, 2)
	})

	t.Run("require digest from flag", func(t *testing.T) {
		p := ParseAdmissionPolicy(log, PolicyFlags{RequireImageDigest: true, MaxStorageMounts: -1})
		assert.True(t, p.RequireImageDigest)
	})

	t.Run("require digest from env", func(t *testing.T) {
		t.Setenv("REQUIRE_IMAGE_DIGEST", "true")
		p := ParseAdmissionPolicy(log, PolicyFlags{MaxStorageMounts: -1})
		assert.True(t, p.RequireImageDigest)
	})

	t.Run("max storage mounts from env", func(t *testing.T) {
		t.Setenv("MAX_STORAGE_MOUNTS", "3")
		p := ParseAdmissionPolicy(log, PolicyFlags{MaxStorageMounts: -1})
		require.NotNil(t, p.MaxStorageMounts)
		assert.Equal(t, 3, *p.MaxStorageMounts)
	})

	t.Run("max storage mounts from flag", func(t *testing.T) {
		p := ParseAdmissionPolicy(log, PolicyFlags{MaxStorageMounts: 5})
		require.NotNil(t, p.MaxStorageMounts)
		assert.Equal(t, 5, *p.MaxStorageMounts)
	})

	t.Run("max storage mounts flag overrides env", func(t *testing.T) {
		t.Setenv("MAX_STORAGE_MOUNTS", "10")
		p := ParseAdmissionPolicy(log, PolicyFlags{MaxStorageMounts: 2})
		require.NotNil(t, p.MaxStorageMounts)
		assert.Equal(t, 2, *p.MaxStorageMounts)
	})

	t.Run("invalid max storage mounts ignored with warning", func(t *testing.T) {
		t.Setenv("MAX_STORAGE_MOUNTS", "notanumber")
		p := ParseAdmissionPolicy(log, PolicyFlags{MaxStorageMounts: -1})
		assert.Nil(t, p.MaxStorageMounts)
	})

	t.Run("negative max storage mounts env ignored with warning", func(t *testing.T) {
		t.Setenv("MAX_STORAGE_MOUNTS", "-5")
		p := ParseAdmissionPolicy(log, PolicyFlags{MaxStorageMounts: -1})
		assert.Nil(t, p.MaxStorageMounts)
	})

	t.Run("required labels from env", func(t *testing.T) {
		t.Setenv("REQUIRED_LABELS", "app.kubernetes.io/managed-by,team")
		p := ParseAdmissionPolicy(log, PolicyFlags{MaxStorageMounts: -1})
		require.Len(t, p.RequiredLabels, 2)
		assert.Equal(t, "app.kubernetes.io/managed-by", p.RequiredLabels[0])
	})

	t.Run("required labels from flag", func(t *testing.T) {
		p := ParseAdmissionPolicy(log, PolicyFlags{RequiredLabels: "team,env", MaxStorageMounts: -1})
		require.Len(t, p.RequiredLabels, 2)
		assert.Equal(t, "team", p.RequiredLabels[0])
		assert.Equal(t, "env", p.RequiredLabels[1])
	})

	t.Run("required labels flag overrides env", func(t *testing.T) {
		t.Setenv("REQUIRED_LABELS", "env-label")
		p := ParseAdmissionPolicy(log, PolicyFlags{RequiredLabels: "flag-label", MaxStorageMounts: -1})
		require.Len(t, p.RequiredLabels, 1)
		assert.Equal(t, "flag-label", p.RequiredLabels[0])
	})
}

func TestHasActiveRules(t *testing.T) {
	t.Run("empty policy", func(t *testing.T) {
		p := &AdmissionPolicy{}
		assert.False(t, p.HasActiveRules())
	})

	t.Run("with allowlist", func(t *testing.T) {
		p := &AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/"}}
		assert.True(t, p.HasActiveRules())
	})

	t.Run("with digest requirement", func(t *testing.T) {
		p := &AdmissionPolicy{RequireImageDigest: true}
		assert.True(t, p.HasActiveRules())
	})

	t.Run("with max storage mounts", func(t *testing.T) {
		max := 3
		p := &AdmissionPolicy{MaxStorageMounts: &max}
		assert.True(t, p.HasActiveRules())
	})

	t.Run("with required labels", func(t *testing.T) {
		p := &AdmissionPolicy{RequiredLabels: []string{"team"}}
		assert.True(t, p.HasActiveRules())
	})
}

func TestValidateImageAllowlist(t *testing.T) {
	t.Run("empty allowlist allows all", func(t *testing.T) {
		p := &AdmissionPolicy{}
		assert.Nil(t, p.ValidateImageAllowlist("docker.io/anything:latest"))
	})

	t.Run("matching prefix allowed", func(t *testing.T) {
		p := &AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/modelcontextprotocol/"}}
		assert.Nil(t, p.ValidateImageAllowlist("ghcr.io/modelcontextprotocol/servers/filesystem:latest"))
	})

	t.Run("non-matching prefix denied", func(t *testing.T) {
		p := &AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/modelcontextprotocol/"}}
		err := p.ValidateImageAllowlist("docker.io/malicious/server:latest")
		require.NotNil(t, err)
		assert.Contains(t, err.Detail, "does not match any allowed registry prefix")
		assert.Contains(t, err.Detail, "ghcr.io/modelcontextprotocol/")
	})

	t.Run("multiple prefixes with partial match", func(t *testing.T) {
		p := &AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/mcp/", "registry.redhat.io/"}}
		assert.Nil(t, p.ValidateImageAllowlist("registry.redhat.io/my-server:v1"))
		err := p.ValidateImageAllowlist("quay.io/other:v1")
		require.NotNil(t, err)
	})

	t.Run("prefix without trailing slash rejects extended path", func(t *testing.T) {
		p := &AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/trusted"}}
		err := p.ValidateImageAllowlist("ghcr.io/trusted-evil/malware:latest")
		require.NotNil(t, err, "should reject prefix that extends into a different org")
	})

	t.Run("prefix without trailing slash allows sub-path", func(t *testing.T) {
		p := &AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/trusted"}}
		assert.Nil(t, p.ValidateImageAllowlist("ghcr.io/trusted/server:v1"))
	})

	t.Run("prefix without trailing slash allows tag boundary", func(t *testing.T) {
		p := &AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/trusted/server"}}
		assert.Nil(t, p.ValidateImageAllowlist("ghcr.io/trusted/server:v1"))
	})

	t.Run("prefix without trailing slash allows digest boundary", func(t *testing.T) {
		p := &AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/trusted/server"}}
		assert.Nil(t, p.ValidateImageAllowlist("ghcr.io/trusted/server@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"))
	})

	t.Run("exact match without trailing slash", func(t *testing.T) {
		p := &AdmissionPolicy{ImageAllowlist: []string{"ghcr.io/trusted/server"}}
		assert.Nil(t, p.ValidateImageAllowlist("ghcr.io/trusted/server"))
	})
}

func TestValidateImageDigest(t *testing.T) {
	t.Run("disabled allows tags", func(t *testing.T) {
		p := &AdmissionPolicy{RequireImageDigest: false}
		assert.Nil(t, p.ValidateImageDigest("ghcr.io/mcp/server:latest"))
	})

	t.Run("enabled rejects tags", func(t *testing.T) {
		p := &AdmissionPolicy{RequireImageDigest: true}
		err := p.ValidateImageDigest("ghcr.io/mcp/server:latest")
		require.NotNil(t, err)
		assert.Contains(t, err.Detail, "must use a digest reference")
	})

	t.Run("enabled allows valid digest", func(t *testing.T) {
		p := &AdmissionPolicy{RequireImageDigest: true}
		assert.Nil(t, p.ValidateImageDigest("ghcr.io/mcp/server@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"))
	})

	t.Run("rejects short digest", func(t *testing.T) {
		p := &AdmissionPolicy{RequireImageDigest: true}
		err := p.ValidateImageDigest("ghcr.io/mcp/server@sha256:short")
		require.NotNil(t, err)
	})

	t.Run("rejects empty digest", func(t *testing.T) {
		p := &AdmissionPolicy{RequireImageDigest: true}
		err := p.ValidateImageDigest("ghcr.io/mcp/server@sha256:")
		require.NotNil(t, err)
	})

	t.Run("rejects uppercase hex in digest", func(t *testing.T) {
		p := &AdmissionPolicy{RequireImageDigest: true}
		err := p.ValidateImageDigest("ghcr.io/mcp/server@sha256:ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890")
		require.NotNil(t, err)
	})
}

func TestValidateRuntimePolicy(t *testing.T) {
	maxMounts := 3

	t.Run("no limits allows all", func(t *testing.T) {
		p := &AdmissionPolicy{}
		errs := p.ValidateRuntimePolicy(5, nil)
		assert.Empty(t, errs)
	})

	t.Run("max storage mounts exceeded", func(t *testing.T) {
		p := &AdmissionPolicy{MaxStorageMounts: &maxMounts}
		errs := p.ValidateRuntimePolicy(4, nil)
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Detail, "exceeds maximum allowed (3)")
	})

	t.Run("max storage mounts within limit", func(t *testing.T) {
		p := &AdmissionPolicy{MaxStorageMounts: &maxMounts}
		errs := p.ValidateRuntimePolicy(2, nil)
		assert.Empty(t, errs)
	})

	t.Run("required label missing", func(t *testing.T) {
		p := &AdmissionPolicy{RequiredLabels: []string{"app.kubernetes.io/managed-by"}}
		errs := p.ValidateRuntimePolicy(0, map[string]string{"other": "value"})
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Detail, "app.kubernetes.io/managed-by")
	})

	t.Run("required label present", func(t *testing.T) {
		p := &AdmissionPolicy{RequiredLabels: []string{"app.kubernetes.io/managed-by"}}
		errs := p.ValidateRuntimePolicy(0, map[string]string{"app.kubernetes.io/managed-by": "operator"})
		assert.Empty(t, errs)
	})

	t.Run("combined violations", func(t *testing.T) {
		p := &AdmissionPolicy{
			MaxStorageMounts: &maxMounts,
			RequiredLabels:   []string{"team", "env"},
		}
		errs := p.ValidateRuntimePolicy(5, map[string]string{})
		assert.Len(t, errs, 3) // 1 storage + 2 labels
	})
}
