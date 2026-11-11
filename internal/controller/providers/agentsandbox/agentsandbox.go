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

// Package agentsandbox implements the "agentsandbox" SandboxProvider on top of
// sigs.k8s.io/agent-sandbox. It is the sole code-mode execution backend.
//
// The provider spans two processes, exactly like the interface it satisfies:
//   - Setup/Teardown run in the operator and reconcile long-lived per-toolset
//     backend state (a SandboxTemplate + SandboxWarmPool) via a controller-runtime
//     client. See setup.go.
//   - Claim/Await/Release run in the execute_code dispatcher and drive one
//     execution through the agent-sandbox Go SDK (reached behind the fakeable
//     seam in sdk.go). They do not re-enter the controller (FR-009/SC-006).
//
// The two processes share no memory; each wires what it needs through the
// package-level setters (SetConfig / SetControllerClient / SetRunnerImage),
// mirroring the pod provider's SetClient pattern.
package agentsandbox

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// ProviderName is the value spec.provider must carry to select this provider.
const ProviderName = "agentsandbox"

const (
	// inputDir is the writable directory (an emptyDir in the sandbox pod) the
	// dispatcher writes per-execution inputs into and the runner reads from. It is
	// also sandboxd's --root-dir (see setup.go), so SDK Write paths are relative to
	// it and the runner reads the same absolute path.
	inputDir = "/codemode"
	// runnerCommand is the command Run executes inside the sandbox. The runner's
	// file-input mode reads code/endpoints/credential/timeout from inputDir.
	runnerCommand = "/runner --input-dir=" + inputDir

	// Per-execution input files, written before the runner is invoked. These are
	// relative to sandboxd's root-dir (inputDir): the sandboxd filesystem API
	// confines every write under root-dir, so an absolute "/codemode/code" would be
	// re-rooted to "/codemode/codemode/code". The names mirror the env-var wire
	// contract in runner_contract.go and the runner's own file-name constants.
	fileCode       = "code"
	fileEndpoints  = "endpoints.json"
	fileCredential = "credential"
	fileTimeout    = "timeout"

	// defaultCredentialKey is the Secret key used when CredentialRef.Key is empty.
	defaultCredentialKey = "credential"

	// claimShutdownGrace is added to a claim's execution timeout to derive its
	// server-side expiry (WithShutdownAfter). It leaves room for scheduling, the
	// runner's own work, and log retrieval while still guaranteeing the claim is
	// reaped if the dispatcher dies before it can Release the sandbox.
	claimShutdownGrace = 5 * time.Minute
)

// secretReader reads a single value from a Secret. It is abstracted so the
// client-go dependency stays injectable and the credential path is unit testable
// without a cluster.
type secretReader func(ctx context.Context, namespace, name, key string) ([]byte, error)

// Provider is the agentsandbox SandboxProvider. It holds operator-side state
// (controllerClient, runnerImage) and dispatcher-side state (cfg, factory, the
// live-handle map), wired per process at startup.
type Provider struct {
	cfg              *rest.Config
	controllerClient client.Client
	runnerImage      string

	// readSecret fetches the scoped credential value at call time. Defaults to a
	// client-go implementation built lazily from cfg; overridable in tests.
	readSecret secretReader

	mu      sync.Mutex
	factory sandboxFactory
	// live maps a claim name (RunnerHandle.Ref) to its open sandbox. The SDK's
	// *Sandbox is a live, connected object, so Claim stores it here for Await and
	// Release in later dispatcher calls.
	live map[string]liveSandbox
}

// singleton is the instance registered in the sandbox registry. It is wired at
// process startup via the SetConfig/SetControllerClient/SetRunnerImage setters.
var singleton = &Provider{live: map[string]liveSandbox{}}

func init() {
	sandbox.Register(singleton)
}

// SetConfig wires the REST config used by both processes: the operator's Setup
// builds a controller-runtime client from it elsewhere, and the dispatcher builds
// the agent-sandbox SDK client (and the Secret reader) from it.
func SetConfig(cfg *rest.Config) { singleton.cfg = cfg }

