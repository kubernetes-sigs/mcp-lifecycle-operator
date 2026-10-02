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
	"net/http"
	"net/http/httptest"
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
	// Inject a catalog per server so discovery is exercised without a live backend.
	d.listBackend = func(_ context.Context, ep sandbox.Endpoint) ([]backendTool, error) {
		return []backendTool{
			{Name: ep.Server + "_list", Description: "list " + ep.Server + " objects"},
		}, nil
	}

	out := d.listToolsets(context.Background())
	if len(out.Toolsets) != 1 || out.Toolsets[0].Name != "ops-toolset" {
		t.Fatalf("toolsets = %+v; want the single configured toolset", out.Toolsets)
	}

	servers := out.Toolsets[0].Servers
	if len(servers) != 2 {
		t.Fatalf("servers = %+v; want kubernetes and github", servers)
	}
	// Progressive disclosure: each server advertises its tool names.
	for _, s := range servers {
		if s.Error != "" {
			t.Errorf("server %q reported error: %s", s.Name, s.Error)
		}
		want := s.Name + "_list"
		if len(s.Tools) != 1 || s.Tools[0].Name != want {
			t.Errorf("server %q tools = %+v; want one tool %q", s.Name, s.Tools, want)
		}
	}

	// US2 scenario 2 (coarse MVP authz): under one-dispatcher-per-CRD this
	// dispatcher serves only its own toolset, so any other toolset is omitted.
	for _, ts := range out.Toolsets {
		if ts.Name != d.toolset {
			t.Errorf("listed a toolset this dispatcher does not serve: %q", ts.Name)
		}
	}
}

// TestListToolsetsDegradesOnBackendError proves a server whose catalog cannot be
// fetched is still listed (with its error) rather than dropped or failing the
// whole call.
func TestListToolsetsDegradesOnBackendError(t *testing.T) {
	d := testDispatcher(&fakeProvider{})
	d.listBackend = func(_ context.Context, ep sandbox.Endpoint) ([]backendTool, error) {
		if ep.Server == "github" {
			return nil, errors.New("connection refused")
		}
		return []backendTool{{Name: "pods_list"}}, nil
	}

	servers := d.listToolsets(context.Background()).Toolsets[0].Servers
	if len(servers) != 2 {
		t.Fatalf("servers = %+v; want both listed despite one failing", servers)
	}
	for _, s := range servers {
		switch s.Name {
		case "kubernetes":
			if s.Error != "" || len(s.Tools) != 1 {
				t.Errorf("kubernetes = %+v; want one tool, no error", s)
			}
		case "github":
			if s.Error == "" || len(s.Tools) != 0 {
				t.Errorf("github = %+v; want error and no tools", s)
			}
		}
	}
}

func TestDescribeTool(t *testing.T) {
	schema := map[string]any{"type": "object"}
	d := testDispatcher(&fakeProvider{})
	d.listBackend = func(_ context.Context, ep sandbox.Endpoint) ([]backendTool, error) {
		return []backendTool{
			{Name: "pods_list", Description: "list pods", InputSchema: schema},
		}, nil
	}

	// Found.
	out := d.describeTool(context.Background(), describeToolInput{Server: "kubernetes", Tool: "pods_list"})
	if out.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Error)
	}
	if out.Description != "list pods" || out.InputSchema == nil {
		t.Errorf("describe = %+v; want description and schema", out)
	}

	// Unknown server -> BackendError, no backend dial needed.
	out = d.describeTool(context.Background(), describeToolInput{Server: "nope", Tool: "x"})
	if out.Error == nil || out.Error.Type != errTypeBackend {
		t.Errorf("unknown server: out = %+v; want BackendError", out)
	}

	// Unknown tool on a known server -> BackendError.
	out = d.describeTool(context.Background(), describeToolInput{Server: "kubernetes", Tool: "missing"})
	if out.Error == nil || out.Error.Type != errTypeBackend {
		t.Errorf("unknown tool: out = %+v; want BackendError", out)
	}
}

// TestListToolsetsLive exercises liveBackendTools against a real streamable-HTTP
// MCP backend, proving the discovery path reads tools/list over the wire (not
// just the injected fake).
func TestListToolsetsLive(t *testing.T) {
	backend := newTestMCPBackend(t, "pods_list", "namespaces_list")
	d := &dispatcher{
		toolset:        "ops-toolset",
		provider:       &fakeProvider{},
		namespace:      "demo",
		defaultTimeout: 60 * time.Second,
		endpoints:      []sandbox.Endpoint{{Server: "kubernetes", URL: backend}},
		// listBackend left nil: use the live liveBackendTools path.
	}

	servers := d.listToolsets(context.Background()).Toolsets[0].Servers
	if len(servers) != 1 {
		t.Fatalf("servers = %+v; want one", servers)
	}
	if servers[0].Error != "" {
		t.Fatalf("live discovery failed: %s", servers[0].Error)
	}
	names := map[string]bool{}
	for _, tl := range servers[0].Tools {
		names[tl.Name] = true
	}
	if !names["pods_list"] || !names["namespaces_list"] {
		t.Errorf("tools = %+v; want pods_list and namespaces_list", servers[0].Tools)
	}
}

// newTestMCPBackend starts an in-process streamable-HTTP MCP server exposing the
// named tools (each a no-op) and returns its /mcp endpoint URL.
func newTestMCPBackend(t *testing.T, toolNames ...string) string {
	t.Helper()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		s := mcp.NewServer(&mcp.Implementation{Name: "backend", Version: "v0"}, nil)
		for _, name := range toolNames {
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: name + " tool"},
				func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
					return nil, nil, nil
				})
		}
		return s
	}, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
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
