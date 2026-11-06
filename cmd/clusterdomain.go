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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
)

// parseClusterDomain normalizes a DNS suffix before it is used to construct
// Service URLs. URL syntax such as userinfo, ports, and paths must never be
// interpreted as part of the configured cluster domain.
func parseClusterDomain(raw string) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(raw))
	if domain == "" {
		return controller.DefaultClusterDomain, nil
	}
	// A terminal root dot is valid in DNS configuration. Store the suffix in
	// the same form as the default cluster domain.
	domain = strings.TrimSuffix(domain, ".")
	if problems := validation.IsDNS1123Subdomain(domain); len(problems) > 0 {
		return "", fmt.Errorf("cluster domain %q must be a DNS subdomain: %s", raw, strings.Join(problems, "; "))
	}
	for label := range strings.SplitSeq(domain, ".") {
		if problems := validation.IsDNS1123Label(label); len(problems) > 0 {
			return "", fmt.Errorf("cluster domain %q has an invalid DNS label: %s", raw, strings.Join(problems, "; "))
		}
	}
	return domain, nil
}
