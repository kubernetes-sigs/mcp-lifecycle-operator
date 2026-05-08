//go:build e2e

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

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

func TestMCPHandshake(t *testing.T) {
	const mcpServerPort = 3001

	feature := features.New("MCP handshake with everything-mcp-server").
		WithLabel("type", "mcp").
		WithLabel("component", "mcpserver").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "everything-mcp-server", true)
		}).
		Assess("MCP server pod is running", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			pod := f.FindPodByLabel(ctx, t, cfg, server.Namespace,
				fmt.Sprintf("mcp-server=%s", server.Name))
			t.Logf("MCP server pod %s is Running", pod.Name)
			return ctx
		}).
		Assess("Accepted and Ready conditions are True", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			accepted := f.GetMCPServerCondition(server, "Accepted")
			if accepted == nil || accepted.Status != metav1.ConditionTrue {
				t.Fatal("Accepted condition is not True")
			}

			ready := f.GetMCPServerCondition(server, "Ready")
			if ready == nil || ready.Status != metav1.ConditionTrue {
				t.Fatal("Ready condition is not True")
			}

			f.AssertAddressURL(t, server, mcpServerPort)
			t.Logf("MCPServer status: address=%s, Accepted=True, Ready=True", server.Status.Address.URL)

			return ctx
		}).
		Assess("MCP handshake and tool listing via API server proxy", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)

			httpClient, proxyURL := f.ServiceProxyHTTPClient(t, cfg,
				server.Namespace, server.Name, int(mcpServerPort), "/mcp")

			mcpClient := mcp.NewClient(
				&mcp.Implementation{
					Name:    "e2e-test-client",
					Version: "v0.0.1",
				},
				nil,
			)

			transport := &mcp.StreamableClientTransport{
				Endpoint:   proxyURL,
				HTTPClient: httpClient,
			}

			connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			session, err := mcpClient.Connect(connectCtx, transport, nil)
			if err != nil {
				t.Fatalf("failed to connect MCP client: %v", err)
			}
			defer session.Close()

			initResult := session.InitializeResult()
			if initResult == nil {
				t.Fatal("InitializeResult is nil")
			}
			t.Logf("connected to MCP server: %s (version %s)",
				initResult.ServerInfo.Name, initResult.ServerInfo.Version)

			toolsResult, err := session.ListTools(connectCtx, nil)
			if err != nil {
				t.Fatalf("failed to list MCP tools: %v", err)
			}
			if toolsResult == nil || len(toolsResult.Tools) == 0 {
				t.Fatal("expected the everything-mcp-server to expose at least one tool")
			}

			t.Logf("found %d tools:", len(toolsResult.Tools))
			for _, tool := range toolsResult.Tools {
				t.Logf("  - %s: %s", tool.Name, tool.Description)
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
