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

import "fmt"

// ErrorType is the typed, caller-distinguishable error taxonomy from the
// execute_code contract (FR-012).
type ErrorType string

const (
	// ErrorTypeScript: the submitted script failed - a syntax/runtime error, or a
	// containment violation (calling a server outside the toolset allow-list).
	ErrorTypeScript ErrorType = "ScriptError"
	// ErrorTypeTimeout: the execution exceeded its time budget and was terminated.
	ErrorTypeTimeout ErrorType = "TimeoutError"
	// ErrorTypeBackend: an MCP tool call could not be completed against a backend
	// server (connection/transport failure), independent of the script's logic.
	ErrorTypeBackend ErrorType = "BackendError"
)

// RunnerError is a typed error carrying one of the contract error types.
type RunnerError struct {
	Type    ErrorType
	Message string
}

func (e *RunnerError) Error() string { return fmt.Sprintf("%s: %s", e.Type, e.Message) }

func scriptError(format string, a ...any) *RunnerError {
	return &RunnerError{Type: ErrorTypeScript, Message: fmt.Sprintf(format, a...)}
}

func timeoutError(format string, a ...any) *RunnerError {
	return &RunnerError{Type: ErrorTypeTimeout, Message: fmt.Sprintf(format, a...)}
}

func backendError(format string, a ...any) *RunnerError {
	return &RunnerError{Type: ErrorTypeBackend, Message: fmt.Sprintf(format, a...)}
}

// Envelope is the JSON the runner writes to stdout: exactly one of Result or
// Error is populated, matching the execute_code output contract.
type Envelope struct {
	Result any            `json:"result,omitempty"`
	Error  *ErrorEnvelope `json:"error,omitempty"`
}

// ErrorEnvelope is the typed error shape returned to the dispatcher.
type ErrorEnvelope struct {
	Type    ErrorType `json:"type"`
	Message string    `json:"message"`
}

// resultEnvelope wraps a successful result.
func resultEnvelope(result any) Envelope {
	return Envelope{Result: result}
}

// errorEnvelope converts any error into the typed error envelope. A RunnerError
// keeps its type; anything else is reported as a BackendError (an unexpected
// failure of the runner harness itself).
func errorEnvelope(err error) Envelope {
	if re, ok := err.(*RunnerError); ok {
		return Envelope{Error: &ErrorEnvelope{Type: re.Type, Message: re.Message}}
	}
	return Envelope{Error: &ErrorEnvelope{Type: ErrorTypeBackend, Message: err.Error()}}
}
