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

// Package pod implements the baseline "pod" SandboxProvider: each execution
// runs in a short-lived, filesystem-isolated Kubernetes Pod created directly
// against the API server. It provides baseline isolation only (no controlled
// egress) and a static, non-production scoped credential (see T023).
package pod

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

// ProviderName is the value that spec.provider must carry to select this provider.
const ProviderName = "pod"

const (
	// runnerContainerName is the name of the single runner container in the sandbox Pod.
	runnerContainerName = "runner"
	// credentialSecretKey is the Secret key the runner credential is read from.
	// The provider mounts it into EnvCredential via a secretKeyRef so the value
	// never passes through the operator or the Pod spec in plain text.
	credentialSecretKey = "credential"
	// awaitPollInterval is how often Await re-checks the sandbox Pod phase.
	awaitPollInterval = 250 * time.Millisecond
)

// logFetcher retrieves the finished sandbox's stdout - the runner's result
// envelope. It is abstracted behind a function type so the client-go dependency
// (Pod log streaming, which the controller-runtime client cannot do) stays out
// of the core provider logic and so tests can supply canned output.
type logFetcher func(ctx context.Context, namespace, name string) ([]byte, error)

// Provider is the baseline pod SandboxProvider. It holds a call-time client used
// by Claim/Release; the client operates directly against the API server and does
// not depend on the controller reconcile loop (FR-009/SC-006).
type Provider struct {
	client    client.Client
	fetchLogs logFetcher
}

// New returns a pod Provider backed by the given client. Await additionally
// requires a log fetcher (see NewWithLogs / SetLogFetcher).
func New(c client.Client) *Provider {
	return &Provider{client: c}
}

// NewWithLogs returns a pod Provider that can also retrieve results via the
// given log fetcher.
func NewWithLogs(c client.Client, f logFetcher) *Provider {
	return &Provider{client: c, fetchLogs: f}
}

// singleton is the instance registered in the sandbox registry. Its client is
// nil until wired at process startup via SetClient. Name/IsolationGuarantees and
// the controller's Setup (near no-op) work without a client; Claim/Release
// require SetClient to have run.
var singleton = &Provider{}

func init() {
	sandbox.Register(singleton)
}

// SetClient wires the call-time client into the registered provider. The
// dispatcher (and the operator, for Setup) call this once at startup.
func SetClient(c client.Client) {
	singleton.client = c
}

// SetLogFetcher wires the result-retrieval mechanism into the registered
// provider. Only the dispatcher (which awaits results) needs this; the operator
// does not. NewClientsetFetcher builds the production implementation.
func SetLogFetcher(f logFetcher) {
	singleton.fetchLogs = f
}

// NewClientsetFetcher builds a logFetcher that streams a Pod's runner-container
// logs via client-go.
func NewClientsetFetcher(cs kubernetes.Interface) logFetcher {
	return func(ctx context.Context, namespace, name string) ([]byte, error) {
		req := cs.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{Container: runnerContainerName})
		stream, err := req.Stream(ctx)
		if err != nil {
			return nil, fmt.Errorf("pod provider: streaming logs for %s/%s: %w", namespace, name, err)
		}
		defer func() { _ = stream.Close() }()
		return io.ReadAll(stream)
	}
}

// Name implements sandbox.SandboxProvider.
func (p *Provider) Name() string { return ProviderName }

// IsolationGuarantees implements sandbox.SandboxProvider. The bare pod provider
// offers baseline isolation: an isolated filesystem and a fresh, ephemeral
// sandbox per execution, but no controlled egress, and only a static credential.
func (p *Provider) IsolationGuarantees() sandbox.IsolationGuarantees {
	return sandbox.IsolationGuarantees{
		FilesystemIsolated:    true,
		PerExecutionEphemeral: true,
		EgressControlled:      false,
		CredentialScoping:     sandbox.CredentialScopingStatic,
	}
}

// Setup is a near no-op: the dispatcher infrastructure is owned by the
// MCPToolset controller, not the provider. The pod provider has no long-lived
// per-toolset backend state to establish.
func (p *Provider) Setup(context.Context, sandbox.ToolsetContext) error { return nil }

// Teardown is a near no-op for the same reason as Setup.
func (p *Provider) Teardown(context.Context, sandbox.ToolsetContext) error { return nil }

