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
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/agentsandbox"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// Dispatcher configuration wire contract. The MCPToolset controller injects
// these into the dispatcher Deployment (one dispatcher per toolset).
const (
	envToolset        = "MCPTOOLSET_NAME"
	envNamespace      = "MCPTOOLSET_NAMESPACE"
	envProvider       = "MCPTOOLSET_PROVIDER"
	envRunnerImage    = "MCPTOOLSET_RUNNER_IMAGE"
	envEndpoints      = "MCPTOOLSET_ENDPOINTS"
	envCredSecret     = "MCPTOOLSET_CREDENTIAL_SECRET"
	envCredKey        = "MCPTOOLSET_CREDENTIAL_KEY"
	envDefaultTimeout = "MCPTOOLSET_DEFAULT_TIMEOUT_SECONDS"
	envListenAddr     = "MCPTOOLSET_LISTEN_ADDR"
)

const (
	defaultListenAddr    = ":8080"
	defaultExecTimeout   = 60 * time.Second
	serverName           = "mcp-code-mode-dispatcher"
	serverVersion        = "v0.1.0"
	mcpPath              = "/mcp"
	readHeaderTimeoutSec = 10
)

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	logger := log.Log.WithName("execute-code")

	d, addr, err := buildDispatcher()
	if err != nil {
		logger.Error(err, "failed to build dispatcher")
		os.Exit(1)
	}

	// The MVP injects a single static, long-lived scoped credential into every
	// sandbox rather than a per-execution brokered identity. This is explicitly
	// NOT production grade: per-execution identity via RFC 8693 token exchange is
	// deferred (see the code-mode design, section 7). Surface it loudly at startup.
	logger.Info("WARNING: code-mode credential model is NOT production grade: "+
		"sandboxes receive a single static scoped credential, not a per-execution "+
		"brokered identity (RFC 8693 token exchange is deferred; see design section 7)",
		"toolset", d.toolset, "credentialSecret", d.credential.SecretName)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		s := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
		d.register(s)
		return s
	}, nil)

	mux := http.NewServeMux()
	mux.Handle(mcpPath, handler)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeoutSec * time.Second,
	}
	logger.Info("serving execute_code", "toolset", d.toolset, "provider", d.provider.Name(), "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		logger.Error(err, "server exited")
		os.Exit(1)
	}
}

// buildDispatcher assembles the dispatcher from the environment and wires the
// agentsandbox provider's call-time half (Claim/Await/Release) directly against
// the API server via the agent-sandbox SDK - no controller involved (SC-006).
func buildDispatcher() (*dispatcher, string, error) {
	providerName := os.Getenv(envProvider)
	provider, ok := sandbox.Get(providerName)
	if !ok {
		return nil, "", fmt.Errorf("unknown provider %q (registered: %v)", providerName, sandbox.Names())
	}

	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, "", fmt.Errorf("loading kube config: %w", err)
	}
	// Wire the agentsandbox provider's call-time dependency. It builds the
	// agent-sandbox SDK client and the credential Secret reader from this config
	// on first use; there is no controller on the execution hot path.
	agentsandbox.SetConfig(cfg)

	endpoints, err := sandbox.UnmarshalEndpoints(os.Getenv(envEndpoints))
	if err != nil {
		return nil, "", fmt.Errorf("parsing endpoints: %w", err)
	}

	d := &dispatcher{
		toolset:        os.Getenv(envToolset),
		provider:       provider,
		namespace:      os.Getenv(envNamespace),
		runnerImage:    os.Getenv(envRunnerImage),
		credential:     sandbox.CredentialRef{SecretName: os.Getenv(envCredSecret), Key: os.Getenv(envCredKey)},
		endpoints:      endpoints,
		defaultTimeout: parseTimeoutSeconds(os.Getenv(envDefaultTimeout), defaultExecTimeout),
	}

	addr := os.Getenv(envListenAddr)
	if addr == "" {
		addr = defaultListenAddr
	}
	return d, addr, nil
}

// parseTimeoutSeconds parses a seconds string, falling back to def when empty or
// invalid.
func parseTimeoutSeconds(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	secs, err := strconv.Atoi(s)
	if err != nil || secs <= 0 {
		return def
	}
	return time.Duration(secs) * time.Second
}
