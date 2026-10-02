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

package agentsandbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// fakeSandbox is a liveSandbox test double recording writes and canned Run output.
type fakeSandbox struct {
	claimName string
	writes    map[string][]byte
	runOut    string
	runErr    error
	closed    bool
}

func (f *fakeSandbox) ClaimName() string { return f.claimName }

func (f *fakeSandbox) Write(_ context.Context, path string, content []byte) error {
	if f.writes == nil {
		f.writes = map[string][]byte{}
	}
	f.writes[path] = content
	return nil
}

func (f *fakeSandbox) Run(context.Context, string) (string, error) { return f.runOut, f.runErr }

func (f *fakeSandbox) Close(context.Context) error { f.closed = true; return nil }

// fakeFactory is a sandboxFactory test double.
type fakeFactory struct {
	sandbox     *fakeSandbox
	createErr   error
	gotWarmPool string
	gotNS       string
}

func (f *fakeFactory) CreateSandbox(_ context.Context, warmPoolName, namespace string) (liveSandbox, error) {
	f.gotWarmPool = warmPoolName
	f.gotNS = namespace
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.sandbox, nil
}

func (f *fakeFactory) GetSandbox(context.Context, string, string) (liveSandbox, error) {
	return f.sandbox, nil
}

func (f *fakeFactory) DeleteSandbox(context.Context, string, string) error { return nil }

func newTestProvider(fac sandboxFactory) *Provider {
	return &Provider{factory: fac, live: map[string]liveSandbox{}}
}

