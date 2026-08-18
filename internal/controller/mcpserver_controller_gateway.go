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
	acv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1/applyconfiguration/api/v1alpha1"
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

func (r *MCPServerReconciler) reconcileGatewayBinding(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
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
			Provider:     mcpServer.Spec.Gateway.ClassName,
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
		logger.Info("Provider changed, recreating MCPGatewayBinding",
			"name", bindingName,
			"oldProvider", existing.Spec.Provider,
			"newProvider", desired.Spec.Provider)
		if err := r.Delete(ctx, existing); err != nil {
			return fmt.Errorf("deleting MCPGatewayBinding for provider change: %w", err)
		}
		return r.Create(ctx, desired)
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
	bindingStatus  *mcpv1alpha1.GatewayBindingStatus
	gatewayAddress string
}

// applyGatewayStatus records metrics, adjusts the ready condition when the
// gateway is not yet registered, and overrides the address URL when the
// gateway provides one. Returns the (possibly updated) readyCondition and
// mcpURL. Extracting this from Reconcile keeps cyclomatic complexity down.
func (r *MCPServerReconciler) applyGatewayStatus(
	mcpServer *mcpv1alpha1.MCPServer,
	gwStatus *gatewayStatus,
	readyCondition metav1.Condition,
	mcpURL string,
) (metav1.Condition, string) {
	preserveLastTransitionTime(&gwStatus.condition, mcpServer.Status.Conditions)
	recordCondition(mcpServer.Name, mcpServer.Namespace,
		gwStatus.condition.Type, string(gwStatus.condition.Status), gwStatus.condition.Reason)

	if gwStatus.condition.Status != metav1.ConditionTrue &&
		readyCondition.Status != metav1.ConditionFalse {
		readyCondition = newReadyCondition(
			metav1.ConditionFalse,
			ReasonGatewayNotRegistered,
			gwStatus.condition.Message,
			mcpServer.Generation,
			mcpServer.Status.Conditions,
		)
		recordCondition(mcpServer.Name, mcpServer.Namespace,
			readyCondition.Type, string(readyCondition.Status), readyCondition.Reason)
	}

	if gwStatus.gatewayAddress != "" {
		mcpURL = gwStatus.gatewayAddress
	}

	return readyCondition, mcpURL
}

// applyGatewayStatusToAC sets the gateway-related conditions and binding
// status on the apply-configuration status builder.
func applyGatewayStatusToAC(
	status *acv1alpha1.MCPServerStatusApplyConfiguration,
	gwStatus *gatewayStatus,
	acceptedCondition, readyCondition metav1.Condition,
) {
	if gwStatus == nil {
		status.WithConditions(
			conditionToAC(acceptedCondition),
			conditionToAC(readyCondition),
		)
		return
	}
	status.WithConditions(
		conditionToAC(acceptedCondition),
		conditionToAC(readyCondition),
		conditionToAC(gwStatus.condition),
	)
	if gwStatus.bindingStatus != nil {
		status.WithGatewayBinding(
			acv1alpha1.GatewayBindingStatus().
				WithName(gwStatus.bindingStatus.Name).
				WithProvider(gwStatus.bindingStatus.Provider),
		)
	}
}

func (r *MCPServerReconciler) reconcileGatewayCondition(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
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
		}
	}

	bs := &mcpv1alpha1.GatewayBindingStatus{
		Name:     binding.Name,
		Provider: binding.Spec.Provider,
	}

	registered := meta.FindStatusCondition(binding.Status.Conditions, ConditionTypeRegistered)
	if registered == nil || registered.Status != metav1.ConditionTrue {
		msg := "Waiting for gateway integration controller to register binding"
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
