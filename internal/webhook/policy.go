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
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// AdmissionPolicy holds the operator-level policy configuration for webhook
// admission decisions. It is parsed once at startup and injected into the
// webhook handler.
type AdmissionPolicy struct {
	ImageAllowlist     []string
	RequireImageDigest bool
	MaxStorageMounts   *int
	RequiredLabels     []string
}

// PolicyFlags holds the CLI flag values for admission policy. A value of -1
// for MaxStorageMounts indicates the flag was not set.
type PolicyFlags struct {
	ImageAllowlist     string
	RequireImageDigest bool
	MaxStorageMounts   int
	RequiredLabels     string
}

// ParseAdmissionPolicy reads admission policy from CLI flags and environment
// variable fallbacks. Flag values take precedence over env vars.
func ParseAdmissionPolicy(log logr.Logger, flags PolicyFlags) *AdmissionPolicy {
	policy := &AdmissionPolicy{
		RequireImageDigest: flags.RequireImageDigest,
	}

	allowlist := flags.ImageAllowlist
	if allowlist == "" {
		allowlist = os.Getenv("IMAGE_ALLOWLIST")
	}
	if allowlist != "" {
		for prefix := range strings.SplitSeq(allowlist, ",") {
			trimmed := strings.TrimSpace(prefix)
			if trimmed != "" {
				policy.ImageAllowlist = append(policy.ImageAllowlist, trimmed)
			}
		}
	}

	if !policy.RequireImageDigest {
		if envVal := os.Getenv("REQUIRE_IMAGE_DIGEST"); envVal != "" {
			policy.RequireImageDigest = strings.EqualFold(envVal, "true")
		}
	}

	if flags.MaxStorageMounts >= 0 {
		policy.MaxStorageMounts = &flags.MaxStorageMounts
	} else if envVal := os.Getenv("MAX_STORAGE_MOUNTS"); envVal != "" {
		if n, err := strconv.Atoi(envVal); err == nil && n >= 0 {
			policy.MaxStorageMounts = &n
		} else {
			log.Info("Ignoring invalid MAX_STORAGE_MOUNTS, must be a non-negative integer", "value", envVal)
		}
	}

	labels := flags.RequiredLabels
	if labels == "" {
		labels = os.Getenv("REQUIRED_LABELS")
	}
	if labels != "" {
		for label := range strings.SplitSeq(labels, ",") {
			trimmed := strings.TrimSpace(label)
			if trimmed != "" {
				policy.RequiredLabels = append(policy.RequiredLabels, trimmed)
			}
		}
	}

	return policy
}

// HasActiveRules returns true if the policy has any active enforcement rules configured.
func (p *AdmissionPolicy) HasActiveRules() bool {
	return len(p.ImageAllowlist) > 0 || p.RequireImageDigest ||
		p.MaxStorageMounts != nil || len(p.RequiredLabels) > 0
}

// ValidateImageAllowlist checks that imageRef starts with one of the allowed
// prefixes. Returns nil if the allowlist is empty (no restriction) or the image
// matches. Prefix matching enforces a path boundary to prevent
// "ghcr.io/trusted" from matching "ghcr.io/trusted-evil/...".
func (p *AdmissionPolicy) ValidateImageAllowlist(imageRef string) *field.Error {
	if len(p.ImageAllowlist) == 0 {
		return nil
	}
	for _, prefix := range p.ImageAllowlist {
		if strings.HasPrefix(imageRef, prefix) {
			if len(imageRef) == len(prefix) {
				return nil
			}
			next := imageRef[len(prefix)]
			if next == '/' || next == ':' || next == '@' {
				return nil
			}
			if strings.HasSuffix(prefix, "/") {
				return nil
			}
		}
	}
	return field.Forbidden(
		field.NewPath("spec", "source", "containerImage", "ref"),
		fmt.Sprintf("image %q does not match any allowed registry prefix: %v", imageRef, p.ImageAllowlist),
	)
}

var validDigestRe = regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)

// ValidateImageDigest checks that imageRef contains a valid digest reference
// (@sha256:<64 hex chars>) when RequireImageDigest is true.
func (p *AdmissionPolicy) ValidateImageDigest(imageRef string) *field.Error {
	if !p.RequireImageDigest {
		return nil
	}
	if validDigestRe.MatchString(imageRef) {
		return nil
	}
	return field.Forbidden(
		field.NewPath("spec", "source", "containerImage", "ref"),
		fmt.Sprintf("image %q must use a digest reference (@sha256:<64 hex chars>) when digest pinning is required", imageRef),
	)
}

// ValidateRuntimePolicy checks storage mount count and required labels against
// policy rules. The caller extracts these values from the MCPServer to avoid an
// import cycle between this package and api/v1alpha1.
func (p *AdmissionPolicy) ValidateRuntimePolicy(storageMountCount int, labels map[string]string) field.ErrorList {
	var allErrs field.ErrorList

	if p.MaxStorageMounts != nil && storageMountCount > *p.MaxStorageMounts {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec", "config", "storage"),
			fmt.Sprintf("number of storage mounts (%d) exceeds maximum allowed (%d)", storageMountCount, *p.MaxStorageMounts),
		))
	}

	for _, requiredLabel := range p.RequiredLabels {
		if _, ok := labels[requiredLabel]; !ok {
			allErrs = append(allErrs, field.Required(
				field.NewPath("metadata", "labels").Key(requiredLabel),
				fmt.Sprintf("label %q is required by admission policy", requiredLabel),
			))
		}
	}

	return allErrs
}