// SetControllerClient wires the reconcile-time client used by Setup/Teardown.
// Only the operator calls this.
func SetControllerClient(c client.Client) { singleton.controllerClient = c }

// SetRunnerImage wires the sandbox image baked into the Template at Setup time.
// Only the operator calls this.
func SetRunnerImage(img string) { singleton.runnerImage = img }

// Name implements sandbox.SandboxProvider.
func (p *Provider) Name() string { return ProviderName }

// IsolationGuarantees implements sandbox.SandboxProvider. agentsandbox gives a
// dedicated, filesystem-isolated sandbox pod per execution, torn down on Release.
// The Template carries a NetworkPolicy that admits the dispatcher and allows all
// egress (see networkPolicySpec), so egress is deliberately not controlled in the
// MVP; the credential is the static, non-production MVP model.
func (p *Provider) IsolationGuarantees() sandbox.IsolationGuarantees {
	return sandbox.IsolationGuarantees{
		FilesystemIsolated:    true,
		PerExecutionEphemeral: true,
		EgressControlled:      false,
		CredentialScoping:     sandbox.CredentialScopingStatic,
	}
}

// Claim provisions a fresh sandbox for one execution. CreateSandbox opens the
// sandbox internally, so Claim only writes the per-execution input files and
// records the live handle. It returns a handle keyed by the claim name.
func (p *Provider) Claim(ctx context.Context, spec sandbox.RunnerSpec) (sandbox.RunnerHandle, error) {
	factory, err := p.getFactory()
	if err != nil {
		return sandbox.RunnerHandle{}, err
	}

	endpointsJSON, err := sandbox.MarshalEndpoints(spec.AllowedEndpoints)
	if err != nil {
		return sandbox.RunnerHandle{}, err
	}

	sb, err := factory.CreateSandbox(ctx, warmPoolName(spec.Toolset), spec.Namespace, spec.Timeout+claimShutdownGrace)
	if err != nil {
		return sandbox.RunnerHandle{}, fmt.Errorf("agentsandbox provider: creating sandbox: %w", err)
	}

	timeoutSeconds := strconv.FormatInt(int64(spec.Timeout.Seconds()), 10)
	inputs := []struct {
		path    string
		content []byte
	}{
		{fileCode, []byte(spec.Code)},
		{fileEndpoints, []byte(endpointsJSON)},
		{fileTimeout, []byte(timeoutSeconds)},
	}
	for _, in := range inputs {
		if err := sb.Write(ctx, in.path, in.content); err != nil {
			_ = sb.Close(ctx)
			return sandbox.RunnerHandle{}, fmt.Errorf("agentsandbox provider: writing %s: %w", in.path, err)
		}
	}

	// The static scoped credential (MVP, non-production) is read from its Secret
	// and written into the sandbox. Unlike the pod provider's secretKeyRef mount,
	// the value transits the dispatcher here; tightening this (CredentialRef on
	// ToolsetContext + a secret-volume on the Template, or brokered scoping) is a
	// post-MVP follow-up.
	if spec.Credential.SecretName != "" {
		key := spec.Credential.Key
		if key == "" {
			key = defaultCredentialKey
		}
		val, err := p.readCredential(ctx, spec.Namespace, spec.Credential.SecretName, key)
		if err != nil {
			_ = sb.Close(ctx)
			return sandbox.RunnerHandle{}, err
		}
		if err := sb.Write(ctx, fileCredential, val); err != nil {
			_ = sb.Close(ctx)
			return sandbox.RunnerHandle{}, fmt.Errorf("agentsandbox provider: writing %s: %w", fileCredential, err)
		}
	}

	claimName := sb.ClaimName()
	p.mu.Lock()
	p.live[claimName] = sb
	p.mu.Unlock()

	return sandbox.RunnerHandle{Namespace: spec.Namespace, Ref: claimName}, nil
}

