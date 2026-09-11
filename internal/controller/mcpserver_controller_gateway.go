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

package controller

import (
	"context"
	"crypto/sha256"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	acv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1/applyconfiguration/api/v1beta1"
)

const gatewayBindingSuffix = "-gateway-binding"

func gatewayBindingName(mcpServerName string) string {
	name := mcpServerName + gatewayBindingSuffix
	if len(name) <= 253 {
		return name
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(mcpServerName)))[:8]
	return mcpServerName[:253-len(gatewayBindingSuffix)-1-len(hash)] + "-" + hash + gatewayBindingSuffix
}

func (r *MCPServerReconciler) cleanupGatewayBindingIfRemoved(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
) {
	if mcpServer.Spec.Gateway != nil {
		return
	}
	logger := log.FromContext(ctx)
	bindingName := gatewayBindingName(mcpServer.Name)
	existing := &mcpv1alpha1.MCPGatewayBinding{}
	if err := r.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: mcpServer.Namespace}, existing); err != nil {
		return
	}
	if ownerErr := r.validateOwnership(ctx, existing, mcpServer); ownerErr != nil {
		return
	}
	logger.Info("Deleting MCPGatewayBinding (gateway removed from spec)", "name", bindingName)
	if err := r.Delete(ctx, existing); err != nil {
		logger.Error(err, "Best-effort gateway binding cleanup failed", "name", bindingName)
	}
}

func (r *MCPServerReconciler) reconcileGatewayBinding(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
) error {
	logger := log.FromContext(ctx)

	bindingName := gatewayBindingName(mcpServer.Name)
	existing := &mcpv1alpha1.MCPGatewayBinding{}
	err := r.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: mcpServer.Namespace}, existing)

	if mcpServer.Spec.Gateway == nil {
		if err == nil {
			if ownerErr := r.validateOwnership(ctx, existing, mcpServer); ownerErr != nil {
				return ownerErr
			}
			logger.Info("Deleting MCPGatewayBinding (gateway removed from spec)", "name", bindingName)
			return r.Delete(ctx, existing)
		}
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	desired := &mcpv1alpha1.MCPGatewayBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bindingName,
			Namespace: mcpServer.Namespace,
		},
		Spec: mcpv1alpha1.MCPGatewayBindingSpec{
			MCPServerRef: mcpServer.Name,
			Provider:     mcpServer.Spec.Gateway.Provider,
			ConfigRef:    mcpServer.Spec.Gateway.ConfigRef,
		},
	}
	if err := controllerutil.SetControllerReference(mcpServer, desired, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference on MCPGatewayBinding: %w", err)
	}

	if apierrors.IsNotFound(err) {
		logger.Info("Creating MCPGatewayBinding", "name", bindingName, "provider", desired.Spec.Provider)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	if ownerErr := r.validateOwnership(ctx, existing, mcpServer); ownerErr != nil {
		return ownerErr
	}

	if existing.Spec.Provider != desired.Spec.Provider {
		logger.Info("Provider changed, deleting MCPGatewayBinding for recreation on next reconcile",
			"name", bindingName,
			"oldProvider", existing.Spec.Provider,
			"newProvider", desired.Spec.Provider)
		if err := r.Delete(ctx, existing); err != nil {
			return fmt.Errorf("deleting MCPGatewayBinding for provider change: %w", err)
		}
		// Return nil so the next reconcile creates the new binding after
		// deletion completes, avoiding AlreadyExists when finalizers are present.
		return nil
	}

	if !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		logger.Info("Updating MCPGatewayBinding", "name", bindingName)
		existing.Spec = desired.Spec
		return r.Update(ctx, existing)
	}

	return nil
}

type gatewayStatus struct {
	condition      metav1.Condition
	bindingStatus  *mcpv1beta1.GatewayBindingStatus
	gatewayAddress string
	err            error
}

