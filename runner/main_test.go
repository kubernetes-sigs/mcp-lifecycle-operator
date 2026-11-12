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
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// writeInputDir lays out a file-input directory, writing only the files whose
// values are non-nil so absent (optional) inputs can be exercised.
func writeInputDir(t *testing.T, files map[string]*string) string {
	t.Helper()
	dir := t.TempDir()
	for name, val := range files {
		if val == nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(*val), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

// TestEnvFromDirMatchesEnvPath asserts the file-input path reconstructs the same
// runnerEnv as the env-var path given identical inputs.
func TestEnvFromDirMatchesEnvPath(t *testing.T) {
	endpoints := []sandbox.Endpoint{{Server: "ops", URL: "http://ops:8080/mcp"}}
	endpointsJSON, err := sandbox.MarshalEndpoints(endpoints)
	if err != nil {
		t.Fatalf("marshalling endpoints: %v", err)
	}

	dir := writeInputDir(t, map[string]*string{
		fileNameCode:       new("return 42"),
		fileNameEndpoints:  new(endpointsJSON),
		fileNameCredential: new("s3cret"),
		fileNameTimeout:    new("30"),
	})

	got, err := envFromDir(dir)
	if err != nil {
		t.Fatalf("envFromDir: %v", err)
	}

	want := runnerEnv{
		endpoints:  endpointsJSON,
		code:       "return 42",
		timeout:    "30",
		credential: "s3cret",
	}
	if got != want {
		t.Errorf("envFromDir = %+v, want %+v", got, want)
	}
}

// TestEnvFromDirOptionalFilesAbsent confirms a missing optional credential and
// timeout read as "" (mirroring an unset env var), while the required code and
// endpoints are still read.
func TestEnvFromDirOptionalFilesAbsent(t *testing.T) {
	dir := writeInputDir(t, map[string]*string{
		fileNameCode:      new("return 1"),
		fileNameEndpoints: new("[]"),
		// credential and timeout intentionally omitted
	})

	got, err := envFromDir(dir)
	if err != nil {
		t.Fatalf("envFromDir: %v", err)
	}

	if got.credential != "" {
		t.Errorf("credential = %q, want empty", got.credential)
	}
	if got.timeout != "" {
		t.Errorf("timeout = %q, want empty", got.timeout)
	}
	if got.code != "return 1" {
		t.Errorf("code = %q, want %q", got.code, "return 1")
	}
	if got.endpoints != "[]" {
		t.Errorf("endpoints = %q, want %q", got.endpoints, "[]")
	}
}

// TestEnvFromDirMissingCode treats an absent required code file as empty rather
// than erroring; the empty script runs and returns a nil result (parity with the
// env path where an unset CODEMODE_CODE is "").
func TestEnvFromDirMissingCode(t *testing.T) {
	dir := writeInputDir(t, map[string]*string{
		fileNameEndpoints: new("[]"),
	})

	got, err := envFromDir(dir)
	if err != nil {
		t.Fatalf("envFromDir: %v", err)
	}
	if got.code != "" {
		t.Errorf("code = %q, want empty", got.code)
	}
}

// TestMalformedEndpointsSurfacesBackendError confirms that a malformed
// endpoints.json produces a single typed BackendError envelope rather than a
// panic or a silent success.
func TestMalformedEndpointsSurfacesBackendError(t *testing.T) {
	dir := writeInputDir(t, map[string]*string{
		fileNameCode:      new("return 1"),
		fileNameEndpoints: new("{not valid json"),
	})

	env, err := envFromDir(dir)
	if err != nil {
		t.Fatalf("envFromDir: %v", err)
	}

	var buf bytes.Buffer
	env.run(context.Background(), &buf)

	var got Envelope
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decoding envelope: %v (raw=%q)", err, buf.String())
	}
	if got.Error == nil {
		t.Fatalf("expected an error envelope, got result %+v", got.Result)
	}
	if got.Error.Type != ErrorTypeBackend {
		t.Errorf("error type = %q, want %q", got.Error.Type, ErrorTypeBackend)
	}
}

// TestEnvFromDirReadError surfaces a genuine read failure (a directory where a
// file is expected) rather than masking it as empty.
func TestEnvFromDirReadError(t *testing.T) {
	dir := t.TempDir()
	// Make "code" a directory so os.ReadFile returns a non-NotExist error.
	if err := os.Mkdir(filepath.Join(dir, fileNameCode), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := envFromDir(dir); err == nil {
		t.Error("expected a read error when an input file is a directory")
	}
}
