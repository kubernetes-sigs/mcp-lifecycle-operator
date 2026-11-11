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
	"time"

	"k8s.io/client-go/rest"
	sandboxsdk "sigs.k8s.io/agent-sandbox/clients/go/sandbox"
)

// liveSandbox is the minimal view of an open agent-sandbox the provider needs at
// call time. It is satisfied by the real SDK adapter (*sdkSandbox, wrapping
// *sandboxsdk.Sandbox) and by test fakes, so Claim/Await/Release are unit
// testable without a real cluster or a running sandboxd.
type liveSandbox interface {
	// ClaimName returns the server-assigned SandboxClaim name identifying this
	// sandbox. It is the key under which the provider tracks the live handle.
	ClaimName() string
	// Write places a file into the sandbox before the runner is invoked.
	Write(ctx context.Context, path string, content []byte) error
	// Run executes a command in the sandbox and returns its stdout (the runner's
	// result envelope).
	Run(ctx context.Context, command string) (stdout string, err error)
	// Close tears the sandbox down and deletes its claim.
	Close(ctx context.Context) error
}

// sandboxFactory creates, re-attaches, and deletes sandboxes. It is satisfied by
// the real adapter over *sandboxsdk.Client and by test fakes.
type sandboxFactory interface {
	// CreateSandbox provisions a fresh sandbox from the named warm pool. The
	// returned sandbox is already open (the SDK's CreateSandbox calls Open
	// internally), so callers must not open it again. shutdownAfter bounds the
	// claim's server-side lifetime so a dispatcher that crashes before Release
	// cannot leak a permanent sandbox; a non-positive value means no expiry.
	CreateSandbox(ctx context.Context, warmPoolName, namespace string, shutdownAfter time.Duration) (liveSandbox, error)
	// GetSandbox re-attaches to an existing sandbox by claim name.
	GetSandbox(ctx context.Context, claimName, namespace string) (liveSandbox, error)
	// DeleteSandbox deletes a sandbox by claim name.
	DeleteSandbox(ctx context.Context, claimName, namespace string) error
}

// newSDKFactory builds the production sandboxFactory from a REST config. The
// provider runs in-cluster (the dispatcher pod), so it uses the sandboxd runtime
// reached directly on the sandbox pod IP, which needs no Service.
func newSDKFactory(cfg *rest.Config) (sandboxFactory, error) {
	c, err := sandboxsdk.NewClient(context.Background(), sandboxsdk.Options{
		RestConfig:   cfg,
		Runtime:      sandboxsdk.RuntimeSandboxd,
		Connectivity: sandboxsdk.ConnectivityInClusterPodIP,
	})
	if err != nil {
		return nil, err
	}
	return &sdkFactory{client: c}, nil
}

// sdkFactory adapts *sandboxsdk.Client to the sandboxFactory interface.
type sdkFactory struct {
	client *sandboxsdk.Client
}

func (f *sdkFactory) CreateSandbox(ctx context.Context, warmPoolName, namespace string, shutdownAfter time.Duration) (liveSandbox, error) {
	var opts []sandboxsdk.CreateOption
	if shutdownAfter > 0 {
		opts = append(opts, sandboxsdk.WithShutdownAfter(shutdownAfter))
	}
	sb, err := f.client.CreateSandbox(ctx, warmPoolName, namespace, opts...)
	if err != nil {
		return nil, err
	}
	return &sdkSandbox{sb: sb, client: f.client, namespace: namespace}, nil
}

func (f *sdkFactory) GetSandbox(ctx context.Context, claimName, namespace string) (liveSandbox, error) {
	sb, err := f.client.GetSandbox(ctx, claimName, namespace)
	if err != nil {
		return nil, err
	}
	return &sdkSandbox{sb: sb, client: f.client, namespace: namespace}, nil
}

func (f *sdkFactory) DeleteSandbox(ctx context.Context, claimName, namespace string) error {
	return f.client.DeleteSandbox(ctx, claimName, namespace)
}

// sdkSandbox adapts *sandboxsdk.Sandbox to the liveSandbox interface. It keeps a
// reference to the owning Client so Close can route cleanup through
// Client.DeleteSandbox (see Close).
type sdkSandbox struct {
	sb        *sandboxsdk.Sandbox
	client    *sandboxsdk.Client
	namespace string
}

func (s *sdkSandbox) ClaimName() string { return s.sb.ClaimName() }

func (s *sdkSandbox) Write(ctx context.Context, path string, content []byte) error {
	return s.sb.Write(ctx, path, content)
}

func (s *sdkSandbox) Run(ctx context.Context, command string) (string, error) {
	res, err := s.sb.Run(ctx, command)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// Close tears the sandbox down. It routes through Client.DeleteSandbox rather
// than Sandbox.Close directly: in the pinned SDK (v1.0.10) Sandbox.Close deletes
// the claim but leaves the handle in the Client's registry, whereas
// Client.DeleteSandbox removes the registry entry as well, so repeated executions
// do not accumulate closed handles in the long-lived dispatcher client.
func (s *sdkSandbox) Close(ctx context.Context) error {
	return s.client.DeleteSandbox(ctx, s.sb.ClaimName(), s.namespace)
}
