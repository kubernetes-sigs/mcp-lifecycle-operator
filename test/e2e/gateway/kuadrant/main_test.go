//go:build e2e

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
	"os"
	"testing"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	mcpv1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1"
	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

var testenv env.Environment

func TestMain(m *testing.M) {
	os.Exit(f.RunPackage("kuadrant", m, func(cfg *envconf.Config) env.Environment {
		testenv = env.NewWithConfig(cfg)

		scheme := cfg.Client().Resources().GetScheme()
		if err := mcpv1.AddToScheme(scheme); err != nil {
			panic(err)
		}
		if err := mcpv1beta1.AddToScheme(scheme); err != nil {
			panic(err)
		}
		if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
			panic(err)
		}
		if err := gatewayv1.Install(scheme); err != nil {
			panic(err)
		}
		if err := kuadrantapi.AddToScheme(scheme); err != nil {
			panic(err)
		}

		f.WithNamespaceManagement(testenv, "kd")

		return testenv
	}))
}
