//go:build e2ecoverage

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

// This file is only compiled into the coverage-instrumented manager, built with
// `go build -cover -tags e2ecoverage` (see the Dockerfile coverage target). The
// production binary compiles cmd/coverage_noop.go instead, so none of this code
// - including the runtime/coverage import and the os.Exit below - is present in
// the image that ships to users. See #177.

package main

import (
	"context"
	"fmt"
	"os"
	"runtime/coverage"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// coverageFlushInterval controls how often coverage counters are written to
// GOCOVERDIR while the manager is running.
const coverageFlushInterval = 10 * time.Second

// Overridable seams for testing. Production code always uses the real
// implementations; tests replace them to exercise the os.Exit and
// instrumentation-enabled paths without a coverage-built binary.
var (
	osExit            = os.Exit
	coverageWriteMeta = coverage.WriteMetaDir
)

// startCoverageFlushing registers a coverage-flushing runnable with mgr when
// GOCOVERDIR is set and the binary is coverage-instrumented; it is a no-op
// otherwise. Registering as a manager Runnable means the manager owns the
// goroutine's lifecycle: it is started with the manager and, because
// mgr.Start blocks on all runnables during graceful shutdown, the final flush
// completes before the process exits - without main having to coordinate it.
func startCoverageFlushing(mgr manager.Manager) {
	runnable, ok := coverageRunnable()
	if !ok {
		return
	}
	if err := mgr.Add(runnable); err != nil {
		setupLog.Error(err, "coverage: failed to register flushing runnable")
		osExit(1)
	}
}

// coverageRunnable decides whether coverage flushing should run and returns the
// runnable to register. It is split out from startCoverageFlushing so the
// decision logic is unit-testable without a manager. It exits the process if
// GOCOVERDIR is set but unusable, so a misconfigured mount fails loudly rather
// than silently producing no coverage.
func coverageRunnable() (manager.Runnable, bool) {
	dir := os.Getenv("GOCOVERDIR")
	if dir == "" {
		return nil, false
	}
	// GOCOVERDIR is set, so the coverage deploy is in effect and the directory
	// must be usable.
	if err := verifyCoverageDir(dir); err != nil {
		setupLog.Error(err, "coverage: GOCOVERDIR is set but unusable", "dir", dir)
		osExit(1)
		return nil, false
	}
	// With a known-good directory, a WriteMetaDir failure genuinely means the
	// binary was not built with -cover, so treat that as "coverage not enabled".
	if err := coverageWriteMeta(dir); err != nil {
		setupLog.Info("coverage instrumentation not enabled; skipping counter flushing",
			"dir", dir, "reason", err.Error())
		return nil, false
	}
	setupLog.Info("coverage counter flushing enabled", "dir", dir, "interval", coverageFlushInterval.String())
	return &coverageFlusher{dir: dir}, true
}

// coverageFlusher is a manager.Runnable that flushes coverage counters to its
// directory on an interval and once more when the manager shuts down. A binary
// built with `go build -cover` only emits counters on clean exit, which is
// unhelpful for a long-running controller that is usually killed under E2E.
type coverageFlusher struct {
	dir string
}

// NeedLeaderElection returns false so coverage is collected on every replica,
// not only the elected leader.
func (*coverageFlusher) NeedLeaderElection() bool { return false }

// Start runs the flush loop until ctx is cancelled (on manager shutdown).
func (c *coverageFlusher) Start(ctx context.Context) error {
	flushCoverageLoop(ctx, coverageFlushInterval, func() error { return coverage.WriteCountersDir(c.dir) })
	return nil
}

// flushCoverageLoop flushes once immediately, then on every tick, and once more
// when ctx is cancelled, before returning. The upfront flush guarantees a
// covcounters file exists shortly after startup, so collection works even when
// the E2E run finishes (and coverage is copied out of the still-running pod)
// before the first tick. It is extracted from coverageFlusher.Start so the
// flushing behaviour can be unit-tested with an injected flush function.
func flushCoverageLoop(ctx context.Context, interval time.Duration, flush func() error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if err := flush(); err != nil {
		setupLog.Error(err, "coverage: initial counter flush failed")
	}
	for {
		select {
		case <-ctx.Done():
			if err := flush(); err != nil {
				setupLog.Error(err, "coverage: final counter flush failed")
			}
			return
		case <-ticker.C:
			if err := flush(); err != nil {
				setupLog.Error(err, "coverage: counter flush failed")
			}
		}
	}
}

// verifyCoverageDir checks that dir exists, is a directory, and is writable, so
// that a misconfigured GOCOVERDIR mount fails the coverage deploy up front
// instead of being misreported as a missing-instrumentation no-op.
func verifyCoverageDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat GOCOVERDIR: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("GOCOVERDIR %q is not a directory", dir)
	}
	probe, err := os.CreateTemp(dir, ".coverage-writable-")
	if err != nil {
		return fmt.Errorf("GOCOVERDIR %q is not writable: %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}
