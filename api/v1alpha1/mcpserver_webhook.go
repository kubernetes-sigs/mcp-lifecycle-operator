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

package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	webhookpolicy "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/webhook"
)

// matchPolicy=Equivalent lets the API server convert other served versions
// (e.g. v1beta1) to v1alpha1 before invoking this validator, so admission
// policy applies to every served version through a single webhook. Do not
// switch this to Exact without registering a per-version handler.
// +kubebuilder:webhook:path=/validate-mcp-x-k8s-io-v1alpha1-mcpserver,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,sideEffects=none,groups=mcp.x-k8s.io,resources=mcpservers,verbs=create;update,versions=v1alpha1,name=vmcpserver.mcp.x-k8s.io,admissionReviewVersions=v1

// +kubebuilder:object:generate=false

// MCPServerCustomValidator validates MCPServer resources against admission policy.
type MCPServerCustomValidator struct {
	Policy *webhookpolicy.AdmissionPolicy
	Reader client.Reader
}

var _ admission.Validator[*MCPServer] = &MCPServerCustomValidator{}

// SetupWebhookWithManager registers the validating webhook for MCPServer.
func SetupWebhookWithManager(mgr ctrl.Manager, policy *webhookpolicy.AdmissionPolicy) error {
	return ctrl.NewWebhookManagedBy(mgr, &MCPServer{}).
		WithValidator(&MCPServerCustomValidator{
			Policy: policy,
			Reader: mgr.GetAPIReader(),
		}).
		Complete()
}

// ValidateCreate validates an MCPServer on creation.
func (v *MCPServerCustomValidator) ValidateCreate(ctx context.Context, mcpServer *MCPServer) (admission.Warnings, error) {
	return v.validate(ctx, mcpServer, "CREATE")
}

// ValidateUpdate validates an MCPServer on update.
func (v *MCPServerCustomValidator) ValidateUpdate(ctx context.Context, _ *MCPServer, mcpServer *MCPServer) (admission.Warnings, error) {
	warnings, err := v.validate(ctx, mcpServer, "UPDATE")
	if err != nil {
		return warnings, err
	}
	return append(warnings, v.alphaReferenceLossWarnings(ctx, mcpServer)...), nil
}

// alphaReferenceLossWarnings warns about lossy updates without changing admission decisions.
func (v *MCPServerCustomValidator) alphaReferenceLossWarnings(ctx context.Context, mcpServer *MCPServer) admission.Warnings {
	if v.Reader == nil {
		return nil
	}
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return nil
	}
	// Equivalent matching converts beta requests to alpha too. Only warn when
	// the client actually used alpha, rather than when admission converted it.
	kind := req.Kind
	if req.RequestKind != nil {
		kind = *req.RequestKind
	}
	if kind.Group != GroupVersion.Group || kind.Version != GroupVersion.Version || kind.Kind != MCPServerKind {
		return nil
	}

	// The admission objects have already been converted to alpha and cannot
	// show beta-only references. Read the stored beta object before this update.
	stored := &v1beta1.MCPServer{}
	if err := v.Reader.Get(ctx, client.ObjectKeyFromObject(mcpServer), stored); err != nil {
		log.FromContext(ctx).Error(err, "Unable to check for BYO reference loss during v1alpha1 update",
			"mcpserver", client.ObjectKeyFromObject(mcpServer))
		return nil
	}
	if stored.Spec.WorkloadRef == nil && stored.Spec.ServiceRef == nil {
		return nil
	}
	return admission.Warnings{fmt.Sprintf(
		"Updating through %s may discard workloadRef or serviceRef; use %s instead", GroupVersion, v1beta1.GroupVersion)}
}

// ValidateDelete is a no-op for MCPServer.
func (v *MCPServerCustomValidator) ValidateDelete(_ context.Context, _ *MCPServer) (admission.Warnings, error) {
	return nil, nil
}

func (v *MCPServerCustomValidator) validate(ctx context.Context, mcpServer *MCPServer, operation string) (admission.Warnings, error) {
	logger := log.FromContext(ctx).WithName("audit")

	if v.Policy == nil {
		logger.Info("webhook_decision",
			"mcpserver", mcpServer.Name,
			"namespace", mcpServer.Namespace,
			"operation", operation,
			"allowed", true,
			"reason", "no policy configured",
		)
		return nil, nil
	}

	var allErrs field.ErrorList
	imageRef := ""

	if mcpServer.Spec.Source.ContainerImage != nil {
		imageRef = mcpServer.Spec.Source.ContainerImage.Ref

		if err := v.Policy.ValidateImageAllowlist(imageRef); err != nil {
			allErrs = append(allErrs, err)
		}

		if err := v.Policy.ValidateImageDigest(imageRef); err != nil {
			allErrs = append(allErrs, err)
		}
	}

	if runtimeErrs := v.Policy.ValidateRuntimePolicy(len(mcpServer.Spec.Config.Storage), mcpServer.Labels); len(runtimeErrs) > 0 {
		allErrs = append(allErrs, runtimeErrs...)
	}

	if secErrs := v.Policy.ValidateSecurityContext(
		mcpServer.Spec.Runtime.Security.PodSecurityContext,
		mcpServer.Spec.Runtime.Security.SecurityContext,
	); len(secErrs) > 0 {
		allErrs = append(allErrs, secErrs...)
	}

	allowed := len(allErrs) == 0
	reason := ""
	if !allowed {
		reason = allErrs.ToAggregate().Error()
	}

	logger.Info("webhook_decision",
		"mcpserver", mcpServer.Name,
		"namespace", mcpServer.Namespace,
		"operation", operation,
		"allowed", allowed,
		"reason", reason,
		"image", imageRef,
	)

	if !allowed {
		return nil, allErrs.ToAggregate()
	}
	return nil, nil
}
