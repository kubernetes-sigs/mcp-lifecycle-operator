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
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// Typed error strings mirror the runner's taxonomy (FR-012). The dispatcher
// produces TimeoutError/BackendError on the claim/await path and InvalidRequest
// for a caller argument that can never succeed on retry; ScriptError only ever
// originates inside the sandbox and reaches the caller unchanged.
const (
	errTypeTimeout = "TimeoutError"
	errTypeBackend = "BackendError"
	errTypeInvalid = "InvalidRequest"
)

// maxTimeout caps any execution's time budget - caller-supplied or the toolset
// default - so one execution cannot hold a sandbox indefinitely. It matches the
// MCPToolset spec.timeoutSeconds maximum (3600s), so a validly configured default
// is never silently truncated.
const maxTimeout = 60 * time.Minute

// maxConcurrentExecutions bounds how many sandboxes one dispatcher claims at
// once. Each execution provisions a sandbox pod, so an unbounded burst of
// execute_code calls could exhaust node capacity; excess calls wait for a slot
// (bounded by the execution timeout) rather than piling on.
const maxConcurrentExecutions = 10

// Discovery (list_toolsets / describe_tool) dials each backend MCP server to read
// its own tools/list. backendListTimeout bounds a single backend lookup so one
// slow or unreachable server cannot stall discovery.
const (
	dispatcherClientName    = "mcp-code-mode-dispatcher"
	dispatcherClientVersion = "v0.1.0"
	backendListTimeout      = 15 * time.Second
)

// dispatcher serves the execute_code data plane for exactly one MCPToolset. It
// claims sandboxes directly from the provider (never through the operator
// reconcile loop, FR-009/SC-006).
type dispatcher struct {
	toolset        string
	provider       sandbox.SandboxProvider
	namespace      string
	runnerImage    string
	credential     sandbox.CredentialRef
	endpoints      []sandbox.Endpoint
	defaultTimeout time.Duration

	// listBackend fetches a backend server's tool catalog for discovery. It
	// defaults to liveBackendTools (a real MCP tools/list) and is overridable in
	// tests so discovery can be exercised without a live backend.
	listBackend backendLister

	// sem bounds concurrent executions (one in-flight sandbox per slot). A nil
	// channel disables the bound (used by unit tests that drive run directly);
	// buildDispatcher installs a buffered channel of maxConcurrentExecutions.
	sem chan struct{}
}

// backendLister fetches one backend server's tool catalog over MCP.
type backendLister func(ctx context.Context, ep sandbox.Endpoint) ([]backendTool, error)

// backendTool is a backend tool definition as returned by tools/list, including
// its full input schema (used by describe_tool).
type backendTool struct {
	Name        string
	Description string
	InputSchema any
}

// executeCodeInput is the execute_code tool argument shape.
type executeCodeInput struct {
	Code    string `json:"code" jsonschema:"the script to run against the toolset"`
	Toolset string `json:"toolset" jsonschema:"the toolset to run against"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"per-execution time budget in seconds"`
}

// toolError is the typed, caller-distinguishable error body (FR-012).
type toolError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// executeCodeOutput is the single result-or-error envelope returned to the
// caller. It is wire-compatible with the runner's stdout envelope, so a
// sandbox result unmarshals into it directly.
type executeCodeOutput struct {
	Result any        `json:"result,omitempty"`
	Error  *toolError `json:"error,omitempty"`
}

// listToolsetsOutput is the list_toolsets result (US2).
type listToolsetsOutput struct {
	Toolsets []toolsetInfo `json:"toolsets"`
}

type toolsetInfo struct {
	Name    string       `json:"name"`
	Servers []serverInfo `json:"servers"`
}

// serverInfo describes one backend MCP server and the tools it exposes. Tools is
// populated live from the backend's own tools/list so an agent can discover what
// to call without the tool list being hand-fed into its prompt (progressive
// disclosure). Error is set, and Tools left empty, when the catalog could not be
// fetched; the server is still listed so the agent knows it exists.
type serverInfo struct {
	Name  string     `json:"name"`
	Tools []toolInfo `json:"tools,omitempty"`
	Error string     `json:"error,omitempty"`
}

// toolInfo is the discovery-level view of a backend tool: enough to choose it,
// not its full schema. Call describe_tool for the input schema.
type toolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// describeToolInput is the describe_tool tool argument shape.
type describeToolInput struct {
	Server string `json:"server" jsonschema:"the backend server that owns the tool"`
	Tool   string `json:"tool" jsonschema:"the tool to describe"`
}

// describeToolOutput returns a single backend tool's full definition, including
// its input schema, so the agent can construct a correct tools.call(...).
type describeToolOutput struct {
	Server      string     `json:"server"`
	Tool        string     `json:"tool"`
	Description string     `json:"description,omitempty"`
	InputSchema any        `json:"inputSchema,omitempty"`
	Error       *toolError `json:"error,omitempty"`
}

