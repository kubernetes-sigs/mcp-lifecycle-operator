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
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

type pingOut struct {
	Pong bool `json:"pong"`
}

// newTestMCPServer starts an in-process MCP server over HTTP exposing a "ping"
// tool (structured success) and a "boom" tool (tool-level error). It records the
// Authorization header of the most recent request so bearer-token propagation can
// be asserted.
func newTestMCPServer(t *testing.T) (url string, lastAuth func() string) {
	t.Helper()

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		s := mcp.NewServer(&mcp.Implementation{Name: "test-backend", Version: "v0"}, nil)
		mcp.AddTool(s, &mcp.Tool{Name: "ping"},
			func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, pingOut, error) {
				return nil, pingOut{Pong: true}, nil
			})
		mcp.AddTool(s, &mcp.Tool{Name: "boom"},
			func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "backend says no"}},
				}, nil, nil
			})
		return s
	}, nil)

	var mu sync.Mutex
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv.URL, func() string {
		mu.Lock()
		defer mu.Unlock()
		return auth
	}
}

func TestMCPToolCallerCallSuccessWithBearer(t *testing.T) {
	url, lastAuth := newTestMCPServer(t)

	caller := newMCPToolCaller([]sandbox.Endpoint{{Server: "backend", URL: url}}, "tok-123")
	defer caller.closeAll()

	res, err := caller.Call(context.Background(), "backend", "ping", map[string]any{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	out, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T; want structured object", res)
	}
	if out["pong"] != true {
		t.Errorf("result = %v; want pong=true", out)
	}
	if got := lastAuth(); got != "Bearer tok-123" {
		t.Errorf("Authorization = %q; want %q", got, "Bearer tok-123")
	}
}

func TestMCPToolCallerNoCredentialSendsNoBearer(t *testing.T) {
	url, lastAuth := newTestMCPServer(t)

	caller := newMCPToolCaller([]sandbox.Endpoint{{Server: "backend", URL: url}}, "")
	defer caller.closeAll()

	if _, err := caller.Call(context.Background(), "backend", "ping", map[string]any{}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got := lastAuth(); got != "" {
		t.Errorf("Authorization = %q; want empty when no credential is set", got)
	}
}

func TestMCPToolCallerToolErrorIsScriptError(t *testing.T) {
	url, _ := newTestMCPServer(t)

	caller := newMCPToolCaller([]sandbox.Endpoint{{Server: "backend", URL: url}}, "")
	defer caller.closeAll()

	_, err := caller.Call(context.Background(), "backend", "boom", map[string]any{})
	var re *RunnerError
	if !errors.As(err, &re) || re.Type != ErrorTypeScript {
		t.Fatalf("err = %v; want ScriptError", err)
	}
}

func TestMCPToolCallerUnknownServerIsBackendError(t *testing.T) {
	caller := newMCPToolCaller([]sandbox.Endpoint{{Server: "backend", URL: "http://unused"}}, "")
	defer caller.closeAll()

	_, err := caller.Call(context.Background(), "not-in-allow-list", "ping", map[string]any{})
	var re *RunnerError
	if !errors.As(err, &re) || re.Type != ErrorTypeBackend {
		t.Fatalf("err = %v; want BackendError", err)
	}
}

func TestMCPToolCallerCloseAllIdempotent(t *testing.T) {
	url, _ := newTestMCPServer(t)
	caller := newMCPToolCaller([]sandbox.Endpoint{{Server: "backend", URL: url}}, "")

	if _, err := caller.Call(context.Background(), "backend", "ping", map[string]any{}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	caller.closeAll()
	caller.closeAll() // second call must not panic on an already-closed session set
}

func TestDecodeResult(t *testing.T) {
	cases := []struct {
		name string
		res  *mcp.CallToolResult
		want any
	}{
		{
			name: "structured content preferred",
			res:  &mcp.CallToolResult{StructuredContent: map[string]any{"a": float64(1)}},
			want: map[string]any{"a": float64(1)},
		},
		{
			name: "json text is parsed",
			res:  &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"b":2}`}}},
			want: map[string]any{"b": float64(2)},
		},
		{
			name: "non-json text returned raw",
			res:  &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "hello"}}},
			want: "hello",
		},
		{
			name: "empty content is nil",
			res:  &mcp.CallToolResult{},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeResult(tc.res)
			if m, ok := tc.want.(map[string]any); ok {
				gm, ok := got.(map[string]any)
				if !ok {
					t.Fatalf("got %T; want map", got)
				}
				for k, v := range m {
					if gm[k] != v {
						t.Errorf("key %q = %v; want %v", k, gm[k], v)
					}
				}
				return
			}
			if got != tc.want {
				t.Errorf("decodeResult = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestContentText(t *testing.T) {
	content := []mcp.Content{
		&mcp.TextContent{Text: "foo"},
		&mcp.ImageContent{Data: []byte("ignored")},
		&mcp.TextContent{Text: "bar"},
	}
	if got := contentText(content); got != "foobar" {
		t.Errorf("contentText = %q; want %q", got, "foobar")
	}
	if got := contentText(nil); got != "" {
		t.Errorf("contentText(nil) = %q; want empty", got)
	}
}
