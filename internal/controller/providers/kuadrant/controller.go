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

package kuadrant

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers"
	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
)

func init() {
	providers.Register(ProviderName, Setup)
}

// Setup creates the kuadrant provider controller and registers it with the manager.
func Setup(mgr ctrl.Manager) error {
	if err := kuadrantapi.AddToScheme(mgr.GetScheme()); err != nil {
		return fmt.Errorf("registering Kuadrant types: %w", err)
	}
	return (&Reconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)
}

const (
	ProviderName = "kuadrant"

	configKeyGatewayName      = "gateway-name"
	configKeyGatewayNamespace = "gateway-namespace"
	configKeyRouteHostname    = "route-hostname"
	configKeyPublicHostname   = "public-hostname"
	configKeyPrefix           = "prefix"
	configKeySectionName      = "section-name"

	defaultSectionName = "mcps"

	reasonRouteNotAccepted     = "RouteNotAccepted"
	reasonRegistrationNotReady = "RegistrationNotReady"
)

// Reconciler reconciles MCPGatewayBinding resources with provider "kuadrant".
// It creates Gateway API HTTPRoute resources and Kuadrant MCPServerRegistration
// resources that register MCP servers with the Kuadrant MCP Gateway.
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpgatewaybindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpgatewaybindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpgatewaybindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpservers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=mcp.kuadrant.io,resources=mcpserverregistrations,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=mcp.kuadrant.io,resources=mcpgatewayextensions,verbs=get;list;watch

type parsedConfig struct {
	gwName, gwNamespace, sectionName string
	routeHostname                    string
	publicHostname                   string
	prefix, path                     string
}

type publicEndpoint struct {
	host   string
	scheme string
}

