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
	"errors"
	"fmt"
	"log"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// OperatorRef holds the discovered operator namespace and resource names.
type OperatorRef struct {
	Namespace          string
	ServiceAccountName string
	MetricsServiceName string
}

// LocateFunc finds the operator. A non-zero OperatorRef means found (possibly
// partial). A zero OperatorRef with nil error means not found. A non-nil error
// is propagated and aborts discovery.
type LocateFunc func(ctx context.Context, cfg *envconf.Config) (OperatorRef, error)

// Tier determines evaluation order: Explicit -> Platform -> Fallback.
type Tier string

const (
	Explicit Tier = "Explicit"
	Platform Tier = "Platform"
	Fallback Tier = "Fallback"
)

var tierOrder = []Tier{Explicit, Platform, Fallback}

// Registry holds tier-keyed locators and caches the discovery result.
type Registry struct {
	tiers        map[Tier]LocateFunc
	discoverOnce sync.Once
	cached       OperatorRef
	cachedErr    error
}

var defaultRegistry Registry

// Register adds a locator to the default registry at the given tier.
func Register(tier Tier, fn LocateFunc) {
	defaultRegistry.Register(tier, fn)
}

// DiscoverOperator runs the default registry through all tiers.
func DiscoverOperator(ctx context.Context, cfg *envconf.Config) (OperatorRef, error) {
	return defaultRegistry.DiscoverOperator(ctx, cfg)
}

func MustDiscoverOperatorOnce(ctx context.Context, cfg *envconf.Config, t testing.TB) OperatorRef {
	return defaultRegistry.MustDiscoverOperatorOnce(ctx, cfg, t)
}

// Register adds a locator at the given tier, replacing any existing one.
func (r *Registry) Register(tier Tier, fn LocateFunc) {
	if r.tiers == nil {
		r.tiers = make(map[Tier]LocateFunc)
	}
	r.tiers[tier] = fn
}

func (r *Registry) DiscoverOperator(ctx context.Context, cfg *envconf.Config) (OperatorRef, error) {
	r.ensureDefaults()
	var result OperatorRef
	matched := false
	for _, tier := range tierOrder {
		fn, ok := r.tiers[tier]
		if !ok {
			continue
		}
		ref, err := fn(ctx, cfg)
		if err != nil {
			return OperatorRef{}, fmt.Errorf("operator discovery (%s/%s): %w",
				tier, locateFuncName(fn), err)
		}
		if ref.isZero() {
			continue
		}
		matched = true
		log.Printf("operator discovery: tier=%s func=%s ns=%s sa=%s svc=%s",
			tier, locateFuncName(fn), ref.Namespace, ref.ServiceAccountName, ref.MetricsServiceName)
		result.merge(ref)
		if result.isComplete() {
			break
		}
	}
	if !matched {
		return OperatorRef{}, errors.New("operator discovery: no locator matched")
	}
	log.Printf("operator resolved: ns=%s sa=%s svc=%s",
		result.Namespace, result.ServiceAccountName, result.MetricsServiceName)
	return result, nil
}

// MustDiscoverOperatorOnce runs DiscoverOperator at most once, caches the
// result, and calls t.Fatalf if discovery fails.
func (r *Registry) MustDiscoverOperatorOnce(ctx context.Context, cfg *envconf.Config, t testing.TB) OperatorRef {
	t.Helper()
	r.discoverOnce.Do(func() {
		r.cached, r.cachedErr = r.DiscoverOperator(ctx, cfg)
	})
	if r.cachedErr != nil {
		t.Fatalf("operator discovery failed: %v", r.cachedErr)
	}
	return r.cached
}

func (o *OperatorRef) merge(other OperatorRef) {
	if o.Namespace == "" {
		o.Namespace = other.Namespace
	}
	if o.ServiceAccountName == "" {
		o.ServiceAccountName = other.ServiceAccountName
	}
	if o.MetricsServiceName == "" {
		o.MetricsServiceName = other.MetricsServiceName
	}
}

func (o *OperatorRef) isZero() bool {
	return o.Namespace == "" && o.ServiceAccountName == "" && o.MetricsServiceName == ""
}

func (o *OperatorRef) isComplete() bool {
	return o.Namespace != "" && o.ServiceAccountName != "" && o.MetricsServiceName != ""
}

func locateFuncName(fn LocateFunc) string {
	ptr := reflect.ValueOf(fn).Pointer()
	f := runtime.FuncForPC(ptr)
	if f == nil {
		return "anonymous"
	}
	name := f.Name()
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	if strings.HasPrefix(name, "func") {
		return "anonymous"
	}
	return name
}