// run executes a script against the dispatcher's toolset and returns the single
// result-or-error envelope. It Claims a fresh sandbox, ships the code, Awaits
// the single result, and Releases the sandbox regardless of outcome.
func (d *dispatcher) run(ctx context.Context, in executeCodeInput) executeCodeOutput {
	if in.Toolset != d.toolset {
		// A wrong toolset name is a caller error, not a backend failure: retrying
		// the same request can never succeed.
		return invalidOutput("unknown toolset %q (this endpoint serves %q)", in.Toolset, d.toolset)
	}

	timeout := d.timeoutFor(in.Timeout)

	// Bound concurrent sandboxes: wait for a slot before starting the execution
	// clock, so time spent queued is not charged against the script's own time
	// budget. The wait is bounded by the caller's context (cancelled on client
	// disconnect); slots always free as in-flight executions complete.
	if d.sem != nil {
		select {
		case d.sem <- struct{}{}:
			defer func() { <-d.sem }()
		case <-ctx.Done():
			return timeoutOutput("timed out waiting for an execution slot")
		}
	}

	// The claim/await budget is the execution timeout plus slack for scheduling
	// and log retrieval, so a slow sandbox surfaces as a typed error, not a hang.
	ctx, cancel := context.WithTimeout(ctx, timeout+30*time.Second)
	defer cancel()

	spec := sandbox.RunnerSpec{
		Namespace:        d.namespace,
		Toolset:          d.toolset,
		RunnerImage:      d.runnerImage,
		AllowedEndpoints: d.endpoints,
		Credential:       d.credential,
		Code:             in.Code,
		Timeout:          timeout,
	}

	handle, err := d.provider.Claim(ctx, spec)
	if err != nil {
		return backendOutput("claiming sandbox: %v", err)
	}
	defer func() {
		// Release on a fresh context so cleanup still runs after a timeout.
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer releaseCancel()
		// Do not discard the Release error: a failed cleanup is retained by the
		// provider for retry, but it must still be visible in the dispatcher logs.
		if err := d.provider.Release(releaseCtx, handle); err != nil {
			log.FromContext(ctx).Error(err, "releasing sandbox after execution",
				"toolset", d.toolset, "handle", handle.Ref)
		}
	}()

	raw, err := d.provider.Await(ctx, handle)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return timeoutOutput("execution exceeded the time budget")
		}
		return backendOutput("awaiting result: %v", err)
	}

	var out executeCodeOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return backendOutput("decoding sandbox result: %v", err)
	}
	return out
}

// timeoutFor resolves the effective timeout: a positive caller value else the
// dispatcher default, in both cases clamped to maxTimeout so no execution can
// hold a sandbox beyond the hard ceiling.
func (d *dispatcher) timeoutFor(seconds int) time.Duration {
	t := d.defaultTimeout
	if seconds > 0 {
		t = time.Duration(seconds) * time.Second
	}
	if t > maxTimeout {
		return maxTimeout
	}
	return t
}

// listToolsets reports the single toolset this dispatcher serves (US2) together
// with each backend server's tool names and descriptions, discovered live from
// the backends' own tools/list. This gives the agent progressive disclosure: it
// can see what it can call without the tool list being hand-fed into its prompt.
// MVP authorization is coarse: an in-cluster caller reaching this dispatcher is
// permitted to use its toolset.
func (d *dispatcher) listToolsets(ctx context.Context) listToolsetsOutput {
	// Dial the backends concurrently: the lookups are independent and each is
	// already bounded by backendListTimeout, so total latency stays ~one lookup
	// instead of scaling with the number of servers. Each goroutine writes its own
	// slice index, preserving endpoint order without a shared-write race.
	servers := make([]serverInfo, len(d.endpoints))
	var wg sync.WaitGroup
	for i, ep := range d.endpoints {
		wg.Add(1)
		go func(i int, ep sandbox.Endpoint) {
			defer wg.Done()
			si := serverInfo{Name: ep.Server}
			tools, err := d.backendTools(ctx, ep)
			if err != nil {
				// Degrade gracefully: list the server so the agent knows it exists,
				// but surface why its catalog is unavailable.
				si.Error = err.Error()
			} else {
				si.Tools = make([]toolInfo, 0, len(tools))
				for _, t := range tools {
					si.Tools = append(si.Tools, toolInfo{Name: t.Name, Description: t.Description})
				}
			}
			servers[i] = si
		}(i, ep)
	}
	wg.Wait()
	return listToolsetsOutput{Toolsets: []toolsetInfo{{Name: d.toolset, Servers: servers}}}
}

