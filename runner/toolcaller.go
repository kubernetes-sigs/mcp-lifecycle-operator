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
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// ToolCaller forwards a single MCP tools/call to a named server and returns the
// decoded result. Implementations enforce nothing about the allow-list; the
// engine performs containment before dispatching here.
type ToolCaller interface {
	Call(ctx context.Context, server, tool string, args map[string]any) (any, error)
}

const (
	runnerClientName    = "mcp-code-mode-runner"
	runnerClientVersion = "v0.1.0"
)

// mcpToolCaller forwards tool calls to allow-listed MCPServer endpoints over the
// MCP protocol using the go-sdk client. Sessions are established lazily and
// reused across calls within a single execution.
type mcpToolCaller struct {
	endpoints  map[string]string // server name -> URL
	credential string            // static scoped credential (MVP, non-production)

	mu       sync.Mutex
	sessions map[string]*mcp.ClientSession
}

// newMCPToolCaller builds a caller for the given allow-listed endpoints. The
// credential, when non-empty, is sent as a bearer token on every backend call.
func newMCPToolCaller(endpoints []sandbox.Endpoint, credential string) *mcpToolCaller {
	m := &mcpToolCaller{
		endpoints:  make(map[string]string, len(endpoints)),
		credential: credential,
		sessions:   make(map[string]*mcp.ClientSession),
	}
	for _, e := range endpoints {
		m.endpoints[e.Server] = e.URL
	}
	return m
}

// Call forwards to MCP tools/call on the named server. A connection or transport
// failure is reported as a BackendError; a tool that reports its own failure is
// surfaced as a ScriptError so the script author sees it as a logic error.
func (m *mcpToolCaller) Call(ctx context.Context, server, tool string, args map[string]any) (any, error) {
	session, err := m.session(ctx, server)
	if err != nil {
		return nil, backendError("connecting to server %q: %v", server, err)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, backendError("calling %s/%s: %v", server, tool, err)
	}
	if res.IsError {
		return nil, scriptError("tool %s/%s returned an error: %s", server, tool, contentText(res.Content))
	}

	return decodeResult(res), nil
}

// session returns a connected session for the server, dialing on first use.
func (m *mcpToolCaller) session(ctx context.Context, server string) (*mcp.ClientSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[server]; ok {
		return s, nil
	}

	url, ok := m.endpoints[server]
	if !ok {
		// Should not happen: the engine enforces containment before dispatching.
		return nil, fmt.Errorf("server %q is not in the allow-list", server)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: runnerClientName, Version: runnerClientVersion}, nil)
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if m.credential != "" {
		httpClient.Transport = &bearerTransport{token: m.credential, base: http.DefaultTransport}
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	m.sessions[server] = session
	return session, nil
}

// closeAll tears down every open session. Safe to call more than once.
func (m *mcpToolCaller) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, s := range m.sessions {
		_ = s.Close()
		delete(m.sessions, name)
	}
}

// bearerTransport attaches the static scoped credential (MVP, non-production) as
// an Authorization bearer header to every backend request.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone so we do not mutate the caller's request.
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r)
}

// decodeResult converts an MCP tool result into a plain Go value for the script.
// Structured content is preferred; otherwise the concatenated text content is
// returned, decoded as JSON when it parses and as a raw string when it does not.
func decodeResult(res *mcp.CallToolResult) any {
	if res.StructuredContent != nil {
		return res.StructuredContent
	}
	text := contentText(res.Content)
	if text == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(text), &v); err == nil {
		return v
	}
	return text
}

// contentText concatenates the text of all TextContent parts.
func contentText(content []mcp.Content) string {
	var b strings.Builder
	for _, c := range content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
