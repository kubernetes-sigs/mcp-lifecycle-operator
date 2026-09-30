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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// fakeProvider is a SandboxProvider that records the RunnerSpec it is handed and
// returns canned results. It exists entirely in-process: no controller, no
// reconcile loop, no API server - which is the point of T014 (SC-006).
type fakeProvider struct {
	mu         sync.Mutex
	claimed    []sandbox.RunnerSpec
	released   int
	await      []byte
	awaitErr   error
	awaitDelay time.Duration
	claimErr   error
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) IsolationGuarantees() sandbox.IsolationGuarantees {
	return sandbox.IsolationGuarantees{FilesystemIsolated: true, PerExecutionEphemeral: true}
}
func (f *fakeProvider) Setup(context.Context, sandbox.ToolsetContext) error    { return nil }
func (f *fakeProvider) Teardown(context.Context, sandbox.ToolsetContext) error { return nil }

func (f *fakeProvider) Claim(_ context.Context, spec sandbox.RunnerSpec) (sandbox.RunnerHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return sandbox.RunnerHandle{}, f.claimErr
	}
	f.claimed = append(f.claimed, spec)
	return sandbox.RunnerHandle{Namespace: spec.Namespace, Name: "fake-runner"}, nil
}

func (f *fakeProvider) Await(ctx context.Context, _ sandbox.RunnerHandle) ([]byte, error) {
	if f.awaitDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.awaitDelay):
		}
	}
	return f.await, f.awaitErr
}

func (f *fakeProvider) Release(context.Context, sandbox.RunnerHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released++
	return nil
}

func testDispatcher(p sandbox.SandboxProvider) *dispatcher {
	return &dispatcher{
		toolset:     "ops-toolset",
		provider:    p,
		namespace:   "demo",
		runnerImage: "quay.io/matzew/codemode-runner:latest",
		endpoints: []sandbox.Endpoint{
			{Server: "kubernetes", URL: "http://kubernetes.demo.svc:8080/mcp"},
			{Server: "github", URL: "http://github.demo.svc:8080/mcp"},
		},
		defaultTimeout: 60 * time.Second,
	}
}

func TestRunReturnsSingleCombinedResult(t *testing.T) {
	// Simulate a two-call script whose only output is one combined value.
	fp := &fakeProvider{await: []byte(`{"result":{"pods":3,"repos":5,"total":8}}`)}
	d := testDispatcher(fp)

	out := d.run(context.Background(), executeCodeInput{
		Code:    "var a = tools.kubernetes.list({}); var b = tools.github.list({}); return a + b;",
		Toolset: "ops-toolset",
	})

	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	result, ok := out.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T; want object", out.Result)
	}
	if result["total"] != float64(8) {
		t.Errorf("total = %v; want 8", result["total"])
	}

	// Exactly one sandbox was claimed and released, and it received the full
	// allow-list (containment plumbing).
	if len(fp.claimed) != 1 {
		t.Fatalf("claims = %d; want 1", len(fp.claimed))
	}
	if fp.released != 1 {
		t.Errorf("releases = %d; want 1", fp.released)
	}
	if got := len(fp.claimed[0].AllowedEndpoints); got != 2 {
		t.Errorf("allowed endpoints = %d; want 2", got)
	}
	if fp.claimed[0].Code == "" {
		t.Errorf("code was not shipped to the sandbox")
	}
}

func TestRunUnknownToolsetIsBackendError(t *testing.T) {
	fp := &fakeProvider{await: []byte(`{"result":null}`)}
	d := testDispatcher(fp)

	out := d.run(context.Background(), executeCodeInput{Code: "return 1;", Toolset: "nope"})
	if out.Error == nil || out.Error.Type != errTypeBackend {
		t.Fatalf("out = %+v; want BackendError", out)
	}
	if len(fp.claimed) != 0 {
		t.Errorf("unknown toolset must not claim a sandbox")
	}
}

func TestRunClaimFailureIsBackendError(t *testing.T) {
	fp := &fakeProvider{claimErr: errors.New("no capacity")}
	d := testDispatcher(fp)

	out := d.run(context.Background(), executeCodeInput{Code: "return 1;", Toolset: "ops-toolset"})
	if out.Error == nil || out.Error.Type != errTypeBackend {
		t.Fatalf("out = %+v; want BackendError", out)
	}
}

func TestRunScriptErrorPassesThrough(t *testing.T) {
	// A ScriptError originates in the sandbox; the dispatcher forwards it verbatim.
	fp := &fakeProvider{await: []byte(`{"error":{"type":"ScriptError","message":"server \"x\" not allowed"}}`)}
	d := testDispatcher(fp)

	out := d.run(context.Background(), executeCodeInput{Code: "tools.call('x','y',{})", Toolset: "ops-toolset"})
	if out.Error == nil || out.Error.Type != "ScriptError" {
		t.Fatalf("out = %+v; want ScriptError", out)
	}
	if fp.released != 1 {
		t.Errorf("sandbox not released after script error")
	}
}

func TestRunTimeoutIsTimeoutError(t *testing.T) {
	// A sandbox that never finishes; a short parent context expires the await.
	fp := &fakeProvider{awaitDelay: time.Hour}
	d := testDispatcher(fp)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	out := d.run(ctx, executeCodeInput{Code: "return 1;", Toolset: "ops-toolset"})
	if out.Error == nil || out.Error.Type != errTypeTimeout {
		t.Fatalf("out = %+v; want TimeoutError", out)
	}
	if fp.released != 1 {
		t.Errorf("releases = %d; want 1 (sandbox released even on timeout)", fp.released)
	}
}

func TestListToolsets(t *testing.T) {
	d := testDispatcher(&fakeProvider{})
	out := d.listToolsets()
	if len(out.Toolsets) != 1 || out.Toolsets[0].Name != "ops-toolset" {
		t.Fatalf("toolsets = %+v; want the single configured toolset", out.Toolsets)
	}
	if len(out.Toolsets[0].Servers) != 2 {
		t.Errorf("servers = %v; want kubernetes and github", out.Toolsets[0].Servers)
	}
}

// TestExecuteCodeOverMCP exercises the full MCP round-trip: a client calls
// execute_code over an in-memory transport and receives the single structured
// result. No controller or API server is involved (SC-006).
func TestExecuteCodeOverMCP(t *testing.T) {
	fp := &fakeProvider{await: []byte(`{"result":7}`)}
	d := testDispatcher(fp)

	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	d.register(server)

	st, ct := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer func() { _ = serverSession.Close() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	clientSession, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "execute_code",
		Arguments: map[string]any{"code": "return 3 + 4;", "toolset": "ops-toolset"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var out executeCodeOutput
	if err := remarshal(res.StructuredContent, &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	if out.Result != float64(7) {
		t.Errorf("result = %v; want 7", out.Result)
	}
}

// remarshal round-trips an arbitrary value through JSON into target.
func remarshal(v any, target any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
