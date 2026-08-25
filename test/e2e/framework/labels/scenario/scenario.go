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

// Package scenario provides the optional third label dimension for e2e tests.
// Scenarios slice larger categories (lifecycle, configuration, resilience) into
// finer groups. Categories with few tests (networking, observability) intentionally
// omit scenario labels.
package scenario

const (
	Label = "scenario"

	// Lifecycle scenarios.
	Deploy     = "deploy"
	SpecUpdate = "spec-update"
	Drift      = "drift"
	Ownership  = "ownership"

	// Configuration scenarios.
	Storage  = "storage"
	Port     = "port"
	Security = "security"
	Metadata = "metadata"

	// BYO scenarios.
	BYO = "byo"

	// Resilience scenarios.
	Failure  = "failure"
	Recovery = "recovery"
)
