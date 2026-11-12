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

// Command runner is the portable code-mode data-plane runner. It runs inside a
// provider-provisioned sandbox, reads the allow-listed MCP endpoints, the scoped
// credential (MVP: static, non-production), the submitted code, and the timeout,
// exposes a tools.call(server, tool, args) surface (plus tools.<server>.<tool>()
// sugar) that forwards to MCP tools/call against allow-listed endpoints only,
// runs the script, and returns only the final result or a typed error.
//
// The same runner image runs unchanged under any SandboxProvider (portability
// contract). Its behaviour is implemented in T011.
package main