// applyGatewayStatus records metrics, adjusts the available condition when the
// gateway is not yet registered, and overrides the address URL when the
// gateway provides one. Returns the (possibly updated) availableCondition and
// mcpURL. Extracting this from Reconcile keeps cyclomatic complexity down.
func (r *MCPServerReconciler) applyGatewayStatus(
	mcpServer *mcpv1beta1.MCPServer,
	gwStatus *gatewayStatus,
	availableCondition metav1.Condition,
	mcpURL string,
) (metav1.Condition, string) {
	preserveLastTransitionTime(&gwStatus.condition, mcpServer.Status.Conditions)
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		gwStatus.condition.Type, string(gwStatus.condition.Status), gwStatus.condition.Reason)

	if gwStatus.condition.Status != metav1.ConditionTrue &&
		availableCondition.Status != metav1.ConditionFalse {
		availableCondition = newAvailableCondition(
			metav1.ConditionFalse,
			ReasonGatewayNotRegistered,
			gwStatus.condition.Message,
			mcpServer.Generation,
			mcpServer.Status.Conditions,
		)
		recordCondition(mcpServer.Name, mcpServer.Namespace,
			availableCondition.Type, string(availableCondition.Status), availableCondition.Reason)
	}

	if gwStatus.gatewayAddress != "" {
		mcpURL = gwStatus.gatewayAddress
	}

	return availableCondition, mcpURL
}

// applyGatewayStatusToAC sets the gateway-related conditions and binding
// status on the apply-configuration status builder.
func applyGatewayStatusToAC(
	status *acv1beta1.MCPServerStatusApplyConfiguration,
	gwStatus *gatewayStatus,
	acceptedCondition, availableCondition, verifiedCondition metav1.Condition,
) {
	if gwStatus == nil {
		status.WithConditions(
			conditionToAC(acceptedCondition),
			conditionToAC(availableCondition),
			conditionToAC(verifiedCondition),
		)
		return
	}
	status.WithConditions(
		conditionToAC(acceptedCondition),
		conditionToAC(availableCondition),
		conditionToAC(verifiedCondition),
		conditionToAC(gwStatus.condition),
	)
	if gwStatus.bindingStatus != nil {
		status.WithGatewayBinding(
			acv1beta1.GatewayBindingStatus().
				WithName(gwStatus.bindingStatus.Name).
				WithProvider(gwStatus.bindingStatus.Provider),
		)
	}
}

func (r *MCPServerReconciler) reconcileGatewayCondition(
	ctx context.Context,
	mcpServer *mcpv1beta1.MCPServer,
) *gatewayStatus {
	if mcpServer.Spec.Gateway == nil {
		return nil
	}

	logger := log.FromContext(ctx)
	bindingName := gatewayBindingName(mcpServer.Name)

	binding := &mcpv1alpha1.MCPGatewayBinding{}
	if err := r.Get(ctx, client.ObjectKey{Name: bindingName, Namespace: mcpServer.Namespace}, binding); err != nil {
		if apierrors.IsNotFound(err) {
			return &gatewayStatus{
				condition: newCondition(
					ConditionTypeGatewayRegistered,
					metav1.ConditionFalse,
					ReasonGatewayBindingNotFound,
					"MCPGatewayBinding not found",
					mcpServer.Generation,
				),
			}
		}
		logger.Error(err, "Failed to get MCPGatewayBinding", "name", bindingName)
		return &gatewayStatus{
			condition: newCondition(
				ConditionTypeGatewayRegistered,
				metav1.ConditionFalse,
				ReasonGatewayNotRegistered,
				fmt.Sprintf("Failed to check MCPGatewayBinding: %v", err),
				mcpServer.Generation,
			),
			err: err,
		}
	}

	bs := &mcpv1beta1.GatewayBindingStatus{
		Name:     binding.Name,
		Provider: binding.Spec.Provider,
	}

	registered := meta.FindStatusCondition(binding.Status.Conditions, ConditionTypeRegistered)
	if registered == nil || registered.Status != metav1.ConditionTrue ||
		registered.ObservedGeneration < binding.Generation {
		msg := fmt.Sprintf("Waiting for gateway integration controller to register binding; "+
			"if this persists, verify that a controller for provider %q is running", binding.Spec.Provider)
		if registered != nil {
			msg = registered.Message
		}
		return &gatewayStatus{
			condition: newCondition(
				ConditionTypeGatewayRegistered,
				metav1.ConditionFalse,
				ReasonGatewayNotRegistered,
				msg,
				mcpServer.Generation,
			),
			bindingStatus: bs,
		}
	}

	return &gatewayStatus{
		condition: newCondition(
			ConditionTypeGatewayRegistered,
			metav1.ConditionTrue,
			ReasonGatewayRegistered,
			"Gateway integration is active",
			mcpServer.Generation,
		),
		bindingStatus:  bs,
		gatewayAddress: binding.Status.URL,
	}
}