func (r *Reconciler) parseConfig(ctx context.Context, binding *mcpv1alpha1.MCPGatewayBinding, mcpServer *mcpv1beta1.MCPServer) (*parsedConfig, error) {
	configMap := &corev1.ConfigMap{}
	if binding.Spec.ConfigRef == "" {
		return nil, r.setNotRegistered(ctx, binding,
			"spec.configRef is required for kuadrant provider")
	}
	if err := r.Get(ctx, client.ObjectKey{Name: binding.Spec.ConfigRef, Namespace: binding.Namespace}, configMap); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q not found", binding.Spec.ConfigRef))
	}

	gwName, ok := configMap.Data[configKeyGatewayName]
	if !ok || gwName == "" {
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q missing required key %q", binding.Spec.ConfigRef, configKeyGatewayName))
	}
	gwNamespace, ok := configMap.Data[configKeyGatewayNamespace]
	if !ok || gwNamespace == "" {
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q missing required key %q", binding.Spec.ConfigRef, configKeyGatewayNamespace))
	}
	sectionName := defaultSectionName
	if sn, ok := configMap.Data[configKeySectionName]; ok && sn != "" {
		sectionName = sn
	}

	prefix, ok := configMap.Data[configKeyPrefix]
	if !ok || prefix == "" {
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q missing required key %q", binding.Spec.ConfigRef, configKeyPrefix))
	}

	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = mcpcontroller.DefaultMCPPath
	}

	return &parsedConfig{
		gwName:         gwName,
		gwNamespace:    gwNamespace,
		sectionName:    sectionName,
		routeHostname:  configMap.Data[configKeyRouteHostname],
		publicHostname: configMap.Data[configKeyPublicHostname],
		prefix:         prefix,
		path:           path,
	}, nil
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	binding := &mcpv1alpha1.MCPGatewayBinding{}
	if err := r.Get(ctx, req.NamespacedName, binding); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if binding.Spec.Provider != ProviderName {
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling MCPGatewayBinding", "name", binding.Name, "namespace", binding.Namespace)

	mcpServer := &mcpv1beta1.MCPServer{}
	if err := r.Get(ctx, client.ObjectKey{Name: binding.Spec.MCPServerRef, Namespace: binding.Namespace}, mcpServer); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	cfg, err := r.parseConfig(ctx, binding, mcpServer)
	if cfg == nil {
		return ctrl.Result{}, err
	}

	routeHostname := cfg.routeHostname
	if routeHostname == "" {
		var resolveErr error
		routeHostname, resolveErr = r.resolveHostname(ctx, mcpServer.Name, cfg.gwName, cfg.gwNamespace, cfg.sectionName)
		if resolveErr != nil {
			return ctrl.Result{}, r.setNotRegistered(ctx, binding, resolveErr.Error())
		}
	}

	if err := r.reconcileHTTPRoute(ctx, binding, mcpServer, cfg.gwName, cfg.gwNamespace, routeHostname, cfg.sectionName, cfg.path); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileMCPServerRegistration(ctx, binding, cfg.path, cfg.prefix); err != nil {
		return ctrl.Result{}, err
	}

	route := &gatewayv1.HTTPRoute{}
	if err := r.Get(ctx, client.ObjectKey{Name: binding.Name, Namespace: binding.Namespace}, route); err != nil {
		return ctrl.Result{}, err
	}

	if !isHTTPRouteAccepted(route, cfg.gwName, cfg.gwNamespace) {
		statusErr := r.updateBindingStatus(ctx, binding, metav1.ConditionFalse,
			reasonRouteNotAccepted, "Waiting for gateway to accept HTTPRoute", "")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, statusErr
	}

	reg := &kuadrantapi.MCPServerRegistration{}
	reg.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPServerRegistration"))
	if err := r.Get(ctx, client.ObjectKey{Name: binding.Name, Namespace: binding.Namespace}, reg); err != nil {
		return ctrl.Result{}, err
	}
	if !isRegistrationReady(reg) {
		msg := "Waiting for MCPServerRegistration to become ready"
		if readyCond := meta.FindStatusCondition(reg.Status.Conditions, "Ready"); readyCond != nil {
			msg = readyCond.Message
		}
		statusErr := r.updateBindingStatus(ctx, binding, metav1.ConditionFalse,
			reasonRegistrationNotReady, msg, "")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, statusErr
	}

	ep, resolveErr := r.resolvePublicEndpoint(ctx, cfg)
	if resolveErr != nil {
		return ctrl.Result{}, r.updateBindingStatus(ctx, binding, metav1.ConditionFalse,
			mcpcontroller.ReasonGatewayNotRegistered, resolveErr.Error(), "")
	}
	if ep == nil {
		statusErr := r.updateBindingStatus(ctx, binding, metav1.ConditionFalse,
			mcpcontroller.ReasonPublicAddressPending,
			"Waiting for public address: no public-hostname in ConfigMap, no MCPGatewayExtension publicHost, and no public listener hostname available", "")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, statusErr
	}

	statusURL := fmt.Sprintf("%s://%s%s", ep.scheme, providers.FormatHost(ep.host), cfg.path)

	return ctrl.Result{}, r.updateBindingStatus(ctx, binding, metav1.ConditionTrue,
		mcpcontroller.ReasonGatewayRegistered, "HTTPRoute accepted and MCPServerRegistration ready", statusURL)
}

func (r *Reconciler) reconcileHTTPRoute(
	ctx context.Context,
	binding *mcpv1alpha1.MCPGatewayBinding,
	mcpServer *mcpv1beta1.MCPServer,
	gwName, gwNamespace, hostname, sectionName, path string,
) error {
	logger := log.FromContext(ctx)

	pathType := gatewayv1.PathMatchPathPrefix
	gwNS := gatewayv1.Namespace(gwNamespace)
	sn := gatewayv1.SectionName(sectionName)
	port := mcpServer.Spec.Config.Port

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      binding.Name,
			Namespace: binding.Namespace,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Group:       ptr.To(gatewayv1.Group(gatewayv1.GroupName)),
						Kind:        ptr.To(gatewayv1.Kind("Gateway")),
						Name:        gatewayv1.ObjectName(gwName),
						Namespace:   &gwNS,
						SectionName: &sn,
					},
				},
			},
			Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(hostname)},
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
									Group: ptr.To(gatewayv1.Group("")),
									Kind:  ptr.To(gatewayv1.Kind("Service")),
									Name:  gatewayv1.ObjectName(mcpServer.Name),
									Port:  &port,
								},
								Weight: ptr.To(int32(1)), //nolint:modernize // value is 1, not zero
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(binding, httpRoute, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference on HTTPRoute: %w", err)
	}

	existing := &gatewayv1.HTTPRoute{}
	err := r.Get(ctx, client.ObjectKey{Name: httpRoute.Name, Namespace: httpRoute.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		logger.Info("Creating HTTPRoute", "name", httpRoute.Name)
		if createErr := r.Create(ctx, httpRoute); createErr != nil {
			return createErr
		}
		return nil
	}
	if err != nil {
		return err
	}

	ownersBefore := existing.OwnerReferences
	if err := controllerutil.SetControllerReference(binding, existing, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference on existing HTTPRoute: %w", err)
	}
	ownersChanged := !equality.Semantic.DeepEqual(ownersBefore, existing.OwnerReferences)
	if ownersChanged || !equality.Semantic.DeepEqual(existing.Spec, httpRoute.Spec) {
		logger.Info("Updating HTTPRoute", "name", httpRoute.Name)
		existing.Spec = httpRoute.Spec
		if updateErr := r.Update(ctx, existing); updateErr != nil {
			return updateErr
		}
	}
	return nil
}

