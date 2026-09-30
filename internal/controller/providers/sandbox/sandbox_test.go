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

package sandbox

import (
	"context"
	"testing"
)

// fakeProvider is a minimal SandboxProvider for registry tests.
type fakeProvider struct {
	name       string
	guarantees IsolationGuarantees
}

func (f fakeProvider) Name() string                                   { return f.name }
func (f fakeProvider) IsolationGuarantees() IsolationGuarantees       { return f.guarantees }
func (f fakeProvider) Setup(context.Context, ToolsetContext) error    { return nil }
func (f fakeProvider) Teardown(context.Context, ToolsetContext) error { return nil }
func (f fakeProvider) Await(context.Context, RunnerHandle) ([]byte, error) {
	return []byte(`{"result":null}`), nil
}
func (f fakeProvider) Claim(context.Context, RunnerSpec) (RunnerHandle, error) {
	return RunnerHandle{}, nil
}
func (f fakeProvider) Release(context.Context, RunnerHandle) error { return nil }

// resetRegistry clears the package registry so tests do not leak into each
// other. Tests must call this before registering.
func resetRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]SandboxProvider{}
}

func TestRegisterAndGet(t *testing.T) {
	resetRegistry()

	p := fakeProvider{name: "pod"}
	Register(p)

	got, ok := Get("pod")
	if !ok {
		t.Fatalf("Get(\"pod\") = _, false; want registered provider")
	}
	if got.Name() != "pod" {
		t.Errorf("got.Name() = %q; want %q", got.Name(), "pod")
	}

	if _, ok := Get("nonexistent"); ok {
		t.Errorf("Get(\"nonexistent\") = _, true; want false")
	}
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	resetRegistry()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Register with empty name did not panic")
		}
	}()
	Register(fakeProvider{name: ""})
}

func TestRegisterDuplicatePanics(t *testing.T) {
	resetRegistry()
	Register(fakeProvider{name: "pod"})
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Register with duplicate name did not panic")
		}
	}()
	Register(fakeProvider{name: "pod"})
}

func TestNames(t *testing.T) {
	resetRegistry()
	Register(fakeProvider{name: "pod"})
	Register(fakeProvider{name: "agentsandbox"})

	names := Names()
	if len(names) != 2 {
		t.Fatalf("Names() len = %d; want 2", len(names))
	}
	// Names must be sorted.
	if names[0] != "agentsandbox" || names[1] != "pod" {
		t.Errorf("Names() = %v; want [agentsandbox pod]", names)
	}
}

func TestIsolationGuaranteesSatisfies(t *testing.T) {
	baseline := IsolationGuarantees{
		FilesystemIsolated:    true,
		PerExecutionEphemeral: true,
		EgressControlled:      false,
		CredentialScoping:     CredentialScopingStatic,
	}
	egress := IsolationGuarantees{
		FilesystemIsolated:    true,
		PerExecutionEphemeral: true,
		EgressControlled:      true,
		CredentialScoping:     CredentialScopingStatic,
	}
	weak := IsolationGuarantees{
		FilesystemIsolated:    false,
		PerExecutionEphemeral: true,
	}

	tests := []struct {
		name         string
		guarantees   IsolationGuarantees
		minIsolation string
		want         bool
	}{
		{"baseline satisfies empty", baseline, "", true},
		{"baseline satisfies baseline", baseline, "baseline", true},
		{"baseline does not satisfy egress", baseline, "egress", false},
		{"egress satisfies baseline", egress, "baseline", true},
		{"egress satisfies egress", egress, "egress", true},
		{"weak does not satisfy baseline", weak, "baseline", false},
		{"unknown level never satisfied", egress, "nonsense", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.guarantees.Satisfies(tt.minIsolation); got != tt.want {
				t.Errorf("Satisfies(%q) = %v; want %v", tt.minIsolation, got, tt.want)
			}
		})
	}
}
