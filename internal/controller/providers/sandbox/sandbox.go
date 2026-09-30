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

// Package sandbox defines the code-mode execution-plane extension point: the
// SandboxProvider interface, its supporting types, and a name-keyed registry.
//
// It is deliberately a leaf package (it imports neither the controller nor the
// gateway providers package) so that both the MCPToolset controller
// (reconcile-time Setup/Teardown) and the execute_code dispatcher (call-time
// Claim/Release) can depend on it without an import cycle. This mirrors the
// gateway provider registry pattern in the parent package but keeps the two
// registries separate.
package sandbox

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// CredentialScoping describes how a provider scopes the credential handed to a
// sandbox.
type CredentialScoping string

const (
	// CredentialScopingStatic is a single, long-lived scoped credential injected
	// into the sandbox. It is the MVP model and is explicitly NOT production
	// grade (see design doc; RFC 8693 per-execution exchange is deferred).
	CredentialScopingStatic CredentialScoping = "static"
	// CredentialScopingBrokered is a per-execution, brokered credential. No MVP
	// provider offers this yet.
	CredentialScopingBrokered CredentialScoping = "brokered"
)

// IsolationGuarantees are the isolation properties a provider declares. The
// controller checks these against a toolset's policy.minIsolation (FR-011).
type IsolationGuarantees struct {
	// EgressControlled indicates the sandbox's network egress is restricted to
	// the toolset's allow-listed endpoints.
	EgressControlled bool
	// FilesystemIsolated indicates the sandbox has an isolated filesystem not
	// shared with other executions or the host.
	FilesystemIsolated bool
	// PerExecutionEphemeral indicates a fresh sandbox is created per execution
	// and torn down afterwards.
	PerExecutionEphemeral bool
	// CredentialScoping records how the credential handed to the sandbox is
	// scoped.
	CredentialScoping CredentialScoping
}

// Satisfies reports whether these guarantees meet the requested minimum
// isolation level. Levels are ordered: "egress" implies "baseline". An empty
// level is treated as "baseline". An unknown level is never satisfied.
func (g IsolationGuarantees) Satisfies(minIsolation string) bool {
	switch minIsolation {
	case "", "baseline":
		return g.FilesystemIsolated && g.PerExecutionEphemeral
	case "egress":
		return g.FilesystemIsolated && g.PerExecutionEphemeral && g.EgressControlled
	default:
		return false
	}
}

// Endpoint is a resolved MCP server reachable from a sandbox: the toolset-local
// server name and its in-cluster URL (scheme://host:port/path). The JSON tags
// are the wire format used to pass endpoints into the runner (see EnvEndpoints).
type Endpoint struct {
	// Server is the toolset-local name (matches an MCPServer resource name and
	// the name scripts use in tools.call(server, ...)).
	Server string `json:"server"`
	// URL is the in-cluster MCP endpoint URL for the server.
	URL string `json:"url"`
}

// CredentialRef references the scoped credential injected into a sandbox. MVP:
// a single Secret in the toolset namespace. NOT production grade.
type CredentialRef struct {
	// SecretName is the name of a Secret in the toolset namespace, or empty when
	// the toolset's servers require no credential.
	SecretName string
	// Key is the key within the Secret holding the credential value.
	Key string
}

// ToolsetContext is the reconcile-time input a provider needs to establish or
// remove long-lived backend state for a toolset.
type ToolsetContext struct {
	Namespace       string
	Name            string
	ServerEndpoints []Endpoint
	MinIsolation    string
	Timeout         time.Duration
}

// RunnerSpec is the call-time input a provider needs to provision one sandbox
// for a single execution.
type RunnerSpec struct {
	// Namespace is where the sandbox is created (the toolset namespace).
	Namespace string
	// Toolset is the toolset name the execution runs against (labelling/tracing).
	Toolset string
	// RunnerImage is the portable data-plane runner image to run.
	RunnerImage string
	// AllowedEndpoints is the exhaustive set of servers the sandbox may reach.
	// It is the enforcement point for containment (FR-005/SC-004): the sandbox
	// receives no endpoint outside its toolset.
	AllowedEndpoints []Endpoint
	// Credential is the scoped credential injected into the sandbox (MVP: static,
	// NOT production).
	Credential CredentialRef
	// Code is the submitted script.
	Code string
	// Timeout is the per-execution time budget.
	Timeout time.Duration
}

// RunnerHandle is an opaque handle identifying a claimed sandbox so it can be
// released.
type RunnerHandle struct {
	// Namespace and Name identify the sandbox resource (e.g. a Pod) for the
	// baseline pod provider. Other providers may leave these empty and use Ref.
	Namespace string
	Name      string
	// Ref is a provider-specific opaque identifier for backends that are not a
	// single named Kubernetes object.
	Ref string
}

// SandboxProvider is an interchangeable code-mode execution backend.
//
// The interface is deliberately split across the two execution paths:
//   - Setup/Teardown are reconcile-time and called by the MCPToolset controller.
//   - Claim/Release are call-time and called by the execute_code dispatcher.
//     They MUST NOT re-enter the controller reconcile loop nor require the
//     controller to be running (FR-009/SC-006).
type SandboxProvider interface {
	// Name matches spec.provider.
	Name() string
	// IsolationGuarantees are the declared guarantees checked against
	// policy.minIsolation.
	IsolationGuarantees() IsolationGuarantees

	// Setup establishes long-lived backend state for a toolset. Idempotent.
	Setup(ctx context.Context, tc ToolsetContext) error
	// Teardown removes backend state on toolset deletion. Idempotent.
	Teardown(ctx context.Context, tc ToolsetContext) error

	// Claim provisions a fresh isolated sandbox for one execution.
	Claim(ctx context.Context, spec RunnerSpec) (RunnerHandle, error)
	// Await blocks until the claimed sandbox finishes and returns the raw result
	// envelope the runner produced on stdout (JSON: {"result":...} or
	// {"error":{...}}). It does not release the sandbox; the caller Releases
	// regardless of outcome. A ctx deadline returns promptly so the dispatcher can
	// surface a timeout rather than hang.
	Await(ctx context.Context, h RunnerHandle) ([]byte, error)
	// Release tears the sandbox down after the execution.
	Release(ctx context.Context, h RunnerHandle) error
}

var (
	registryMu sync.RWMutex
	registry   = map[string]SandboxProvider{}
)

// Register adds a sandbox provider to the registry, keyed by its Name().
// Providers call this from their init() function. It panics on a duplicate or
// empty name, matching the fail-fast behaviour expected at process startup.
func Register(p SandboxProvider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	name := p.Name()
	if name == "" {
		panic("sandbox: provider with empty name")
	}
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("sandbox: provider %q already registered", name))
	}
	registry[name] = p
}

// Get returns the registered provider for the given name.
func Get(name string) (SandboxProvider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := registry[name]
	return p, ok
}

// Names returns the sorted names of all registered providers (diagnostics).
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