// describeTool returns a single backend tool's full definition (description plus
// input schema) so the agent can build a correct tools.call(...). The server
// must belong to this dispatcher's toolset.
func (d *dispatcher) describeTool(ctx context.Context, in describeToolInput) describeToolOutput {
	out := describeToolOutput{Server: in.Server, Tool: in.Tool}

	ep, found := d.endpointFor(in.Server)
	if !found {
		// Caller named a server outside this toolset: a request error, not a
		// backend failure.
		out.Error = &toolError{Type: errTypeInvalid, Message: fmt.Sprintf("unknown server %q in toolset %q", in.Server, d.toolset)}
		return out
	}

	tools, err := d.backendTools(ctx, ep)
	if err != nil {
		out.Error = &toolError{Type: errTypeBackend, Message: fmt.Sprintf("listing tools on %q: %v", in.Server, err)}
		return out
	}
	for _, t := range tools {
		if t.Name == in.Tool {
			out.Description = t.Description
			out.InputSchema = t.InputSchema
			return out
		}
	}
	// The server was reachable but has no such tool: a caller request error.
	out.Error = &toolError{Type: errTypeInvalid, Message: fmt.Sprintf("tool %q not found on server %q", in.Tool, in.Server)}
	return out
}

// endpointFor returns the allow-listed endpoint for a server name.
func (d *dispatcher) endpointFor(server string) (sandbox.Endpoint, bool) {
	for _, ep := range d.endpoints {
		if ep.Server == server {
			return ep, true
		}
	}
	return sandbox.Endpoint{}, false
}

// backendTools fetches one backend server's tool catalog, applying the per-lookup
// timeout and falling back to the live MCP lister when none is injected.
func (d *dispatcher) backendTools(ctx context.Context, ep sandbox.Endpoint) ([]backendTool, error) {
	lister := d.listBackend
	if lister == nil {
		lister = liveBackendTools
	}
	ctx, cancel := context.WithTimeout(ctx, backendListTimeout)
	defer cancel()
	return lister(ctx, ep)
}

// liveBackendTools connects to a backend MCP server and returns its tools/list.
//
// Discovery is unauthenticated in the MVP: the dispatcher holds only a
// CredentialRef (the resolved token is mounted into the runner, not here), so a
// backend that requires auth to enumerate tools surfaces as an error and is still
// listed. In-cluster MCP servers that authorize via their own ServiceAccount (the
// kubernetes-mcp pattern) enumerate fine.
func liveBackendTools(ctx context.Context, ep sandbox.Endpoint) ([]backendTool, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: dispatcherClientName, Version: dispatcherClientVersion}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             ep.URL,
		HTTPClient:           &http.Client{Timeout: backendListTimeout},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()

	// Use the SDK's Tools iterator, which follows the tools/list cursor across
	// pages. A plain ListTools returns only the first page, so a backend that
	// paginates its catalog would hide its later tools from discovery.
	var tools []backendTool
	for t, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		tools = append(tools, backendTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return tools, nil
}

// register wires the dispatcher's tools onto an MCP server.
func (d *dispatcher) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "execute_code",
		Description: "Run a script inside an isolated sandbox against a toolset and return only " +
			"its final value. Composing several tool calls in one script keeps the " +
			"intermediate data in the sandbox, so only the result you return comes back. " +
			"The runtime is ES5.1 (goja): write synchronous code with var and a top-level " +
			"return. There is no async/await, no require, and no npm. Call another MCP tool " +
			"with tools.call(server, tool, args); it returns the result synchronously, so " +
			"assign it to a variable and use it directly. Discover the servers and tools you " +
			"can call with list_toolsets, and a tool's input schema with describe_tool.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in executeCodeInput) (*mcp.CallToolResult, executeCodeOutput, error) {
		return nil, d.run(ctx, in), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_toolsets",
		Description: "List the toolsets this endpoint can run code against, including each " +
			"server's available tools (name and description) for use in execute_code. " +
			"Call describe_tool for a specific tool's input schema.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listToolsetsOutput, error) {
		return nil, d.listToolsets(ctx), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "describe_tool",
		Description: "Return a backend tool's full definition - description and JSON input " +
			"schema - so you can build a correct tools.call(server, tool, args) in " +
			"execute_code. Use list_toolsets first to find server and tool names.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in describeToolInput) (*mcp.CallToolResult, describeToolOutput, error) {
		return nil, d.describeTool(ctx, in), nil
	})
}

func backendOutput(format string, a ...any) executeCodeOutput {
	return executeCodeOutput{Error: &toolError{Type: errTypeBackend, Message: fmt.Sprintf(format, a...)}}
}

func invalidOutput(format string, a ...any) executeCodeOutput {
	return executeCodeOutput{Error: &toolError{Type: errTypeInvalid, Message: fmt.Sprintf(format, a...)}}
}

func timeoutOutput(msg string) executeCodeOutput {
	return executeCodeOutput{Error: &toolError{Type: errTypeTimeout, Message: msg}}
}
