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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

const (
	configKeyGatewayName      = "gateway-name"
	configKeyGatewayNamespace = "gateway-namespace"
	configKeyHostname         = "hostname"

	bindingFieldManager = "mcpgatewaybinding-httproute-controller"
)

// MCPGatewayBindingReconciler reconciles MCPGatewayBinding resources with
// provider "httproute". It creates Gateway API HTTPRoute resources that route
// traffic from a Gateway to the MCPServer's Service.
type MCPGatewayBindingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpgatewaybindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpgatewaybindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;delete

func (r *MCPGatewayBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	binding := &mcpv1alpha1.MCPGatewayBinding{}
	if err := r.Get(ctx, req.NamespacedName, binding); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if binding.Spec.Provider != ProviderHTTPRoute {
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling MCPGatewayBinding", "name", binding.Name, "namespace", binding.Namespace)

	mcpServer := &mcpv1alpha1.MCPServer{}
	if err := r.Get(ctx, client.ObjectKey{Name: binding.Spec.MCPServerRef, Namespace: binding.Namespace}, mcpServer); err != nil {
		return ctrl.Result{}, r.setRegisteredCondition(ctx, binding, metav1.ConditionFalse,
			ReasonGatewayNotRegistered,
			fmt.Sprintf("MCPServer %q not found: %v", binding.Spec.MCPServerRef, err))
	}

	configMap := &corev1.ConfigMap{}
	if binding.Spec.ConfigRef == "" {
		return ctrl.Result{}, r.setRegisteredCondition(ctx, binding, metav1.ConditionFalse,
			ReasonGatewayNotRegistered,
			"spec.configRef is required for httproute provider")
	}
	if err := r.Get(ctx, client.ObjectKey{Name: binding.Spec.ConfigRef, Namespace: binding.Namespace}, configMap); err != nil {
		return ctrl.Result{}, r.setRegisteredCondition(ctx, binding, metav1.ConditionFalse,
			ReasonGatewayNotRegistered,
			fmt.Sprintf("ConfigMap %q not found: %v", binding.Spec.ConfigRef, err))
	}

	gwName, ok := configMap.Data[configKeyGatewayName]
	if !ok || gwName == "" {
		return ctrl.Result{}, r.setRegisteredCondition(ctx, binding, metav1.ConditionFalse,
			ReasonGatewayNotRegistered,
			fmt.Sprintf("ConfigMap %q missing required key %q", binding.Spec.ConfigRef, configKeyGatewayName))
	}
	gwNamespace, ok := configMap.Data[configKeyGatewayNamespace]
	if !ok || gwNamespace == "" {
		return ctrl.Result{}, r.setRegisteredCondition(ctx, binding, metav1.ConditionFalse,
			ReasonGatewayNotRegistered,
			fmt.Sprintf("ConfigMap %q missing required key %q", binding.Spec.ConfigRef, configKeyGatewayNamespace))
	}

	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = defaultMCPPath
	}
	pathType := gatewayv1.PathMatchPathPrefix

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      binding.Name,
			Namespace: binding.Namespace,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name:      gatewayv1.ObjectName(gwName),
						Namespace: ptr.To(gatewayv1.Namespace(gwNamespace)),
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  &pathType,
								Value: &path,
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(mcpServer.Name),
									Port: ptr.To(gatewayv1.PortNumber(mcpServer.Spec.Config.Port)),
								},
							},
						},
					},
				},
			},
		},
	}

	if hostname, ok := configMap.Data[configKeyHostname]; ok && hostname != "" {
		httpRoute.Spec.Hostnames = []gatewayv1.Hostname{gatewayv1.Hostname(hostname)}
	}

	if err := controllerutil.SetControllerReference(binding, httpRoute, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("setting controller reference on HTTPRoute: %w", err)
	}

	existing := &gatewayv1.HTTPRoute{}
	err := r.Get(ctx, client.ObjectKey{Name: httpRoute.Name, Namespace: httpRoute.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		logger.Info("Creating HTTPRoute", "name", httpRoute.Name)
		if err := r.Create(ctx, httpRoute); err != nil {
			return ctrl.Result{}, r.setRegisteredCondition(ctx, binding, metav1.ConditionFalse,
				ReasonGatewayNotRegistered,
				fmt.Sprintf("Failed to create HTTPRoute: %v", err))
		}
	} else if err != nil {
		return ctrl.Result{}, err
	} else {
		if !equality.Semantic.DeepEqual(existing.Spec, httpRoute.Spec) {
			logger.Info("Updating HTTPRoute", "name", httpRoute.Name)
			existing.Spec = httpRoute.Spec
			if err := r.Update(ctx, existing); err != nil {
				return ctrl.Result{}, r.setRegisteredCondition(ctx, binding, metav1.ConditionFalse,
					ReasonGatewayNotRegistered,
					fmt.Sprintf("Failed to update HTTPRoute: %v", err))
			}
		}
	}

	url := ""
	if hostname, ok := configMap.Data[configKeyHostname]; ok && hostname != "" {
		url = fmt.Sprintf("http://%s%s", hostname, path)
	}

	return ctrl.Result{}, r.setRegisteredConditionWithURL(ctx, binding, metav1.ConditionTrue,
		ReasonGatewayRegistered, "HTTPRoute created", url)
}

func (r *MCPGatewayBindingReconciler) setRegisteredCondition(
	ctx context.Context,
	binding *mcpv1alpha1.MCPGatewayBinding,
	status metav1.ConditionStatus,
	reason, message string,
) error {
	return r.setRegisteredConditionWithURL(ctx, binding, status, reason, message, "")
}

func (r *MCPGatewayBindingReconciler) setRegisteredConditionWithURL(
	ctx context.Context,
	binding *mcpv1alpha1.MCPGatewayBinding,
	status metav1.ConditionStatus,
	reason, message, url string,
) error {
	condition := metav1.Condition{
		Type:               ConditionTypeRegistered,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: binding.Generation,
		LastTransitionTime: metav1.Now(),
	}
	preserveLastTransitionTime(&condition, binding.Status.Conditions)

	meta.SetStatusCondition(&binding.Status.Conditions, condition)
	binding.Status.URL = url

	return r.Status().Update(ctx, binding)
}

// SetupWithManager sets up the controller with the Manager.
// It checks whether the Gateway API HTTPRoute CRD is installed before
// registering. If the CRD is not available, the controller is skipped.
func (r *MCPGatewayBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	httpRouteGVK := schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "HTTPRoute",
	}

	_, err := mgr.GetRESTMapper().RESTMapping(httpRouteGVK.GroupKind(), httpRouteGVK.Version)
	if err != nil {
		setupLog := mgr.GetLogger().WithName("setup")
		setupLog.Info("Gateway API HTTPRoute CRD not found, skipping MCPGatewayBinding httproute controller. "+
			"Install Gateway API CRDs and restart the operator to enable gateway integration.",
			"gvk", httpRouteGVK.String())
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPGatewayBinding{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Named("mcpgatewaybinding-httproute").
		Complete(r)
}
