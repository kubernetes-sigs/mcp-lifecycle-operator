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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"

	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/sandbox"
)

const (
	// sandboxContainerName is the single container in the sandbox pod. It runs the
	// sandboxd runtime server; the runner binary is invoked inside it via Run.
	sandboxContainerName = "sandbox"
	// codemodeVolumeName is the writable emptyDir mounted at inputDir so the
	// dispatcher's Write calls land on a writable path.
	codemodeVolumeName = "codemode"

	// sandboxd listens on these ports inside the sandbox pod (REST + gRPC). The
	// dispatcher reaches them directly over the pod IP, so the template
	// NetworkPolicy must admit the dispatcher on both.
	sandboxdRESTPort = 8080
	sandboxdGRPCPort = 9090

	// Dispatcher pod labels the sandbox NetworkPolicy admits ingress from. These
	// mirror the labels the MCPToolset controller stamps on the dispatcher pod
	// (dispatcherLabels in mcptoolset_controller_dispatcher.go). The controller
	// package imports this one, so the two cannot share a constant without an
	// import cycle; they are kept in sync by this wire contract, exactly like the
	// dispatcher env-var names.
	dispatcherComponentKey   = "app.kubernetes.io/component"
	dispatcherComponentValue = "code-mode-dispatcher"
	toolsetLabelKey          = "mcp.x-k8s.io/toolset"
)

// Operator (reconcile-time) RBAC: Setup/Teardown own the per-toolset
// SandboxTemplate + SandboxWarmPool. The call-time resources (sandboxclaims,
// sandboxes) are used by the dispatcher through a programmatic namespace Role.
// Kubernetes RBAC forbids an account from granting permissions it does not hold
// itself (privilege-escalation prevention), so the operator ClusterRole must
// also carry sandboxclaims and sandboxes for that Role to be created. (pods and
// secrets, also granted to the dispatcher, are already held by the operator's
// core controllers.)
// +kubebuilder:rbac:groups=extensions.agents.x-k8s.io,resources=sandboxtemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.agents.x-k8s.io,resources=sandboxwarmpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.agents.x-k8s.io,resources=sandboxclaims,verbs=get;list;create;delete
// +kubebuilder:rbac:groups=agents.x-k8s.io,resources=sandboxes,verbs=get;list

// Setup establishes the long-lived per-toolset backend state: a SandboxTemplate
// (the sandbox blueprint, carrying the runner image and a writable /codemode
// volume) and a SandboxWarmPool referencing it with replicas 0 (no idle pods;
// each claim cold-starts). Both are created-or-updated, so Setup is idempotent.
func (p *Provider) Setup(ctx context.Context, tc sandbox.ToolsetContext) error {
	if p.controllerClient == nil {
		return fmt.Errorf("agentsandbox provider: controller client not set (call SetControllerClient)")
	}
	if p.runnerImage == "" {
		return fmt.Errorf("agentsandbox provider: runner image not set (call SetRunnerImage)")
	}

	name := resourceName(tc.Name)
	labels := managedLabels(tc.Name)

	tmpl := &extv1beta1.SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tc.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, p.controllerClient, tmpl, func() error {
		tmpl.Labels = labels
		tmpl.Spec = p.templateSpec(tc.Name, labels)
		return nil
	}); err != nil {
		return fmt.Errorf("agentsandbox provider: applying SandboxTemplate %s/%s: %w", tc.Namespace, name, err)
	}

	wp := &extv1beta1.SandboxWarmPool{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tc.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, p.controllerClient, wp, func() error {
		wp.Labels = labels
		wp.Spec.Replicas = new(int32) // 0: no idle pods, cold-start per claim (MVP)
		wp.Spec.TemplateRef = extv1beta1.SandboxTemplateRef{Name: name}
		return nil
	}); err != nil {
		return fmt.Errorf("agentsandbox provider: applying SandboxWarmPool %s/%s: %w", tc.Namespace, name, err)
	}

	return nil
}

