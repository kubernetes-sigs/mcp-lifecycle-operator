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
	"fmt"
	"strings"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

const testPlatformNS = "platform-ns"

func makeLocateFunc(ref OperatorRef) LocateFunc {
	return func(_ context.Context, _ *envconf.Config) (OperatorRef, error) {
		return ref, nil
	}
}

func mustDiscover(t *testing.T, r *Registry) OperatorRef {
	t.Helper()
	ref, err := r.DiscoverOperator(context.Background(), nil)
	if err != nil {
		t.Fatalf("DiscoverOperator() returned unexpected error: %v", err)
	}
	return ref
}

func TestRegistry_Register_ReplacesExisting(t *testing.T) {
	r := &Registry{}
	r.Register(Explicit, makeLocateFunc(OperatorRef{Namespace: "first"}))
	r.Register(Explicit, makeLocateFunc(OperatorRef{Namespace: "second"}))

	ref := mustDiscover(t, r)
	if ref.Namespace != "second" {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, "second")
	}
}

func TestRegistry_DiscoverOperator_ExplicitBeforePlatform(t *testing.T) {
	r := &Registry{}
	full := OperatorRef{Namespace: "explicit-ns", ServiceAccountName: "sa", MetricsServiceName: "svc"}
	r.Register(Explicit, makeLocateFunc(full))
	r.Register(Platform, makeLocateFunc(OperatorRef{Namespace: testPlatformNS}))

	ref := mustDiscover(t, r)
	if ref.Namespace != "explicit-ns" {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, "explicit-ns")
	}
}

func TestRegistry_DiscoverOperator_PlatformBeforeFallback(t *testing.T) {
	r := &Registry{}
	full := OperatorRef{Namespace: testPlatformNS, ServiceAccountName: "sa", MetricsServiceName: "svc"}
	r.Register(Fallback, makeLocateFunc(OperatorRef{Namespace: "fallback-ns"}))
	r.Register(Platform, makeLocateFunc(full))

	ref := mustDiscover(t, r)
	if ref.Namespace != testPlatformNS {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, testPlatformNS)
	}
}

func TestRegistry_DiscoverOperator_FallsThroughWhenLocatorReturnsFalse(t *testing.T) {
	r := &Registry{}
	full := OperatorRef{Namespace: testPlatformNS, ServiceAccountName: "sa", MetricsServiceName: "svc"}
	r.Register(Explicit, makeLocateFunc(OperatorRef{}))
	r.Register(Platform, makeLocateFunc(full))

	ref := mustDiscover(t, r)
	if ref.Namespace != testPlatformNS {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, testPlatformNS)
	}
}

func TestRegistry_DiscoverOperator_FallsBackToDefaultsWhenOthersDontMatch(t *testing.T) {
	r := &Registry{}
	r.Register(Explicit, makeLocateFunc(OperatorRef{}))
	r.Register(Platform, makeLocateFunc(OperatorRef{}))

	ref := mustDiscover(t, r)
	want := OperatorRef{
		Namespace:          DefaultNamespace,
		ServiceAccountName: DefaultServiceAccountName,
		MetricsServiceName: DefaultMetricsServiceName,
	}
	if ref != want {
		t.Errorf("expected defaults %+v, got %+v", want, ref)
	}
}

func TestRegistry_DiscoverOperator_EmptyRegistryReturnsDefaults(t *testing.T) {
	r := &Registry{}

	ref := mustDiscover(t, r)
	want := OperatorRef{
		Namespace:          DefaultNamespace,
		ServiceAccountName: DefaultServiceAccountName,
		MetricsServiceName: DefaultMetricsServiceName,
	}
	if ref != want {
		t.Errorf("expected defaults %+v, got %+v", want, ref)
	}
}

func TestRegistry_DiscoverOperator_SkipsMissingTiers(t *testing.T) {
	r := &Registry{}
	full := OperatorRef{Namespace: testPlatformNS, ServiceAccountName: "sa", MetricsServiceName: "svc"}
	r.Register(Platform, makeLocateFunc(full))

	ref := mustDiscover(t, r)
	if ref.Namespace != testPlatformNS {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, testPlatformNS)
	}
}

