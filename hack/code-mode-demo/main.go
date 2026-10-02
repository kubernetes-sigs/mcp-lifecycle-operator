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

// Command code-mode-demo is a tiny MCP client for demoing code mode against a
// running execute_code dispatcher. It connects to the dispatcher's /mcp
// endpoint, lists the available tools and toolsets, and submits a script via
// execute_code, printing the single combined result.
//
// Usage:
//
//	# after: kubectl port-forward svc/ops-toolset-execute 8080:8080
//	go run ./hack/code-mode-demo \
//	  -endpoint http://localhost:8080/mcp \
//	  -toolset ops-toolset \
//	  -code "var a = tools.call('k8s-mcp','pods_list',{}); return { pods: a.length };"
//
// With no -code, it runs a default two-call composition so you can see one
// request/response returning a combined result. The runner evaluates scripts on
// an ES5.1 engine (goja): tools.call(...) is synchronous, so do NOT use await.
// The default script targets the "k8s-mcp" server from examples/mcptoolset-ops.yaml.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultCode composes two MCP tool calls against the "k8s-mcp" server from
// examples/mcptoolset-ops.yaml and returns a single combined result. The runner
// engine is goja (ES5.1): tools.call(...) is synchronous, so there is no await.
const defaultCode = `
// Compose multiple MCP tool calls; only this final value comes back to the caller.
var pods = tools.call('k8s-mcp', 'pods_list', {});
var ns   = tools.call('k8s-mcp', 'namespaces_list', {});
return { pods: (pods.items ? pods.items.length : pods.length), namespaces: (ns.items ? ns.items.length : ns.length) };
`

func main() {
	endpoint := flag.String("endpoint", "http://localhost:8080/mcp", "dispatcher execute_code MCP endpoint")
	toolset := flag.String("toolset", "ops-toolset", "toolset name to run the code against")
	code := flag.String("code", "", "script to run (defaults to a two-call composition)")
	timeout := flag.Duration("timeout", 60*time.Second, "overall client timeout")
	flag.Parse()

	if *code == "" {
		*code = defaultCode
	}

	if err := run(*endpoint, *toolset, *code, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(endpoint, toolset, code string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "code-mode-demo", Version: "v0.1.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true, MaxRetries: -1}

	fmt.Printf("connecting to %s ...\n", endpoint)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = session.Close() }()

	// 1. Show the tools the dispatcher exposes (expect list_toolsets + execute_code).
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	fmt.Println("\ndispatcher tools:")
	for _, t := range tools.Tools {
		fmt.Printf("  - %s: %s\n", t.Name, t.Description)
	}

	// 2. Discover toolsets (US2 / FR-007).
	if res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_toolsets"}); err == nil {
		fmt.Println("\nlist_toolsets ->")
		printStructured(res)
	} else {
		fmt.Printf("\nlist_toolsets failed (continuing): %v\n", err)
	}

	// 3. The headline: compose multiple tool calls, get one result (US1 / SC-003).
	fmt.Printf("\nexecute_code against toolset %q ...\n", toolset)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "execute_code",
		Arguments: map[string]any{"code": code, "toolset": toolset},
	})
	if err != nil {
		return fmt.Errorf("execute_code: %w", err)
	}
	fmt.Println("execute_code ->")
	printStructured(res)
	return nil
}

// printStructured pretty-prints a tool result's structured content, falling back
// to its text content.
func printStructured(res *mcp.CallToolResult) {
	if res.StructuredContent != nil {
		if b, err := json.MarshalIndent(res.StructuredContent, "  ", "  "); err == nil {
			fmt.Printf("  %s\n", b)
			return
		}
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			fmt.Printf("  %s\n", tc.Text)
		}
	}
}