// Await runs the runner in the claimed sandbox and returns its stdout verbatim -
// the runner's JSON result envelope, which is exactly the contract the dispatcher
// expects. It does not release the sandbox.
func (p *Provider) Await(ctx context.Context, h sandbox.RunnerHandle) ([]byte, error) {
	p.mu.Lock()
	sb := p.live[h.Ref]
	p.mu.Unlock()
	if sb == nil {
		return nil, fmt.Errorf("agentsandbox provider: no live sandbox for %q", h.Ref)
	}

	stdout, err := sb.Run(ctx, runnerCommand)
	if err != nil {
		return nil, fmt.Errorf("agentsandbox provider: running runner in %q: %w", h.Ref, err)
	}
	return []byte(stdout), nil
}

// Release closes the sandbox (tearing it down and deleting its claim) and drops
// the live handle. A missing handle is treated as already released.
//
// The handle is removed from the live map up front so a concurrent or repeated
// Release of the same ref cannot double-close; if Close then fails, the handle is
// re-inserted so a subsequent Release can retry cleanup instead of silently
// no-opping. The claim's WithShutdownAfter expiry is the backstop if no retry
// ever arrives.
func (p *Provider) Release(ctx context.Context, h sandbox.RunnerHandle) error {
	if h.Ref == "" {
		return nil
	}
	p.mu.Lock()
	sb, ok := p.live[h.Ref]
	delete(p.live, h.Ref)
	p.mu.Unlock()
	if !ok || sb == nil {
		// Already released (or claimed by a concurrent Release): nothing to do.
		return nil
	}
	if err := sb.Close(ctx); err != nil {
		// Re-insert so a later Release retries, unless a fresh claim already took
		// this ref in the meantime.
		p.mu.Lock()
		if _, taken := p.live[h.Ref]; !taken {
			p.live[h.Ref] = sb
		}
		p.mu.Unlock()
		return fmt.Errorf("agentsandbox provider: closing sandbox %q: %w", h.Ref, err)
	}
	return nil
}

// getFactory returns the call-time SDK factory, building it from cfg on first
// use. Tests may set p.factory directly to inject a fake.
func (p *Provider) getFactory() (sandboxFactory, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.factory != nil {
		return p.factory, nil
	}
	if p.cfg == nil {
		return nil, fmt.Errorf("agentsandbox provider: config not set (call SetConfig)")
	}
	f, err := newSDKFactory(p.cfg)
	if err != nil {
		return nil, fmt.Errorf("agentsandbox provider: building SDK client: %w", err)
	}
	p.factory = f
	return f, nil
}

// readCredential fetches the scoped credential value from its Secret, using the
// injected reader when set and otherwise a client-go clientset built from cfg.
func (p *Provider) readCredential(ctx context.Context, namespace, name, key string) ([]byte, error) {
	if p.readSecret != nil {
		return p.readSecret(ctx, namespace, name, key)
	}
	if p.cfg == nil {
		return nil, fmt.Errorf("agentsandbox provider: config not set (call SetConfig)")
	}
	cs, err := kubernetes.NewForConfig(p.cfg)
	if err != nil {
		return nil, fmt.Errorf("agentsandbox provider: building clientset: %w", err)
	}
	sec, err := cs.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("agentsandbox provider: credential Secret %s/%s not found", namespace, name)
		}
		return nil, fmt.Errorf("agentsandbox provider: getting credential Secret %s/%s: %w", namespace, name, err)
	}
	val, ok := sec.Data[key]
	if !ok {
		return nil, fmt.Errorf("agentsandbox provider: key %q not found in credential Secret %s/%s", key, namespace, name)
	}
	return val, nil
}

// resourceName is the deterministic name of the per-toolset SandboxTemplate and
// SandboxWarmPool. The dispatcher derives the warm-pool name from the toolset the
// same way, so no extra wiring is needed.
func resourceName(toolset string) string { return "codemode-" + toolset }

// warmPoolName is the SandboxWarmPool name a Claim provisions from.
func warmPoolName(toolset string) string { return resourceName(toolset) }
