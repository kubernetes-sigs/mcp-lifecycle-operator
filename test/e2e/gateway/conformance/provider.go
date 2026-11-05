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

package conformance

import (
	"context"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// Provider describes a gateway provider for conformance testing.
// Infrastructure setup (operator installation, gateway creation) is handled
// by lifecycle hooks — this interface covers per-test prerequisites and
// controller configuration only.
type Provider interface {
	// Name returns the provider identifier used in MCPServer.Spec.Gateway.Provider.
	Name() string

	// Setup runs per-test prerequisites that depend on the dynamic test namespace.
	// Providers can register additional API schemes and create ReferenceGrants
	// from the test namespace to the gateway namespace here.
	// Called once per conformance test, before ConfigData.
	Setup(ctx context.Context, t *testing.T, cfg *envconf.Config, ns string)

	// ConfigData returns a new map with the ConfigMap data the controller reads.
	// Each call returns a fresh map safe to mutate.
	ConfigData() map[string]string

	// InvalidConfigData returns deliberately broken ConfigMap data that causes
	// GatewayRegistered=False. Used by the recovery conformance test.
	InvalidConfigData() map[string]string

	// GatewayAddress returns the gateway's load balancer address for data-plane
	// traffic tests (HTTP reachability, hostname separation).
	GatewayAddress(ctx context.Context, t *testing.T, cfg *envconf.Config) string
}
