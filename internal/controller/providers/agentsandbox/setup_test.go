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
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

const testRunnerImage = "quay.io/matzew/mcplo-agentsandbox:test"

func newSetupClient(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		sandboxv1beta1.AddToScheme,
		extv1beta1.AddToScheme,
		mcpv1alpha1.AddToScheme,
	} {
		if err := add(s); err != nil {
			t.Fatalf("AddToScheme: %v", err)
		}
	}
	return fake.NewClientBuilder().WithScheme(s).Build()
}

func newSetupProvider(c client.Client) *Provider {
	return &Provider{controllerClient: c, runnerImage: testRunnerImage, live: map[string]liveSandbox{}}
}

func sampleToolset() sandbox.ToolsetContext {
	return sandbox.ToolsetContext{Namespace: "team-a", Name: "ops"}
}

func TestSetupCreatesTemplateAndWarmPool(t *testing.T) {
	c := newSetupClient(t)
	p := newSetupProvider(c)
	ctx := context.Background()
	tc := sampleToolset()

	if err := p.Setup(ctx, tc); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	key := types.NamespacedName{Namespace: "team-a", Name: "codemode-ops"}

	var tmpl extv1beta1.SandboxTemplate
	if err := c.Get(ctx, key, &tmpl); err != nil {
		t.Fatalf("getting SandboxTemplate: %v", err)
	}
	pod := tmpl.Spec.PodTemplate.Spec
	if len(pod.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(pod.Containers))
	}
	ctr := pod.Containers[0]
	if ctr.Image != testRunnerImage {
		t.Errorf("container image = %q, want %q", ctr.Image, testRunnerImage)
	}
	// The /codemode input dir must be a writable mounted volume.
	var mounted bool
	for _, vm := range ctr.VolumeMounts {
		if vm.MountPath == inputDir && vm.Name == codemodeVolumeName {
			mounted = true
		}
	}
	if !mounted {
		t.Errorf("container missing writable %s volume mount", inputDir)
	}
	var hasVol bool
	for _, v := range pod.Volumes {
		if v.Name == codemodeVolumeName && v.EmptyDir != nil {
			hasVol = true
		}
	}
	if !hasVol {
		t.Errorf("pod missing %s emptyDir volume", codemodeVolumeName)
	}
	// Restricted security context.
	if ctr.SecurityContext == nil || ctr.SecurityContext.AllowPrivilegeEscalation == nil ||
		*ctr.SecurityContext.AllowPrivilegeEscalation {
		t.Error("container AllowPrivilegeEscalation must be false")
	}
	if ctr.SecurityContext == nil || ctr.SecurityContext.Capabilities == nil ||
		len(ctr.SecurityContext.Capabilities.Drop) == 0 || ctr.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Error("container must drop ALL capabilities")
	}
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Error("AutomountServiceAccountToken must be false")
	}

	var wp extv1beta1.SandboxWarmPool
	if err := c.Get(ctx, key, &wp); err != nil {
		t.Fatalf("getting SandboxWarmPool: %v", err)
	}
	if wp.Spec.TemplateRef.Name != "codemode-ops" {
		t.Errorf("warm pool templateRef = %q, want codemode-ops", wp.Spec.TemplateRef.Name)
	}
	if wp.Spec.Replicas == nil || *wp.Spec.Replicas != 0 {
		t.Errorf("warm pool replicas = %v, want 0", wp.Spec.Replicas)
	}
}

// TestSetupTemplateNetworkPolicyAdmitsDispatcher guards the hot-path fix: the
// agent-sandbox "Managed" default NetworkPolicy would admit only the sandbox
// router (ingress) and block all RFC1918 egress, breaking both the dispatcher's
// direct-pod-IP call and the script's reach to in-cluster MCPServers. Setup must
// instead supply an explicit policy: dispatcher ingress on the sandboxd ports and
// allow-all egress.
func TestSetupTemplateNetworkPolicyAdmitsDispatcher(t *testing.T) {
	c := newSetupClient(t)
	p := newSetupProvider(c)
	ctx := context.Background()
	tc := sampleToolset()

	if err := p.Setup(ctx, tc); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	var tmpl extv1beta1.SandboxTemplate
	key := types.NamespacedName{Namespace: tc.Namespace, Name: "codemode-" + tc.Name}
	if err := c.Get(ctx, key, &tmpl); err != nil {
		t.Fatalf("getting SandboxTemplate: %v", err)
	}

	if tmpl.Spec.NetworkPolicyManagement != extv1beta1.NetworkPolicyManagementManaged {
		t.Errorf("NetworkPolicyManagement = %q, want Managed", tmpl.Spec.NetworkPolicyManagement)
	}
	np := tmpl.Spec.NetworkPolicy
	if np == nil {
		t.Fatal("template carries no NetworkPolicy; the default would break the hot path")
	}

	if len(np.Ingress) != 1 || len(np.Ingress[0].From) != 1 {
		t.Fatalf("ingress = %+v, want a single dispatcher peer", np.Ingress)
	}
	sel := np.Ingress[0].From[0].PodSelector
	if sel == nil || sel.MatchLabels[toolsetLabelKey] != tc.Name ||
		sel.MatchLabels[dispatcherComponentKey] != dispatcherComponentValue {
		t.Errorf("ingress peer selector = %+v, want the toolset's dispatcher pod", sel)
	}
	ports := map[int32]bool{}
	for _, pt := range np.Ingress[0].Ports {
		ports[pt.Port.IntVal] = true
	}
	if !ports[sandboxdRESTPort] || !ports[sandboxdGRPCPort] {
		t.Errorf("ingress ports = %+v, want sandboxd %d and %d", np.Ingress[0].Ports, sandboxdRESTPort, sandboxdGRPCPort)
	}

	// Allow-all egress is a single rule with no peers and no ports.
	if len(np.Egress) != 1 || len(np.Egress[0].To) != 0 || len(np.Egress[0].Ports) != 0 {
		t.Errorf("egress = %+v, want a single allow-all rule", np.Egress)
	}
}

