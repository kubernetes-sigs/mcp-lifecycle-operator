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

package pod

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

func newFakeClient(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).Build()
}

func sampleSpec() sandbox.RunnerSpec {
	return sandbox.RunnerSpec{
		Namespace:   "demo",
		Toolset:     "ops",
		RunnerImage: "quay.io/matzew/codemode-runner:latest",
		AllowedEndpoints: []sandbox.Endpoint{
			{Server: "kubernetes", URL: "http://kubernetes.demo.svc.cluster.local:8080/mcp"},
		},
		Credential: sandbox.CredentialRef{SecretName: "ops-cred", Key: "token"},
		Code:       "return 42;",
		Timeout:    90 * time.Second,
	}
}

func getEnv(c corev1.Container, name string) (corev1.EnvVar, bool) {
	for _, e := range c.Env {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

func TestProviderMetadata(t *testing.T) {
	p := New(newFakeClient(t))
	if p.Name() != "pod" {
		t.Errorf("Name() = %q; want pod", p.Name())
	}
	g := p.IsolationGuarantees()
	if !g.FilesystemIsolated || !g.PerExecutionEphemeral {
		t.Errorf("baseline guarantees missing: %+v", g)
	}
	if g.EgressControlled {
		t.Errorf("pod provider must not claim EgressControlled")
	}
	if g.CredentialScoping != sandbox.CredentialScopingStatic {
		t.Errorf("CredentialScoping = %q; want static", g.CredentialScoping)
	}
	// The pod provider satisfies baseline but not egress (FR-011).
	if !g.Satisfies("baseline") {
		t.Errorf("pod provider should satisfy baseline")
	}
	if g.Satisfies("egress") {
		t.Errorf("pod provider must not satisfy egress")
	}
}

func TestClaimCreatesConfiguredPod(t *testing.T) {
	c := newFakeClient(t)
	p := New(c)
	spec := sampleSpec()

	h, err := p.Claim(context.Background(), spec)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if h.Namespace != "demo" || h.Name == "" {
		t.Fatalf("handle = %+v; want namespace demo and a generated name", h)
	}

	var pod corev1.Pod
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: h.Namespace, Name: h.Name}, &pod); err != nil {
		t.Fatalf("get created pod: %v", err)
	}

	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("containers = %d; want 1", len(pod.Spec.Containers))
	}
	ctr := pod.Spec.Containers[0]
	if ctr.Image != spec.RunnerImage {
		t.Errorf("image = %q; want %q", ctr.Image, spec.RunnerImage)
	}
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restartPolicy = %q; want Never", pod.Spec.RestartPolicy)
	}

	// Endpoints allow-list is passed as JSON and round-trips.
	epEnv, ok := getEnv(ctr, sandbox.EnvEndpoints)
	if !ok {
		t.Fatalf("missing %s env", sandbox.EnvEndpoints)
	}
	eps, err := sandbox.UnmarshalEndpoints(epEnv.Value)
	if err != nil {
		t.Fatalf("unmarshal endpoints: %v", err)
	}
	if len(eps) != 1 || eps[0].Server != "kubernetes" {
		t.Errorf("endpoints = %v; want single kubernetes endpoint", eps)
	}

	if codeEnv, ok := getEnv(ctr, sandbox.EnvCode); !ok || codeEnv.Value != spec.Code {
		t.Errorf("code env = %q, ok=%v; want %q", codeEnv.Value, ok, spec.Code)
	}
	if toEnv, ok := getEnv(ctr, sandbox.EnvTimeoutSeconds); !ok || toEnv.Value != "90" {
		t.Errorf("timeout env = %q, ok=%v; want 90", toEnv.Value, ok)
	}

	// Credential comes from a Secret via secretKeyRef, never inline.
	credEnv, ok := getEnv(ctr, sandbox.EnvCredential)
	if !ok {
		t.Fatalf("missing %s env", sandbox.EnvCredential)
	}
	if credEnv.Value != "" {
		t.Errorf("credential must not be inline; got value %q", credEnv.Value)
	}
	if credEnv.ValueFrom == nil || credEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("credential env has no secretKeyRef")
	}
	if credEnv.ValueFrom.SecretKeyRef.Name != "ops-cred" || credEnv.ValueFrom.SecretKeyRef.Key != "token" {
		t.Errorf("secretKeyRef = %+v; want ops-cred/token", credEnv.ValueFrom.SecretKeyRef)
	}
}

