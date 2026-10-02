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
	"errors"
	"testing"
)

func TestRunnerErrorError(t *testing.T) {
	err := scriptError("server %q not allowed", "vault")
	if got, want := err.Error(), `ScriptError: server "vault" not allowed`; got != want {
		t.Errorf("Error() = %q; want %q", got, want)
	}
}

// TestErrorEnvelopeKeepsRunnerType confirms a typed RunnerError is carried
// through to the envelope unchanged.
func TestErrorEnvelopeKeepsRunnerType(t *testing.T) {
	env := errorEnvelope(timeoutError("deadline exceeded"))
	if env.Error == nil {
		t.Fatal("envelope has no error")
	}
	if env.Error.Type != ErrorTypeTimeout {
		t.Errorf("type = %q; want %q", env.Error.Type, ErrorTypeTimeout)
	}
	if env.Error.Message != "deadline exceeded" {
		t.Errorf("message = %q; want %q", env.Error.Message, "deadline exceeded")
	}
	if env.Result != nil {
		t.Errorf("result = %v; want nil on an error envelope", env.Result)
	}
}

// TestErrorEnvelopePlainErrorIsBackend confirms a non-RunnerError (an unexpected
// harness failure) is reported as a BackendError rather than losing its type.
func TestErrorEnvelopePlainErrorIsBackend(t *testing.T) {
	env := errorEnvelope(errors.New("boom"))
	if env.Error == nil {
		t.Fatal("envelope has no error")
	}
	if env.Error.Type != ErrorTypeBackend {
		t.Errorf("type = %q; want %q", env.Error.Type, ErrorTypeBackend)
	}
	if env.Error.Message != "boom" {
		t.Errorf("message = %q; want %q", env.Error.Message, "boom")
	}
}