func TestSetupIsIdempotent(t *testing.T) {
	c := newSetupClient(t)
	p := newSetupProvider(c)
	ctx := context.Background()
	tc := sampleToolset()

	if err := p.Setup(ctx, tc); err != nil {
		t.Fatalf("first Setup: %v", err)
	}
	if err := p.Setup(ctx, tc); err != nil {
		t.Fatalf("second Setup: %v", err)
	}

	var tmpls extv1beta1.SandboxTemplateList
	if err := c.List(ctx, &tmpls); err != nil {
		t.Fatalf("listing templates: %v", err)
	}
	if len(tmpls.Items) != 1 {
		t.Errorf("expected 1 template after repeated Setup, got %d", len(tmpls.Items))
	}
}

// TestSetupSetsOwnerReference proves the backend resources are owner-referenced
// to the toolset so Kubernetes garbage-collects them on toolset deletion (no
// orphaned SandboxTemplate/SandboxWarmPool, no finalizer needed).
func TestSetupSetsOwnerReference(t *testing.T) {
	c := newSetupClient(t)
	p := newSetupProvider(c)
	ctx := context.Background()

	owner := &mcpv1alpha1.MCPToolset{
		ObjectMeta: metav1.ObjectMeta{Name: "ops", Namespace: "team-a", UID: "owner-uid"},
	}
	tc := sampleToolset()
	tc.Owner = owner

	if err := p.Setup(ctx, tc); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	key := types.NamespacedName{Namespace: "team-a", Name: "codemode-ops"}
	assertControlledBy := func(kind string, refs []metav1.OwnerReference) {
		for _, ref := range refs {
			if ref.Kind == "MCPToolset" && ref.Name == "ops" && ref.Controller != nil && *ref.Controller {
				return
			}
		}
		t.Errorf("%s owner references = %+v; want a controller ref to MCPToolset ops", kind, refs)
	}

	var tmpl extv1beta1.SandboxTemplate
	if err := c.Get(ctx, key, &tmpl); err != nil {
		t.Fatalf("getting SandboxTemplate: %v", err)
	}
	assertControlledBy("SandboxTemplate", tmpl.OwnerReferences)

	var wp extv1beta1.SandboxWarmPool
	if err := c.Get(ctx, key, &wp); err != nil {
		t.Fatalf("getting SandboxWarmPool: %v", err)
	}
	assertControlledBy("SandboxWarmPool", wp.OwnerReferences)
}

func TestTeardownDeletesBothAndToleratesNotFound(t *testing.T) {
	c := newSetupClient(t)
	p := newSetupProvider(c)
	ctx := context.Background()
	tc := sampleToolset()

	if err := p.Setup(ctx, tc); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := p.Teardown(ctx, tc); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	key := types.NamespacedName{Namespace: "team-a", Name: "codemode-ops"}
	if err := c.Get(ctx, key, &extv1beta1.SandboxTemplate{}); !apierrors.IsNotFound(err) {
		t.Errorf("SandboxTemplate still present: %v", err)
	}
	if err := c.Get(ctx, key, &extv1beta1.SandboxWarmPool{}); !apierrors.IsNotFound(err) {
		t.Errorf("SandboxWarmPool still present: %v", err)
	}

	// Second Teardown on already-absent resources is a clean no-op.
	if err := p.Teardown(ctx, tc); err != nil {
		t.Errorf("idempotent Teardown: %v", err)
	}
}

func TestSetupRequiresWiring(t *testing.T) {
	ctx := context.Background()
	tc := sampleToolset()

	if err := (&Provider{runnerImage: testRunnerImage}).Setup(ctx, tc); err == nil {
		t.Error("expected error when controller client is unset")
	}
	if err := (&Provider{controllerClient: newSetupClient(t)}).Setup(ctx, tc); err == nil {
		t.Error("expected error when runner image is unset")
	}
}
