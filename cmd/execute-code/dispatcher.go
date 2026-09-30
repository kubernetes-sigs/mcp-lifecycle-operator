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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// Typed error strings mirror the runner's taxonomy (FR-012). The dispatcher
// produces BackendError/TimeoutError on the claim/await path; ScriptError only
// ever originates inside the sandbox and reaches the caller unchanged.
const (
	errTypeTimeout = "TimeoutError"
	errTypeBackend = "BackendError"
)

// maxTimeout caps a caller-supplied timeout so one execution cannot hold a
// sandbox indefinitely.
const maxTimeout = 10 * time.Minute

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
	Name    string   `json:"name"`
	Servers []string `json:"servers"`
}

// run executes a script against the dispatcher's toolset and returns the single
// result-or-error envelope. It Claims a fresh sandbox, ships the code, Awaits
// the single result, and Releases the sandbox regardless of outcome.
func (d *dispatcher) run(ctx context.Context, in executeCodeInput) executeCodeOutput {
	if in.Toolset != d.toolset {
		return backendOutput("unknown toolset %q", in.Toolset)
	}

	timeout := d.timeoutFor(in.Timeout)
	spec := sandbox.RunnerSpec{
		Namespace:        d.namespace,
		Toolset:          d.toolset,
		RunnerImage:      d.runnerImage,
		AllowedEndpoints: d.endpoints,
		Credential:       d.credential,
		Code:             in.Code,
		Timeout:          timeout,
	}

	// The claim/await budget is the execution timeout plus slack for scheduling
	// and log retrieval, so a slow sandbox surfaces as a typed error, not a hang.
	ctx, cancel := context.WithTimeout(ctx, timeout+30*time.Second)
	defer cancel()

	handle, err := d.provider.Claim(ctx, spec)
	if err != nil {
		return backendOutput("claiming sandbox: %v", err)
	}
	defer func() {
		// Release on a fresh context so cleanup still runs after a timeout.
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer releaseCancel()
		_ = d.provider.Release(releaseCtx, handle)
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

// timeoutFor resolves the effective timeout: a positive caller value (capped at
// maxTimeout) else the dispatcher default.
func (d *dispatcher) timeoutFor(seconds int) time.Duration {
	if seconds <= 0 {
		return d.defaultTimeout
	}
	t := time.Duration(seconds) * time.Second
	if t > maxTimeout {
		return maxTimeout
	}
	return t
}

// listToolsets reports the single toolset this dispatcher serves (US2). MVP
// authorization is coarse: an in-cluster caller reaching this dispatcher is
// permitted to use its toolset.
func (d *dispatcher) listToolsets() listToolsetsOutput {
	servers := make([]string, 0, len(d.endpoints))
	for _, ep := range d.endpoints {
		servers = append(servers, ep.Server)
	}
	return listToolsetsOutput{Toolsets: []toolsetInfo{{Name: d.toolset, Servers: servers}}}
}

// register wires the dispatcher's tools onto an MCP server.
func (d *dispatcher) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "execute_code",
		Description: "Run a script against a toolset and return only the final result.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in executeCodeInput) (*mcp.CallToolResult, executeCodeOutput, error) {
		return nil, d.run(ctx, in), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_toolsets",
		Description: "List the toolsets this endpoint can run code against.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listToolsetsOutput, error) {
		return nil, d.listToolsets(), nil
	})
}

func backendOutput(format string, a ...any) executeCodeOutput {
	return executeCodeOutput{Error: &toolError{Type: errTypeBackend, Message: fmt.Sprintf(format, a...)}}
}

func timeoutOutput(msg string) executeCodeOutput {
	return executeCodeOutput{Error: &toolError{Type: errTypeTimeout, Message: msg}}
}