func (r *Reconciler) reconcileMCPServerRegistration(
	ctx context.Context,
	binding *mcpv1alpha1.MCPGatewayBinding,
	path, prefix string,
) error {
	logger := log.FromContext(ctx)

	reg := &kuadrantapi.MCPServerRegistration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      binding.Name,
			Namespace: binding.Namespace,
		},
		Spec: kuadrantapi.MCPServerRegistrationSpec{
			TargetRef: kuadrantapi.TargetReference{
				Group: "gateway.networking.k8s.io",
				Kind:  "HTTPRoute",
				Name:  binding.Name,
			},
			Path:   path,
			Prefix: prefix,
			State:  "Enabled",
		},
	}
	reg.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPServerRegistration"))

	if err := controllerutil.SetControllerReference(binding, reg, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference on MCPServerRegistration: %w", err)
	}

	existing := &kuadrantapi.MCPServerRegistration{}
	err := r.Get(ctx, client.ObjectKey{Name: reg.Name, Namespace: reg.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		logger.Info("Creating MCPServerRegistration", "name", reg.Name)
		if createErr := r.Create(ctx, reg); createErr != nil {
			return createErr
		}
		return nil
	}
	if err != nil {
		return err
	}

	ownersBefore := existing.OwnerReferences
	if err := controllerutil.SetControllerReference(binding, existing, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference on existing MCPServerRegistration: %w", err)
	}
	ownersChanged := !equality.Semantic.DeepEqual(ownersBefore, existing.OwnerReferences)
	if ownersChanged || !equality.Semantic.DeepEqual(existing.Spec, reg.Spec) {
		logger.Info("Updating MCPServerRegistration", "name", reg.Name)
		existing.Spec = reg.Spec
		if updateErr := r.Update(ctx, existing); updateErr != nil {
			return updateErr
		}
	}
	return nil
}

func (r *Reconciler) setNotRegistered(
	ctx context.Context,
	binding *mcpv1alpha1.MCPGatewayBinding,
	message string,
) error {
	if err := r.deleteStaleResources(ctx, binding); err != nil {
		return err
	}
	return r.updateBindingStatus(ctx, binding, metav1.ConditionFalse, mcpcontroller.ReasonGatewayNotRegistered, message, "")
}

func (r *Reconciler) deleteStaleResources(ctx context.Context, binding *mcpv1alpha1.MCPGatewayBinding) error {
	key := client.ObjectKey{Name: binding.Name, Namespace: binding.Namespace}

	route := &gatewayv1.HTTPRoute{}
	if err := r.Get(ctx, key, route); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("checking for stale HTTPRoute: %w", err)
		}
	} else if err := r.Delete(ctx, route); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting stale HTTPRoute: %w", err)
	}

	reg := &kuadrantapi.MCPServerRegistration{}
	reg.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPServerRegistration"))
	if err := r.Get(ctx, key, reg); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("checking for stale MCPServerRegistration: %w", err)
		}
	} else if err := r.Delete(ctx, reg); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting stale MCPServerRegistration: %w", err)
	}

	return nil
}

