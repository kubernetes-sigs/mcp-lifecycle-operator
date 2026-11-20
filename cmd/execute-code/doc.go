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

// Command execute-code is the code-mode dispatcher: a thin data-plane MCP
// server that exposes the list_toolsets and execute_code meta-tools. It claims
// a sandbox directly from the selected SandboxProvider for each execution and
// is deliberately a separate binary from the operator so that it never depends
// on the reconcile loop being on the per-execution hot path (FR-009/SC-006).
//
// The MCP surface is implemented in T013 (execute_code) and T018 (list_toolsets).
package main
