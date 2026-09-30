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
	"io"
	"os"
	"strconv"
	"time"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// defaultTimeout bounds a single execution when the sandbox does not supply one.
const defaultTimeout = 60 * time.Second

func main() {
	env := runnerEnv{
		endpoints:  os.Getenv(sandbox.EnvEndpoints),
		code:       os.Getenv(sandbox.EnvCode),
		timeout:    os.Getenv(sandbox.EnvTimeoutSeconds),
		credential: os.Getenv(sandbox.EnvCredential),
	}

	env.run(context.Background(), os.Stdout)
}

// runnerEnv is the sandbox-injected input, kept as a struct so run is testable
// without touching the process environment.
type runnerEnv struct {
	endpoints  string
	code       string
	timeout    string
	credential string
}

// run parses the injected input, executes the script, and writes exactly one
// result-or-error JSON envelope to out (the single-result contract, SC-002).
func (e runnerEnv) run(ctx context.Context, out io.Writer) {
	var env Envelope
	result, err := execute(ctx, e)
	if err != nil {
		env = errorEnvelope(err)
	} else {
		env = resultEnvelope(result)
	}

	// A marshalling failure of the result itself is a backend/harness failure.
	if data, mErr := json.Marshal(env); mErr == nil {
		_, _ = out.Write(append(data, '\n'))
	} else {
		fallback, _ := json.Marshal(errorEnvelope(backendError("encoding result: %v", mErr)))
		_, _ = out.Write(append(fallback, '\n'))
	}
}

// execute wires the tool caller and engine together and runs the code.
func execute(ctx context.Context, e runnerEnv) (any, error) {
	endpoints, err := sandbox.UnmarshalEndpoints(e.endpoints)
	if err != nil {
		return nil, backendError("parsing endpoints: %v", err)
	}

	servers := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		servers = append(servers, ep.Server)
	}

	caller := newMCPToolCaller(endpoints, e.credential)
	defer caller.closeAll()

	engine, err := newEngine(caller, servers)
	if err != nil {
		return nil, backendError("initializing engine: %v", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, parseTimeout(e.timeout))
	defer cancel()

	return engine.Run(runCtx, e.code)
}

// parseTimeout reads the injected timeout (seconds); a missing or invalid value
// falls back to defaultTimeout.
func parseTimeout(s string) time.Duration {
	if s == "" {
		return defaultTimeout
	}
	secs, err := strconv.Atoi(s)
	if err != nil || secs <= 0 {
		return defaultTimeout
	}
	return time.Duration(secs) * time.Second
}