// resolveHostname constructs the backend hostname from the Gateway listener's
// wildcard hostname. For example, if the listener hostname is "*.mcp.local" and
// the MCPServer name is "my-server", the result is "my-server.mcp.local".
func (r *Reconciler) resolveHostname(ctx context.Context, mcpServerName, gwName, gwNamespace, sectionName string) (string, error) {
	gw := &gatewayv1.Gateway{}
	if err := r.Get(ctx, client.ObjectKey{Name: gwName, Namespace: gwNamespace}, gw); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("gateway %s/%s not found", gwNamespace, gwName)
		}
		return "", err
	}

	for _, listener := range gw.Spec.Listeners {
		if string(listener.Name) != sectionName {
			continue
		}
		if listener.Hostname == nil {
			return "", fmt.Errorf("gateway listener %q has no hostname; set %q in the ConfigMap", sectionName, configKeyRouteHostname)
		}
		h := string(*listener.Hostname)
		if !strings.HasPrefix(h, "*.") {
			return "", fmt.Errorf("gateway listener %q hostname %q is not a wildcard; set %q in the ConfigMap", sectionName, h, configKeyRouteHostname)
		}
		return mcpServerName + h[1:], nil
	}

	return "", fmt.Errorf("gateway %s/%s has no listener named %q", gwNamespace, gwName, sectionName)
}

// resolvePublicEndpoint determines the public host and scheme for the status URL.
// Fallback chain: ConfigMap public-hostname → MCPGatewayExtension.publicHost →
// public listener hostname.
// Returns nil when no source provides a public hostname (caller should requeue).
func (r *Reconciler) resolvePublicEndpoint(ctx context.Context, cfg *parsedConfig) (*publicEndpoint, error) {
	if cfg.publicHostname != "" {
		scheme, err := r.schemeFromListener(ctx, cfg.gwName, cfg.gwNamespace, cfg.sectionName)
		if err != nil {
			return nil, err
		}
		return &publicEndpoint{host: cfg.publicHostname, scheme: scheme}, nil
	}

	ext, err := r.findMCPGatewayExtension(ctx, cfg.gwName, cfg.gwNamespace)
	if err != nil {
		return nil, err
	}

	if ext != nil {
		scheme := r.schemeFromExtension(ctx, cfg.gwName, cfg.gwNamespace, cfg.sectionName, ext)

		if ext.Spec.PublicHost != "" {
			return &publicEndpoint{host: ext.Spec.PublicHost, scheme: scheme}, nil
		}

		listenerHost := r.listenerHostname(ctx, cfg.gwName, cfg.gwNamespace, ext.Spec.TargetRef.SectionName)
		if listenerHost != "" {
			return &publicEndpoint{host: listenerHost, scheme: scheme}, nil
		}
	}

	return nil, nil
}

// findMCPGatewayExtension finds the MCPGatewayExtension targeting the given
// Gateway. It matches on Gateway identity only, ignoring sectionName — the
// extension and the config may legitimately target different listeners (e.g.
// public vs internal). When multiple extensions target the same Gateway, it
// returns an error directing the user to set public-hostname explicitly.
func (r *Reconciler) findMCPGatewayExtension(ctx context.Context, gwName, gwNamespace string) (*kuadrantapi.MCPGatewayExtension, error) {
	extList := &kuadrantapi.MCPGatewayExtensionList{}
	if err := r.List(ctx, extList); err != nil {
		return nil, fmt.Errorf("listing MCPGatewayExtensions: %w", err)
	}

	var matches []kuadrantapi.MCPGatewayExtension
	for _, ext := range extList.Items {
		ref := ext.Spec.TargetRef
		if ref.Group != "" && ref.Group != gatewayv1.GroupName {
			continue
		}
		refNS := ref.Namespace
		if refNS == "" {
			refNS = ext.Namespace
		}
		if ref.Kind != "Gateway" || ref.Name != gwName || refNS != gwNamespace {
			continue
		}
		matches = append(matches, ext)
	}

	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("multiple MCPGatewayExtensions target gateway %s/%s; set %q in the ConfigMap to specify the public hostname", gwNamespace, gwName, configKeyPublicHostname)
	}
}

func (r *Reconciler) schemeFromListener(ctx context.Context, gwName, gwNamespace, sectionName string) (string, error) {
	gw := &gatewayv1.Gateway{}
	if err := r.Get(ctx, client.ObjectKey{Name: gwName, Namespace: gwNamespace}, gw); err != nil {
		if apierrors.IsNotFound(err) {
			return providers.SchemeHTTP, nil
		}
		return "", fmt.Errorf("failed to get Gateway %s/%s: %w", gwNamespace, gwName, err)
	}
	for _, listener := range gw.Spec.Listeners {
		if string(listener.Name) == sectionName {
			return protocolToScheme(listener.Protocol), nil
		}
	}
	return providers.SchemeHTTP, nil
}