func TestClaimWritesInputFiles(t *testing.T) {
	fs := &fakeSandbox{claimName: "codemode-ops-abc123"}
	fac := &fakeFactory{sandbox: fs}
	p := newTestProvider(fac)

	endpoints := []sandbox.Endpoint{{Server: "ops", URL: "http://ops:8080/mcp"}}
	endpointsJSON, err := sandbox.MarshalEndpoints(endpoints)
	if err != nil {
		t.Fatalf("marshalling endpoints: %v", err)
	}

	h, err := p.Claim(context.Background(), sandbox.RunnerSpec{
		Namespace:        "team-a",
		Toolset:          "ops",
		AllowedEndpoints: endpoints,
		Code:             "return 42",
		Timeout:          60 * time.Second,
	})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if fac.gotWarmPool != "codemode-ops" {
		t.Errorf("warm pool name = %q, want %q", fac.gotWarmPool, "codemode-ops")
	}
	if fac.gotNS != "team-a" {
		t.Errorf("namespace = %q, want %q", fac.gotNS, "team-a")
	}
	if h.Ref != fs.claimName {
		t.Errorf("handle Ref = %q, want %q", h.Ref, fs.claimName)
	}
	if h.Namespace != "team-a" {
		t.Errorf("handle Namespace = %q, want %q", h.Namespace, "team-a")
	}

	wantFiles := map[string]string{
		fileCode:      "return 42",
		fileEndpoints: endpointsJSON,
		fileTimeout:   "60",
	}
	for path, want := range wantFiles {
		got, ok := fs.writes[path]
		if !ok {
			t.Errorf("expected write to %s", path)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if _, ok := fs.writes[fileCredential]; ok {
		t.Errorf("credential file written when no credential configured")
	}

	// The live handle is tracked under the claim name for Await/Release.
	if p.live[fs.claimName] != fs {
		t.Errorf("live handle not tracked under claim name")
	}
}

func TestClaimWritesCredential(t *testing.T) {
	fs := &fakeSandbox{claimName: "codemode-ops-xyz"}
	p := newTestProvider(&fakeFactory{sandbox: fs})
	p.readSecret = func(_ context.Context, namespace, name, key string) ([]byte, error) {
		if namespace != "team-a" || name != "ops-cred" || key != "token" {
			t.Errorf("readSecret called with (%q,%q,%q)", namespace, name, key)
		}
		return []byte("s3cret"), nil
	}

	_, err := p.Claim(context.Background(), sandbox.RunnerSpec{
		Namespace:  "team-a",
		Toolset:    "ops",
		Code:       "x",
		Timeout:    30 * time.Second,
		Credential: sandbox.CredentialRef{SecretName: "ops-cred", Key: "token"},
	})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got := string(fs.writes[fileCredential]); got != "s3cret" {
		t.Errorf("credential file = %q, want %q", got, "s3cret")
	}
}

func TestClaimCredentialErrorClosesSandbox(t *testing.T) {
	fs := &fakeSandbox{claimName: "c1"}
	p := newTestProvider(&fakeFactory{sandbox: fs})
	p.readSecret = func(context.Context, string, string, string) ([]byte, error) {
		return nil, errors.New("boom")
	}

	_, err := p.Claim(context.Background(), sandbox.RunnerSpec{
		Namespace:  "ns",
		Toolset:    "ops",
		Timeout:    time.Second,
		Credential: sandbox.CredentialRef{SecretName: "cred"},
	})
	if err == nil {
		t.Fatal("expected error when credential read fails")
	}
	if !fs.closed {
		t.Error("sandbox not closed after credential failure")
	}
	if len(p.live) != 0 {
		t.Error("failed claim should not track a live handle")
	}
}

func TestAwaitReturnsStdoutVerbatim(t *testing.T) {
	envelope := `{"result":{"answer":42}}`
	fs := &fakeSandbox{claimName: "c1", runOut: envelope}
	p := newTestProvider(&fakeFactory{sandbox: fs})
	p.live["c1"] = fs

	out, err := p.Await(context.Background(), sandbox.RunnerHandle{Ref: "c1"})
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if string(out) != envelope {
		t.Errorf("Await = %q, want %q", out, envelope)
	}
}

func TestAwaitMissingHandle(t *testing.T) {
	p := newTestProvider(&fakeFactory{})
	if _, err := p.Await(context.Background(), sandbox.RunnerHandle{Ref: "nope"}); err == nil {
		t.Fatal("expected error for missing live sandbox")
	}
}

func TestRelease(t *testing.T) {
	fs := &fakeSandbox{claimName: "c1"}
	p := newTestProvider(&fakeFactory{})
	p.live["c1"] = fs

	if err := p.Release(context.Background(), sandbox.RunnerHandle{Ref: "c1"}); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if !fs.closed {
		t.Error("sandbox not closed on Release")
	}
	if _, ok := p.live["c1"]; ok {
		t.Error("handle not dropped from live map")
	}
	// Releasing an already-released / unknown handle is a clean no-op.
	if err := p.Release(context.Background(), sandbox.RunnerHandle{Ref: "c1"}); err != nil {
		t.Errorf("second Release: %v", err)
	}
	if err := p.Release(context.Background(), sandbox.RunnerHandle{}); err != nil {
		t.Errorf("empty-ref Release: %v", err)
	}
}

func TestRegistryAndIsolation(t *testing.T) {
	// The package init() registers the singleton under ProviderName.
	p, ok := sandbox.Get(ProviderName)
	if !ok {
		t.Fatalf("provider %q not registered", ProviderName)
	}
	if p.Name() != ProviderName {
		t.Errorf("Name() = %q, want %q", p.Name(), ProviderName)
	}
	g := p.IsolationGuarantees()
	if !g.Satisfies("baseline") {
		t.Error("expected baseline isolation to be satisfied")
	}
	if !g.Satisfies("") {
		t.Error("expected empty (baseline) isolation to be satisfied")
	}
	if g.Satisfies("egress") {
		t.Error("egress isolation must not be satisfied in the MVP")
	}
	if g.CredentialScoping != sandbox.CredentialScopingStatic {
		t.Errorf("CredentialScoping = %q, want static", g.CredentialScoping)
	}
}
