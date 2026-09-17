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

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// fakeManager records Add calls. It embeds manager.Manager (nil) so it satisfies
// the interface; only Add is exercised by these tests.
type fakeManager struct {
	manager.Manager
	added  []manager.Runnable
	addErr error
}

func (f *fakeManager) Add(r manager.Runnable) error {
	f.added = append(f.added, r)
	return f.addErr
}

func TestVerifyCoverageDir(t *testing.T) {
	t.Run("valid writable directory", func(t *testing.T) {
		if err := verifyCoverageDir(t.TempDir()); err != nil {
			t.Fatalf("expected nil error for a writable temp dir, got %v", err)
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		if err := verifyCoverageDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
			t.Fatal("expected an error for a missing directory, got nil")
		}
	})

	t.Run("path is a file, not a directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "afile")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := verifyCoverageDir(file); err == nil {
			t.Fatal("expected an error when path is a file, got nil")
		}
	})

	t.Run("unwritable directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root bypasses directory permissions")
		}
		dir := filepath.Join(t.TempDir(), "readonly")
		if err := os.Mkdir(dir, 0o500); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := verifyCoverageDir(dir); err == nil {
			t.Fatal("expected an error for an unwritable directory, got nil")
		}
	})
}

func TestCoverageRunnable_Unset(t *testing.T) {
	t.Setenv("GOCOVERDIR", "")

	if r, ok := coverageRunnable(); ok || r != nil {
		t.Fatalf("expected no runnable when GOCOVERDIR is unset, got (%v, %v)", r, ok)
	}
}

func TestCoverageRunnable_UnusableDir(t *testing.T) {
	t.Setenv("GOCOVERDIR", filepath.Join(t.TempDir(), "missing"))
	gotCode, called := captureExit(t)

	coverageRunnable()

	if !*called {
		t.Fatal("expected osExit to be called for an unusable GOCOVERDIR")
	}
	if *gotCode != 1 {
		t.Fatalf("expected exit code 1, got %d", *gotCode)
	}
}

func TestCoverageRunnable_NotInstrumented(t *testing.T) {
	t.Setenv("GOCOVERDIR", t.TempDir())
	stubWriteMeta(t, errors.New("not built with -cover"))

	if r, ok := coverageRunnable(); ok || r != nil {
		t.Fatalf("expected no runnable when not instrumented, got (%v, %v)", r, ok)
	}
}

func TestCoverageRunnable_Enabled(t *testing.T) {
	t.Setenv("GOCOVERDIR", t.TempDir())
	stubWriteMeta(t, nil)

	r, ok := coverageRunnable()
	if !ok || r == nil {
		t.Fatal("expected a runnable when instrumented with a usable GOCOVERDIR")
	}
	flusher, isFlusher := r.(*coverageFlusher)
	if !isFlusher {
		t.Fatalf("expected *coverageFlusher, got %T", r)
	}
	if flusher.NeedLeaderElection() {
		t.Fatal("coverage flusher must run on every replica, not only the leader")
	}
}

func TestStartCoverageFlushing_RegistersRunnable(t *testing.T) {
	t.Setenv("GOCOVERDIR", t.TempDir())
	stubWriteMeta(t, nil)

	mgr := &fakeManager{}
	startCoverageFlushing(mgr)

	if len(mgr.added) != 1 {
		t.Fatalf("expected exactly one runnable registered, got %d", len(mgr.added))
	}
}

func TestStartCoverageFlushing_NoopWhenUnset(t *testing.T) {
	t.Setenv("GOCOVERDIR", "")

	mgr := &fakeManager{}
	startCoverageFlushing(mgr)

	if len(mgr.added) != 0 {
		t.Fatalf("expected no runnable registered when GOCOVERDIR is unset, got %d", len(mgr.added))
	}
}

func TestStartCoverageFlushing_ExitsOnAddError(t *testing.T) {
	t.Setenv("GOCOVERDIR", t.TempDir())
	stubWriteMeta(t, nil)
	gotCode, called := captureExit(t)

	startCoverageFlushing(&fakeManager{addErr: errors.New("boom")})

	if !*called || *gotCode != 1 {
		t.Fatalf("expected osExit(1) when mgr.Add fails, called=%v code=%d", *called, *gotCode)
	}
}

func TestCoverageFlusher_StartStopsOnCancel(t *testing.T) {
	c := &coverageFlusher{dir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- c.Start(ctx) }()

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("coverageFlusher.Start did not return after context cancellation")
	}
}

func TestFlushCoverageLoop(t *testing.T) {
	t.Run("flushes on tick and on cancel", func(t *testing.T) {
		var mu sync.Mutex
		var calls int
		flush := func() error {
			mu.Lock()
			calls++
			mu.Unlock()
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); flushCoverageLoop(ctx, time.Millisecond, flush) }()

		time.Sleep(20 * time.Millisecond)
		cancel()
		assertClosed(t, done, "loop should return after cancellation")

		mu.Lock()
		defer mu.Unlock()
		if calls < 2 {
			t.Fatalf("expected at least one tick flush plus the final flush, got %d calls", calls)
		}
	})

	t.Run("logs but keeps going when flush errors", func(t *testing.T) {
		flush := func() error { return errors.New("write failed") }

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); flushCoverageLoop(ctx, time.Millisecond, flush) }()

		time.Sleep(10 * time.Millisecond)
		cancel()
		assertClosed(t, done, "loop should return even when every flush errors")
	})
}

// stubWriteMeta replaces coverageWriteMeta for the duration of the test.
func stubWriteMeta(t *testing.T, ret error) {
	t.Helper()
	restore := coverageWriteMeta
	coverageWriteMeta = func(string) error { return ret }
	t.Cleanup(func() { coverageWriteMeta = restore })
}

// captureExit replaces osExit so a call is recorded instead of terminating the
// test process. It returns pointers to the captured code and whether it fired.
func captureExit(t *testing.T) (*int, *bool) {
	t.Helper()
	var code int
	var called bool
	restore := osExit
	osExit = func(c int) {
		called = true
		code = c
	}
	t.Cleanup(func() { osExit = restore })
	return &code, &called
}

func assertClosed(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
}