func (r *Reconciler) schemeFromExtension(ctx context.Context, gwName, gwNamespace, configSectionName string, ext *kuadrantapi.MCPGatewayExtension) string {
	sectionName := ext.Spec.TargetRef.SectionName
	if sectionName == "" {
		sectionName = configSectionName
	}
	if sectionName == "" {
		return providers.SchemeHTTP
	}
	scheme, err := r.schemeFromListener(ctx, gwName, gwNamespace, sectionName)
	if err != nil {
		return providers.SchemeHTTP
	}
	return scheme
}

func (r *Reconciler) listenerHostname(ctx context.Context, gwName, gwNamespace, sectionName string) string {
	if sectionName == "" {
		return ""
	}
	gw := &gatewayv1.Gateway{}
	if err := r.Get(ctx, client.ObjectKey{Name: gwName, Namespace: gwNamespace}, gw); err != nil {
		return ""
	}
	for _, listener := range gw.Spec.Listeners {
		if string(listener.Name) == sectionName && listener.Hostname != nil {
			h := string(*listener.Hostname)
			if !strings.HasPrefix(h, "*.") {
				return h
			}
		}
	}
	return ""
}

func protocolToScheme(protocol gatewayv1.ProtocolType) string {
	switch protocol {
	case gatewayv1.HTTPSProtocolType, gatewayv1.TLSProtocolType:
		return providers.SchemeHTTPS
	default:
		return providers.SchemeHTTP
	}
}

func (r *Reconciler) updateBindingStatus(
	ctx context.Context,
	binding *mcpv1alpha1.MCPGatewayBinding,
	status metav1.ConditionStatus,
	reason, message, url string,
) error {
	existing := meta.FindStatusCondition(binding.Status.Conditions, mcpcontroller.ConditionTypeRegistered)
	if existing != nil && existing.Status == status && existing.Reason == reason &&
		existing.Message == message && binding.Status.URL == url &&
		existing.ObservedGeneration == binding.Generation {
		return nil
	}

	condition := metav1.Condition{
		Type:               mcpcontroller.ConditionTypeRegistered,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: binding.Generation,
	}
	meta.SetStatusCondition(&binding.Status.Conditions, condition)
	binding.Status.URL = url

	return r.Status().Update(ctx, binding)
}

