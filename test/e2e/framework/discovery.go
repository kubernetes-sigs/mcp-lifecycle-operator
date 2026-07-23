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
	"context"
	"log"
	"sort"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

const (
	defaultNamespace          = "mcp-lifecycle-operator-system"
	defaultServiceAccountName = "mcp-lifecycle-operator-controller-manager"
	defaultMetricsServiceName = "mcp-lifecycle-operator-controller-manager-metrics-service"
)

// OperatorLocator finds the operator namespace, SA, and metrics service.
// Each method returns the value and true if found, or ("", false) to
// fall through to the next locator.
type OperatorLocator interface {
	Namespace(ctx context.Context, cfg *envconf.Config) (string, bool)
	ServiceAccount(ctx context.Context, cfg *envconf.Config, namespace string) (string, bool)
	MetricsService(ctx context.Context, cfg *envconf.Config, namespace string) (string, bool)
	// Priority determines execution order. Higher priority runs first.
	// Recommended bands:
	//   5000 = env var overrides (always wins when set)
	//    500 = platform-specific (e.g. ODH/RHOAI module operator CR)
	//     50 = generic cluster search (pod label scan)
	//      0 = hardcoded fallback (built into Discover* functions)
	Priority() int
}

var locators []OperatorLocator

// RegisterLocator adds a locator and re-sorts by priority (highest first).
// Call from init() in separate files to register platform-specific strategies.
func RegisterLocator(d OperatorLocator) {
	locators = append(locators, d)
	sort.Slice(locators, func(i, j int) bool {
		return locators[i].Priority() > locators[j].Priority()
	})
}

// DiscoverNamespace tries each registered locator in priority order.
// Falls back to "mcp-lifecycle-operator-system".
func DiscoverNamespace(ctx context.Context, cfg *envconf.Config) string {
	for _, d := range locators {
		if ns, ok := d.Namespace(ctx, cfg); ok {
			log.Printf("operator namespace discovered: %s (by %T)", ns, d)
			return ns
		}
	}
	log.Printf("operator namespace: using default %s", defaultNamespace)
	return defaultNamespace
}

// DiscoverServiceAccount tries each registered locator in priority order.
// Falls back to "mcp-lifecycle-operator-controller-manager".
func DiscoverServiceAccount(ctx context.Context, cfg *envconf.Config, namespace string) string {
	for _, d := range locators {
		if sa, ok := d.ServiceAccount(ctx, cfg, namespace); ok {
			log.Printf("operator service account discovered: %s (by %T)", sa, d)
			return sa
		}
	}
	log.Printf("operator service account: using default %s", defaultServiceAccountName)
	return defaultServiceAccountName
}

// DiscoverMetricsService tries each registered locator in priority order.
// Falls back to "mcp-lifecycle-operator-controller-manager-metrics-service".
func DiscoverMetricsService(ctx context.Context, cfg *envconf.Config, namespace string) string {
	for _, d := range locators {
		if svc, ok := d.MetricsService(ctx, cfg, namespace); ok {
			log.Printf("operator metrics service discovered: %s (by %T)", svc, d)
			return svc
		}
	}
	log.Printf("operator metrics service: using default %s", defaultMetricsServiceName)
	return defaultMetricsServiceName
}