// Teardown removes the toolset's SandboxWarmPool and SandboxTemplate, tolerating
// not-found so it is idempotent.
func (p *Provider) Teardown(ctx context.Context, tc sandbox.ToolsetContext) error {
	if p.controllerClient == nil {
		return fmt.Errorf("agentsandbox provider: controller client not set (call SetControllerClient)")
	}

	name := resourceName(tc.Name)

	wp := &extv1beta1.SandboxWarmPool{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tc.Namespace}}
	if err := p.controllerClient.Delete(ctx, wp); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("agentsandbox provider: deleting SandboxWarmPool %s/%s: %w", tc.Namespace, name, err)
	}

	tmpl := &extv1beta1.SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tc.Namespace}}
	if err := p.controllerClient.Delete(ctx, tmpl); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("agentsandbox provider: deleting SandboxTemplate %s/%s: %w", tc.Namespace, name, err)
	}

	return nil
}

// templateSpec builds the SandboxTemplate spec: a restricted, non-root sandbox
// pod running the configured runner image with a writable /codemode emptyDir,
// plus the NetworkPolicy the code-mode hot path requires (see networkPolicySpec).
func (p *Provider) templateSpec(toolset string, labels map[string]string) extv1beta1.SandboxTemplateSpec {
	return extv1beta1.SandboxTemplateSpec{
		NetworkPolicyManagement: extv1beta1.NetworkPolicyManagementManaged,
		NetworkPolicy:           networkPolicySpec(toolset),
		SandboxBlueprint: sandboxv1beta1.SandboxBlueprint{
			PodTemplate: sandboxv1beta1.PodTemplate{
				ObjectMeta: sandboxv1beta1.PodMetadata{Labels: labels},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: new(false),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   new(true),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Volumes: []corev1.Volume{{
						Name:         codemodeVolumeName,
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}},
					Containers: []corev1.Container{{
						Name:  sandboxContainerName,
						Image: p.runnerImage,
						// Root sandboxd at the writable /codemode emptyDir so the
						// dispatcher's (relative) file writes land there and the
						// runner reads them from the same path. sandboxd's REST/gRPC
						// ports keep their defaults (8080/9090), which the SDK expects.
						Args:         []string{"--root-dir=" + inputDir},
						VolumeMounts: []corev1.VolumeMount{{Name: codemodeVolumeName, MountPath: inputDir}},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: new(false),
							RunAsNonRoot:             new(true),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
						},
					}},
				},
			},
		},
	}
}

// networkPolicySpec is the ingress/egress the agent-sandbox SandboxTemplate
// controller stamps onto the shared per-template NetworkPolicy (it manages the
// PodSelector and PolicyTypes itself). The "Managed" default would deny both
// directions the code-mode hot path needs: its ingress admits only the
// sandbox-router, but the dispatcher reaches sandboxd directly on the pod IP; and
// its egress blocks all RFC1918 ranges, but the running script must reach the
// allow-listed in-cluster MCPServers (and cluster DNS). So:
//   - Ingress: admit the toolset's dispatcher pod on the sandboxd REST+gRPC ports.
//   - Egress: allow all. The containment boundary for the MVP is the runner's
//     endpoint allow-list, not the network; IsolationGuarantees reports
//     EgressControlled=false accordingly. Narrowing egress to just DNS plus the
//     resolved MCPServer endpoints is a post-MVP follow-up.
func networkPolicySpec(toolset string) *extv1beta1.NetworkPolicySpec {
	tcp := corev1.ProtocolTCP
	restPort := intstr.FromInt(sandboxdRESTPort)
	grpcPort := intstr.FromInt(sandboxdGRPCPort)
	return &extv1beta1.NetworkPolicySpec{
		Ingress: []networkingv1.NetworkPolicyIngressRule{{
			From: []networkingv1.NetworkPolicyPeer{{
				// podSelector with no namespaceSelector matches pods in the
				// sandbox's own namespace, which is where the dispatcher runs.
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					dispatcherComponentKey: dispatcherComponentValue,
					toolsetLabelKey:        toolset,
				}},
			}},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcp, Port: &restPort},
				{Protocol: &tcp, Port: &grpcPort},
			},
		}},
		// A single egress rule with no peers and no ports allows all egress.
		Egress: []networkingv1.NetworkPolicyEgressRule{{}},
	}
}

// managedLabels are the labels stamped on the per-toolset backend resources.
func managedLabels(toolset string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "mcp-lifecycle-operator",
		"mcp.x-k8s.io/component":       "code-mode-sandbox",
	}
	if toolset != "" {
		labels["mcp.x-k8s.io/toolset"] = toolset
	}
	return labels
}