func TestRegistry_DiscoverOperator_AllFieldsReturned(t *testing.T) {
	r := &Registry{}
	want := OperatorRef{
		Namespace:          "test-ns",
		ServiceAccountName: "test-sa",
		MetricsServiceName: "test-svc",
	}
	r.Register(Explicit, makeLocateFunc(want))

	got := mustDiscover(t, r)
	if got != want {
		t.Errorf("DiscoverOperator() = %+v, want %+v", got, want)
	}
}

func TestRegistry_DiscoverOperator_StopsWhenComplete(t *testing.T) {
	full := OperatorRef{Namespace: "explicit-ns", ServiceAccountName: "sa", MetricsServiceName: "svc"}
	fallbackCalled := false
	fallbackLocate := func(_ context.Context, _ *envconf.Config) (OperatorRef, error) {
		fallbackCalled = true
		return OperatorRef{Namespace: "fallback-ns"}, nil
	}

	r := &Registry{}
	r.Register(Explicit, makeLocateFunc(full))
	r.Register(Fallback, fallbackLocate)

	mustDiscover(t, r)

	if fallbackCalled {
		t.Error("Fallback locator was called after result was complete, want skipped")
	}
}

func TestRegistry_DiscoverOperator_MergesPartialResults(t *testing.T) {
	r := &Registry{}
	r.Register(Explicit, makeLocateFunc(OperatorRef{Namespace: "env-ns"}))
	r.Register(Platform, makeLocateFunc(OperatorRef{
		Namespace:          "platform-ns",
		ServiceAccountName: "platform-sa",
		MetricsServiceName: "platform-svc",
	}))

	ref := mustDiscover(t, r)
	want := OperatorRef{
		Namespace:          "env-ns",
		ServiceAccountName: "platform-sa",
		MetricsServiceName: "platform-svc",
	}
	if ref != want {
		t.Errorf("DiscoverOperator() = %+v, want %+v", ref, want)
	}
}

func TestRegistry_DiscoverOperator_NoMatchReturnsError(t *testing.T) {
	r := &Registry{}
	noMatch := func(_ context.Context, _ *envconf.Config) (OperatorRef, error) {
		return OperatorRef{}, nil
	}
	r.Register(Explicit, noMatch)
	r.Register(Platform, noMatch)
	r.Register(Fallback, noMatch)

	_, err := r.DiscoverOperator(context.Background(), nil)
	if err == nil {
		t.Fatal("DiscoverOperator() returned nil error, want error when no locator matches")
	}
}

func TestRegistry_DiscoverOperator_PropagatesLocatorError(t *testing.T) {
	r := &Registry{}
	r.Register(Explicit, func(_ context.Context, _ *envconf.Config) (OperatorRef, error) {
		return OperatorRef{}, fmt.Errorf("connection refused")
	})

	_, err := r.DiscoverOperator(context.Background(), nil)
	if err == nil {
		t.Fatal("DiscoverOperator() returned nil error, want propagated error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "connection refused")
	}
}

func TestRegistry_MustDiscoverOperatorOnce_CachesResult(t *testing.T) {
	calls := 0
	r := &Registry{}
	r.Register(Explicit, func(_ context.Context, _ *envconf.Config) (OperatorRef, error) {
		calls++
		return OperatorRef{Namespace: "cached-ns", ServiceAccountName: "sa", MetricsServiceName: "svc"}, nil
	})

	first := r.MustDiscoverOperatorOnce(context.Background(), nil, t)
	second := r.MustDiscoverOperatorOnce(context.Background(), nil, t)

	if calls != 1 {
		t.Errorf("locator called %d times, want 1 (cached)", calls)
	}
	if first != second {
		t.Errorf("first = %+v, second = %+v, want identical", first, second)
	}
}

func TestRegistry_Register_OverridesEnsureDefaults(t *testing.T) {
	r := &Registry{}
	custom := func(_ context.Context, _ *envconf.Config) (OperatorRef, error) {
		return OperatorRef{Namespace: "custom-ns", ServiceAccountName: "custom-sa", MetricsServiceName: "custom-svc"}, nil
	}
	r.Register(Platform, custom)

	ref := mustDiscover(t, r)
	if ref.Namespace != "custom-ns" {
		t.Errorf("Namespace = %q, want %q (ensureDefaults should not overwrite)", ref.Namespace, "custom-ns")
	}
}