// SetupWithManager sets up the controller with the Manager.
// It checks whether the Gateway API HTTPRoute CRD and the Kuadrant
// MCPServerRegistration CRD are installed before registering.
// If either CRD is not available, the controller is skipped.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	setupLog := mgr.GetLogger().WithName("setup")

	httpRouteGVK := schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "HTTPRoute",
	}
	if _, err := mgr.GetRESTMapper().RESTMapping(httpRouteGVK.GroupKind(), httpRouteGVK.Version); err != nil {
		if meta.IsNoMatchError(err) {
			setupLog.Info("Gateway API HTTPRoute CRD not found, skipping MCPGatewayBinding kuadrant controller. "+
				"Install Gateway API CRDs and restart the operator to enable Kuadrant gateway integration.",
				"gvk", httpRouteGVK.String())
			return nil
		}
		return fmt.Errorf("checking for HTTPRoute CRD: %w", err)
	}

	regGVK := schema.GroupVersionKind{
		Group:   "mcp.kuadrant.io",
		Version: "v1alpha1",
		Kind:    "MCPServerRegistration",
	}
	if _, err := mgr.GetRESTMapper().RESTMapping(regGVK.GroupKind(), regGVK.Version); err != nil {
		if meta.IsNoMatchError(err) {
			setupLog.Info("Kuadrant MCPServerRegistration CRD not found, skipping MCPGatewayBinding kuadrant controller. "+
				"Install Kuadrant MCP Gateway CRDs and restart the operator to enable Kuadrant gateway integration.",
				"gvk", regGVK.String())
			return nil
		}
		return fmt.Errorf("checking for MCPServerRegistration CRD: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPGatewayBinding{}, builder.WithPredicates(providers.MatchesProvider(ProviderName))).
		Owns(&gatewayv1.HTTPRoute{}).
		Owns(&kuadrantapi.MCPServerRegistration{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findBindingsForConfigMap),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&mcpv1beta1.MCPServer{},
			handler.EnqueueRequestsFromMapFunc(r.findBindingsForMCPServer),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&gatewayv1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(r.findBindingsForGateway),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&kuadrantapi.MCPGatewayExtension{},
			handler.EnqueueRequestsFromMapFunc(r.findBindingsForGatewayExtension),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("mcpgatewaybinding-kuadrant").
		Complete(r)
}

func isRegistrationReady(reg *kuadrantapi.MCPServerRegistration) bool {
	cond := meta.FindStatusCondition(reg.Status.Conditions, "Ready")
	return cond != nil && cond.Status == metav1.ConditionTrue
}

func isHTTPRouteAccepted(route *gatewayv1.HTTPRoute, gwName, gwNamespace string) bool {
	for _, parent := range route.Status.Parents {
		if string(parent.ParentRef.Name) != gwName {
			continue
		}
		ns := route.Namespace
		if parent.ParentRef.Namespace != nil {
			ns = string(*parent.ParentRef.Namespace)
		}
		if ns != gwNamespace {
			continue
		}

		accepted := false
		resolvedRefs := false
		for _, cond := range parent.Conditions {
			if cond.ObservedGeneration < route.Generation {
				continue
			}
			if cond.Type == string(gatewayv1.RouteConditionAccepted) &&
				cond.Status == metav1.ConditionTrue {
				accepted = true
			}
			if cond.Type == string(gatewayv1.RouteConditionResolvedRefs) &&
				cond.Status == metav1.ConditionTrue {
				resolvedRefs = true
			}
		}
		if accepted && resolvedRefs {
			return true
		}
	}
	return false
}

func (r *Reconciler) findBindingsForConfigMap(ctx context.Context, obj client.Object) []ctrl.Request {
	bindingList := &mcpv1alpha1.MCPGatewayBindingList{}
	if err := r.List(ctx, bindingList, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var requests []ctrl.Request
	for i := range bindingList.Items {
		if bindingList.Items[i].Spec.Provider == ProviderName &&
			bindingList.Items[i].Spec.ConfigRef == obj.GetName() {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(&bindingList.Items[i]),
			})
		}
	}
	return requests
}

func (r *Reconciler) findBindingsForGateway(ctx context.Context, obj client.Object) []ctrl.Request {
	return r.findBindingsForGatewayByNamespace(ctx, obj.GetName(), obj.GetNamespace())
}

func (r *Reconciler) findBindingsForGatewayByNamespace(ctx context.Context, gwName, gwNamespace string) []ctrl.Request {
	bindingList := &mcpv1alpha1.MCPGatewayBindingList{}
	if err := r.List(ctx, bindingList); err != nil {
		return nil
	}
	var requests []ctrl.Request
	for i := range bindingList.Items {
		b := &bindingList.Items[i]
		if b.Spec.Provider != ProviderName || b.Spec.ConfigRef == "" {
			continue
		}
		cm := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{Name: b.Spec.ConfigRef, Namespace: b.Namespace}, cm); err != nil {
			continue
		}
		if cm.Data[configKeyGatewayName] == gwName &&
			cm.Data[configKeyGatewayNamespace] == gwNamespace {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(b),
			})
		}
	}
	return requests
}

func (r *Reconciler) findBindingsForGatewayExtension(ctx context.Context, obj client.Object) []ctrl.Request {
	ext, ok := obj.(*kuadrantapi.MCPGatewayExtension)
	if !ok {
		return nil
	}
	ref := ext.Spec.TargetRef
	if ref.Kind != "Gateway" || ref.Name == "" {
		return nil
	}
	// An empty targetRef.namespace defaults to the extension's own namespace,
	// matching how resolvePublicHostname selects the extension. Without this,
	// a co-located extension (namespace omitted) that the resolver does match
	// would never re-enqueue its bindings on change, leaving status stale.
	gwNamespace := ref.Namespace
	if gwNamespace == "" {
		gwNamespace = ext.Namespace
	}
	return r.findBindingsForGatewayByNamespace(ctx, ref.Name, gwNamespace)
}

func (r *Reconciler) findBindingsForMCPServer(ctx context.Context, obj client.Object) []ctrl.Request {
	bindingList := &mcpv1alpha1.MCPGatewayBindingList{}
	if err := r.List(ctx, bindingList, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var requests []ctrl.Request
	for i := range bindingList.Items {
		if bindingList.Items[i].Spec.Provider == ProviderName &&
			bindingList.Items[i].Spec.MCPServerRef == obj.GetName() {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(&bindingList.Items[i]),
			})
		}
	}
	return requests
}
