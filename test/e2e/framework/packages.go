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
	"os"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

type LifecycleTier int

const (
	DefaultTier LifecycleTier = iota
	PlatformTier
)

type PackageLifecycle interface {
	Setup(ctx context.Context, cfg *envconf.Config) error
	Teardown(ctx context.Context, cfg *envconf.Config) error
}

type PackageRegistration struct {
	Labels map[string][]string
}

type packageEntry struct {
	reg        PackageRegistration
	lifecycles map[LifecycleTier][]PackageLifecycle
}

var packages = map[string]*packageEntry{}

func RegisterPackage(name string, reg PackageRegistration) {
	if _, ok := packages[name]; ok {
		panic(fmt.Sprintf("package %q already registered", name))
	}
	packages[name] = &packageEntry{
		reg:        reg,
		lifecycles: make(map[LifecycleTier][]PackageLifecycle),
	}
}

func RegisterPackageLifecycle(tier LifecycleTier, name string, lc PackageLifecycle) {
	entry, ok := packages[name]
	if !ok {
		panic(fmt.Sprintf("package %q not registered; call RegisterPackage first", name))
	}
	entry.lifecycles[tier] = append(entry.lifecycles[tier], lc)
}

func resolveLifecycles(name string) []PackageLifecycle {
	entry, ok := packages[name]
	if !ok {
		return nil
	}
	if lcs := entry.lifecycles[PlatformTier]; len(lcs) > 0 {
		return lcs
	}
	return entry.lifecycles[DefaultTier]
}

func labelsIntersect(a, b map[string][]string) bool {
	for key, aVals := range a {
		bVals, ok := b[key]
		if !ok {
			continue
		}
		for _, av := range aVals {
			if slices.Contains(bVals, av) {
				return true
			}
		}
	}
	return false
}

// RunPackage is the entry point for a provider sub-package's TestMain.
//
//  1. Parses flags and resolves the active profile.
//  2. Checks label intersection — no match → returns 0 (skip).
//     No active profile → runs unconditionally.
//  3. Calls the configure callback so the package can build its testenv.
//  4. Resolves lifecycle tier (Platform > Default) and runs Setup in order.
//  5. Calls testenv.Run(m).
//  6. Runs Teardown in reverse order (FILO).
func RunPackage(name string, m *testing.M, configure func(cfg *envconf.Config) env.Environment) int {
	RegisterProfileFlag()

	entry, ok := packages[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown package %q\n", name)
		return 1
	}

	cfg, err := envconf.NewFromFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "package %q: envconf: %v\n", name, err)
		return 1
	}

	profileLabels := ResolveProfile()
	if profileLabels != nil && !labelsIntersect(profileLabels, entry.reg.Labels) {
		fmt.Fprintf(os.Stderr, "package %q: profile labels do not intersect, skipping\n", name)
		return 0
	}
	if profileLabels != nil {
		cfg.WithLabels(profileLabels)
	}

	testenv := configure(cfg)

	ctx := context.Background()
	lcs := resolveLifecycles(name)

	for i, lc := range lcs {
		if err := lc.Setup(ctx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "package %q: lifecycle[%d] setup failed: %v\n", name, i, err)
			for j := i - 1; j >= 0; j-- {
				if tdErr := lcs[j].Teardown(ctx, cfg); tdErr != nil {
					fmt.Fprintf(os.Stderr, "package %q: lifecycle[%d] teardown failed: %v\n", name, j, tdErr)
				}
			}
			return 1
		}
	}

	code := testenv.Run(m)

	for i, lc := range slices.Backward(lcs) {
		if err := lc.Teardown(ctx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "package %q: lifecycle[%d] teardown failed: %v\n", name, i, err)
		}
	}

	return code
}

func resetPackages() {
	packages = map[string]*packageEntry{}
}

// WithNamespaceManagement registers BeforeEachTest/AfterEachTest hooks that
// create a unique namespace before each test and delete it after.
func WithNamespaceManagement(testenv env.Environment, prefix string) {
	testenv.BeforeEachTest(func(ctx context.Context, cfg *envconf.Config, t *testing.T) (context.Context, error) {
		MustDiscoverOperatorOnce(ctx, cfg, t)

		ns := envconf.RandomName(prefix, 16)
		nsObj := &corev1.Namespace{Name: ns}
		if err := cfg.Client().Resources().Create(ctx, nsObj); err != nil {
			return ctx, err
		}
		t.Logf("created namespace %s", ns)
		ctx = context.WithValue(ctx, NsKey, ns)
		return ctx, nil
	})

	testenv.AfterEachTest(func(ctx context.Context, cfg *envconf.Config, t *testing.T) (context.Context, error) {
		ns, ok := ctx.Value(NsKey).(string)
		if !ok || ns == "" {
			return ctx, nil
		}
		if t.Failed() {
			DumpDiagnostics(ctx, t, cfg, ns)
		}
		nsObj := &corev1.Namespace{Name: ns}
		if err := cfg.Client().Resources().Delete(ctx, nsObj); err != nil {
			t.Logf("failed to delete namespace %s: %v", ns, err)
		}
		return ctx, nil
	})
}
