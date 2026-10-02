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
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// defaultTimeout bounds a single execution when the sandbox does not supply one.
const defaultTimeout = 60 * time.Second

// envInputDir selects file-input mode (also settable via --input-dir).
const envInputDir = "CODEMODE_INPUT_DIR"

// Input file names used in file-input mode. Their contents mirror the env-var
// wire contract in runner_contract.go.
const (
	fileNameCode       = "code"
	fileNameEndpoints  = "endpoints.json"
	fileNameCredential = "credential"
	fileNameTimeout    = "timeout"
)

func main() {
	var inputDir string
	flag.StringVar(&inputDir, "input-dir", os.Getenv(envInputDir),
		"directory to read execution inputs from (file-input mode); env-var mode when empty")
	flag.Parse()

	if inputDir != "" {
		// File-input mode: the sandbox provider wrote the inputs as files (the
		// agent-sandbox SDK has no per-exec env/stdin channel).
		env, err := envFromDir(inputDir)
		if err != nil {
			writeEnvelope(os.Stdout, errorEnvelope(backendError("reading input dir %q: %v", inputDir, err)))
			return
		}
		env.run(context.Background(), os.Stdout)
		return
	}

	// Env-var mode: local/standalone runs and the existing test path.
	env := runnerEnv{
		endpoints:  os.Getenv(sandbox.EnvEndpoints),
		code:       os.Getenv(sandbox.EnvCode),
		timeout:    os.Getenv(sandbox.EnvTimeoutSeconds),
		credential: os.Getenv(sandbox.EnvCredential),
	}

	env.run(context.Background(), os.Stdout)
}

// envFromDir reads the per-execution inputs from files in dir. Absent files
// (notably the optional credential and timeout) read as empty, matching env-var
// mode where an unset variable is "". A genuine read error is returned.
func envFromDir(dir string) (runnerEnv, error) {
	code, err := readInputFile(dir, fileNameCode)
	if err != nil {
		return runnerEnv{}, err
	}
	endpoints, err := readInputFile(dir, fileNameEndpoints)
	if err != nil {
		return runnerEnv{}, err
	}
	credential, err := readInputFile(dir, fileNameCredential)
	if err != nil {
		return runnerEnv{}, err
	}
	timeout, err := readInputFile(dir, fileNameTimeout)
	if err != nil {
		return runnerEnv{}, err
	}
	return runnerEnv{endpoints: endpoints, code: code, timeout: timeout, credential: credential}, nil
}

// readInputFile reads dir/name, treating a missing file as empty.
func readInputFile(dir, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
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

	writeEnvelope(out, env)
}

// writeEnvelope writes exactly one JSON envelope to out, falling back to an error
// envelope if the result itself fails to marshal (a backend/harness failure).
func writeEnvelope(out io.Writer, env Envelope) {
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