// Claim provisions a fresh runner Pod for a single execution and returns a
// handle identifying it. The Pod is configured with the runner image, the
// allow-listed endpoints, the code and timeout, and (if set) the static scoped
// credential sourced from a Secret. Each Claim yields a brand-new Pod so no
// state is shared between executions (FR-006/SC-005).
func (p *Provider) Claim(ctx context.Context, spec sandbox.RunnerSpec) (sandbox.RunnerHandle, error) {
	if p.client == nil {
		return sandbox.RunnerHandle{}, fmt.Errorf("pod provider: client not configured (call SetClient)")
	}
	if spec.RunnerImage == "" {
		return sandbox.RunnerHandle{}, fmt.Errorf("pod provider: RunnerImage is required")
	}

	endpointsJSON, err := sandbox.MarshalEndpoints(spec.AllowedEndpoints)
	if err != nil {
		return sandbox.RunnerHandle{}, err
	}

	pod := buildRunnerPod(spec, endpointsJSON)

	if err := p.client.Create(ctx, pod); err != nil {
		return sandbox.RunnerHandle{}, fmt.Errorf("pod provider: creating sandbox Pod: %w", err)
	}

	return sandbox.RunnerHandle{Namespace: pod.Namespace, Name: pod.Name}, nil
}

// Await blocks until the sandbox Pod reaches a terminal phase and returns the
// runner container's stdout (the result envelope). It polls the Pod phase via
// the controller-runtime client and reads logs via the configured fetcher. A
// ctx deadline returns promptly so the dispatcher can surface a timeout.
func (p *Provider) Await(ctx context.Context, h sandbox.RunnerHandle) ([]byte, error) {
	if p.client == nil {
		return nil, fmt.Errorf("pod provider: client not configured (call SetClient)")
	}
	if p.fetchLogs == nil {
		return nil, fmt.Errorf("pod provider: log fetcher not configured (call SetLogFetcher)")
	}

	for {
		var pod corev1.Pod
		if err := p.client.Get(ctx, types.NamespacedName{Namespace: h.Namespace, Name: h.Name}, &pod); err != nil {
			return nil, fmt.Errorf("pod provider: getting sandbox Pod %s/%s: %w", h.Namespace, h.Name, err)
		}
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			return p.fetchLogs(ctx, h.Namespace, h.Name)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(awaitPollInterval):
		}
	}
}

// Release deletes the sandbox Pod identified by the handle. A not-found Pod is
// treated as already released.
func (p *Provider) Release(ctx context.Context, h sandbox.RunnerHandle) error {
	if p.client == nil {
		return fmt.Errorf("pod provider: client not configured (call SetClient)")
	}
	if h.Name == "" {
		return nil
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: h.Namespace, Name: h.Name}}
	if err := p.client.Delete(ctx, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("pod provider: deleting sandbox Pod %s/%s: %w", h.Namespace, h.Name, err)
	}
	return nil
}

// buildRunnerPod constructs the sandbox Pod object from the RunnerSpec. It uses
// GenerateName so concurrent claims never collide, restartPolicy Never (a
// runner runs once), and the restricted Pod Security Standard security context.
func buildRunnerPod(spec sandbox.RunnerSpec, endpointsJSON string) *corev1.Pod {
	timeout := int64(spec.Timeout.Seconds())

	env := []corev1.EnvVar{
		{Name: sandbox.EnvEndpoints, Value: endpointsJSON},
		{Name: sandbox.EnvCode, Value: spec.Code},
		{Name: sandbox.EnvTimeoutSeconds, Value: strconv.FormatInt(timeout, 10)},
	}

	// The static scoped credential (MVP, non-production - see T023) is sourced
	// from a Secret via secretKeyRef so its value never appears in the Pod spec.
	if spec.Credential.SecretName != "" {
		key := spec.Credential.Key
		if key == "" {
			key = credentialSecretKey
		}
		env = append(env, corev1.EnvVar{
			Name: sandbox.EnvCredential,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: spec.Credential.SecretName},
					Key:                  key,
				},
			},
		})
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by": "mcp-lifecycle-operator",
		"mcp.x-k8s.io/component":       "code-mode-runner",
	}
	if spec.Toolset != "" {
		labels["mcp.x-k8s.io/toolset"] = spec.Toolset
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateName(spec.Toolset),
			Namespace:    spec.Namespace,
			Labels:       labels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			AutomountServiceAccountToken: new(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   new(true),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:  runnerContainerName,
				Image: spec.RunnerImage,
				Env:   env,
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: new(false),
					ReadOnlyRootFilesystem:   new(true),
					RunAsNonRoot:             new(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				},
			}},
		},
	}
	return pod
}

// generateName returns a GenerateName prefix for a sandbox Pod, incorporating
// the toolset name when available. Kubernetes appends a random suffix.
func generateName(toolset string) string {
	if toolset == "" {
		return "codemode-runner-"
	}
	return fmt.Sprintf("codemode-%s-", toolset)
}
