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

package sandbox

import (
	"encoding/json"
	"fmt"
)

// Runner wire-contract environment variable names. A provider injects these
// into the sandbox; the runner reads them. Keeping the names here makes the
// provider (producer) and the runner (consumer) share a single source of truth.
const (
	// EnvEndpoints is a JSON array of Endpoint objects: the exhaustive allow-list
	// of servers the sandbox may reach. Any server not in this list is unreachable
	// (containment, FR-005/SC-004).
	EnvEndpoints = "CODEMODE_ENDPOINTS"
	// EnvCode is the submitted script.
	EnvCode = "CODEMODE_CODE"
	// EnvTimeoutSeconds is the per-execution time budget, in seconds.
	EnvTimeoutSeconds = "CODEMODE_TIMEOUT_SECONDS"
	// EnvCredential is the scoped credential value injected into the sandbox.
	// MVP: a single static credential, NOT production grade (see design section 7
	// and the T023 non-production markers).
	EnvCredential = "CODEMODE_CREDENTIAL"
)

// MarshalEndpoints encodes an allow-list of endpoints into the JSON wire format
// carried by EnvEndpoints.
func MarshalEndpoints(endpoints []Endpoint) (string, error) {
	b, err := json.Marshal(endpoints)
	if err != nil {
		return "", fmt.Errorf("marshaling endpoints: %w", err)
	}
	return string(b), nil
}

// UnmarshalEndpoints decodes the EnvEndpoints wire format back into endpoints.
func UnmarshalEndpoints(s string) ([]Endpoint, error) {
	if s == "" {
		return nil, nil
	}
	var endpoints []Endpoint
	if err := json.Unmarshal([]byte(s), &endpoints); err != nil {
		return nil, fmt.Errorf("unmarshaling endpoints: %w", err)
	}
	return endpoints, nil
}
