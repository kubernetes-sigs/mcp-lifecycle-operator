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

package framework

import (
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scope"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

func init() {
	RegisterProfile(Profile{
		Name: "smoke",
		Labels: map[string][]string{
			category.Label: {category.Lifecycle, category.Configuration},
			speed.Label:    {speed.Fast},
		},
	})
	RegisterProfile(Profile{
		Name: "extended",
		Labels: map[string][]string{
			category.Label: {category.Lifecycle, category.Configuration, category.Resilience},
			speed.Label:    {speed.Fast, speed.Moderate},
		},
	})

	// Gateway provider profiles (require -tags=e2e_gateway to compile
	// matching tests; without it the profile matches nothing).
	RegisterProfile(Profile{
		Name: "gateway-httproute",
		Labels: map[string][]string{
			scope.Label: {scope.GatewayConformance, scope.HTTPRoute},
		},
	})
	RegisterProfile(Profile{
		Name: "gateway-kuadrant",
		Labels: map[string][]string{
			scope.Label: {scope.GatewayConformance, scope.Kuadrant},
		},
	})
}