func TestClaimYieldsFreshSandboxEachTime(t *testing.T) {
	c := newFakeClient(t)
	p := New(c)
	spec := sampleSpec()

	h1, err := p.Claim(context.Background(), spec)
	if err != nil {
		t.Fatalf("Claim 1: %v", err)
	}
	h2, err := p.Claim(context.Background(), spec)
	if err != nil {
		t.Fatalf("Claim 2: %v", err)
	}
	if h1.Name == h2.Name {
		t.Errorf("two claims produced the same pod name %q; want distinct sandboxes", h1.Name)
	}

	var pods corev1.PodList
	if err := c.List(context.Background(), &pods); err != nil {
		t.Fatalf("list pods: %v", err)
	}
	if len(pods.Items) != 2 {
		t.Errorf("pod count = %d; want 2", len(pods.Items))
	}
}

func TestReleaseDeletesPod(t *testing.T) {
	c := newFakeClient(t)
	p := New(c)

	h, err := p.Claim(context.Background(), sampleSpec())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := p.Release(context.Background(), h); err != nil {
		t.Fatalf("Release: %v", err)
	}

	var pod corev1.Pod
	err = c.Get(context.Background(), types.NamespacedName{Namespace: h.Namespace, Name: h.Name}, &pod)
	if !apierrors.IsNotFound(err) {
		t.Errorf("get after release err = %v; want NotFound", err)
	}

	// Releasing an already-gone sandbox is a no-op.
	if err := p.Release(context.Background(), h); err != nil {
		t.Errorf("second Release: %v; want nil", err)
	}
}

func TestAwaitReturnsRunnerOutputOnCompletion(t *testing.T) {
	c := newFakeClient(t)
	fetched := false
	fetch := func(_ context.Context, ns, name string) ([]byte, error) {
		fetched = true
		return []byte(`{"result":42}`), nil
	}
	p := NewWithLogs(c, fetch)

	h, err := p.Claim(context.Background(), sampleSpec())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	// Drive the sandbox Pod to a terminal phase so Await returns.
	var pod corev1.Pod
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: h.Namespace, Name: h.Name}, &pod); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	pod.Status.Phase = corev1.PodSucceeded
	if err := c.Status().Update(context.Background(), &pod); err != nil {
		t.Fatalf("update pod status: %v", err)
	}

	out, err := p.Await(context.Background(), h)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if !fetched {
		t.Errorf("log fetcher was not called")
	}
	if string(out) != `{"result":42}` {
		t.Errorf("Await output = %q; want the runner envelope", out)
	}
}

func TestAwaitRespectsContextDeadline(t *testing.T) {
	c := newFakeClient(t)
	p := NewWithLogs(c, func(context.Context, string, string) ([]byte, error) {
		t.Fatal("log fetcher must not run while the Pod is still running")
		return nil, nil
	})

	h, err := p.Claim(context.Background(), sampleSpec())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	// Pod stays Pending; Await must return promptly when the context expires.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := p.Await(ctx, h); err == nil {
		t.Errorf("Await: want context error while Pod never completes")
	}
}

func TestAwaitRequiresClientAndFetcher(t *testing.T) {
	if _, err := (&Provider{}).Await(context.Background(), sandbox.RunnerHandle{Namespace: "demo", Name: "x"}); err == nil {
		t.Errorf("Await without client: want error")
	}
	if _, err := New(newFakeClient(t)).Await(context.Background(), sandbox.RunnerHandle{Namespace: "demo", Name: "x"}); err == nil {
		t.Errorf("Await without fetcher: want error")
	}
}

func TestClaimRequiresClientAndImage(t *testing.T) {
	// No client configured.
	if _, err := (&Provider{}).Claim(context.Background(), sampleSpec()); err == nil {
		t.Errorf("Claim without client: want error")
	}

	// No runner image.
	spec := sampleSpec()
	spec.RunnerImage = ""
	if _, err := New(newFakeClient(t)).Claim(context.Background(), spec); err == nil {
		t.Errorf("Claim without image: want error")
	}
}
