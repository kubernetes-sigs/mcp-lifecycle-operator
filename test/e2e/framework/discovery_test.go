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
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

type stubLocator struct {
	priority  int
	namespace string
	sa        string
	metrics   string
}

func (s *stubLocator) Priority() int { return s.priority }
func (s *stubLocator) Namespace(_ context.Context, _ *envconf.Config) (string, bool) {
	return s.namespace, s.namespace != ""
}
func (s *stubLocator) ServiceAccount(_ context.Context, _ *envconf.Config, _ string) (string, bool) {
	return s.sa, s.sa != ""
}
func (s *stubLocator) MetricsService(_ context.Context, _ *envconf.Config, _ string) (string, bool) {
	return s.metrics, s.metrics != ""
}

func resetLocators() {
	locators = nil
}

func TestRegisterLocator_SortsByPriorityHighestFirst(t *testing.T) {
	resetLocators()
	defer resetLocators()

	low := &stubLocator{priority: 10}
	high := &stubLocator{priority: 5000}

	RegisterLocator(low)
	RegisterLocator(high)

	if len(locators) != 2 {
		t.Fatalf("expected 2 locators, got %d", len(locators))
	}
	if locators[0].Priority() != 5000 {
		t.Errorf("locators[0].Priority() = %d, want 5000 (highest first)", locators[0].Priority())
	}
	if locators[1].Priority() != 10 {
		t.Errorf("locators[1].Priority() = %d, want 10", locators[1].Priority())
	}
}

func TestRegisterLocator_SingleLocator(t *testing.T) {
	resetLocators()
	defer resetLocators()

	RegisterLocator(&stubLocator{priority: 500})

	if len(locators) != 1 {
		t.Fatalf("expected 1 locator, got %d", len(locators))
	}
	if locators[0].Priority() != 500 {
		t.Errorf("locators[0].Priority() = %d, want 500", locators[0].Priority())
	}
}

func TestRegisterLocator_ThreeLocatorsSortedCorrectly(t *testing.T) {
	resetLocators()
	defer resetLocators()

	RegisterLocator(&stubLocator{priority: 50})
	RegisterLocator(&stubLocator{priority: 5000})
	RegisterLocator(&stubLocator{priority: 500})

	want := []int{5000, 500, 50}
	for i, wantPri := range want {
		if locators[i].Priority() != wantPri {
			t.Errorf("locators[%d].Priority() = %d, want %d", i, locators[i].Priority(), wantPri)
		}
	}
}

func TestDiscoverNamespace_HighPriorityWins(t *testing.T) {
	resetLocators()
	defer resetLocators()

	RegisterLocator(&stubLocator{priority: 50, namespace: "low-ns"})
	RegisterLocator(&stubLocator{priority: 5000, namespace: "high-ns"})

	got := DiscoverNamespace(context.Background(), nil)
	if got != "high-ns" {
		t.Errorf("DiscoverNamespace() = %q, want %q", got, "high-ns")
	}
}

func TestDiscoverNamespace_FallsBackToDefault(t *testing.T) {
	resetLocators()
	defer resetLocators()

	RegisterLocator(&stubLocator{priority: 5000, namespace: ""})

	got := DiscoverNamespace(context.Background(), nil)
	if got != defaultNamespace {
		t.Errorf("DiscoverNamespace() = %q, want default %q", got, defaultNamespace)
	}
}

func TestDiscoverNamespace_EmptyRegistry(t *testing.T) {
	resetLocators()
	defer resetLocators()

	got := DiscoverNamespace(context.Background(), nil)
	if got != defaultNamespace {
		t.Errorf("DiscoverNamespace() = %q, want default %q", got, defaultNamespace)
	}
}

func TestDiscoverServiceAccount_HighPriorityWins(t *testing.T) {
	resetLocators()
	defer resetLocators()

	RegisterLocator(&stubLocator{priority: 50, sa: "low-sa"})
	RegisterLocator(&stubLocator{priority: 5000, sa: "high-sa"})

	got := DiscoverServiceAccount(context.Background(), nil, "ns")
	if got != "high-sa" {
		t.Errorf("DiscoverServiceAccount() = %q, want %q", got, "high-sa")
	}
}

func TestDiscoverServiceAccount_FallsBackToDefault(t *testing.T) {
	resetLocators()
	defer resetLocators()

	got := DiscoverServiceAccount(context.Background(), nil, "ns")
	if got != defaultServiceAccountName {
		t.Errorf("DiscoverServiceAccount() = %q, want default %q", got, defaultServiceAccountName)
	}
}

func TestDiscoverMetricsService_HighPriorityWins(t *testing.T) {
	resetLocators()
	defer resetLocators()

	RegisterLocator(&stubLocator{priority: 50, metrics: "low-svc"})
	RegisterLocator(&stubLocator{priority: 5000, metrics: "high-svc"})

	got := DiscoverMetricsService(context.Background(), nil, "ns")
	if got != "high-svc" {
		t.Errorf("DiscoverMetricsService() = %q, want %q", got, "high-svc")
	}
}

func TestDiscoverMetricsService_FallsBackToDefault(t *testing.T) {
	resetLocators()
	defer resetLocators()

	got := DiscoverMetricsService(context.Background(), nil, "ns")
	if got != defaultMetricsServiceName {
		t.Errorf("DiscoverMetricsService() = %q, want default %q", got, defaultMetricsServiceName)
	}
}
