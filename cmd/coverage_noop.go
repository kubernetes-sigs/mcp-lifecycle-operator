//go:build !e2ecoverage

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

// This is the production stub for coverage flushing. The real implementation
// lives in cmd/coverage.go and is compiled only into the coverage-instrumented
// manager (-tags e2ecoverage). Keeping it out of the default build means the
// production controller carries no runtime/coverage import and no coverage code
// path at all, regardless of environment variables. See #177.

package main

import "sigs.k8s.io/controller-runtime/pkg/manager"

// startCoverageFlushing is a no-op in the production build.
func startCoverageFlushing(_ manager.Manager) {}
