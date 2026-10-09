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

package httproute

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
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
)

func init() {
	providers.Register(ProviderName, providers.Registration{
		Factory: Setup,
		RequiredCRDs: []schema.GroupVersionKind{
			{Group: gatewayv1.GroupName, Version: "v1", Kind: "HTTPRoute"},
			{Group: gatewayv1.GroupName, Version: "v1", Kind: "Gateway"},
		},
	})
}

// allowedGatewayNamespaces is the operator-level allowlist of namespaces the
// provider is permitted to target with an HTTPRoute ParentRef. It is configured
// once at startup via SetAllowedGatewayNamespaces and consumed by Setup when the
// reconciler is constructed. Empty means unrestricted (the historical default
// behavior).
var allowedGatewayNamespaces []string

// SetAllowedGatewayNamespaces configures the trusted gateway-namespace allowlist
// applied to httproute reconcilers created by Setup. It must be called before
// providers.SetupAll. An empty or nil slice disables the control.
func SetAllowedGatewayNamespaces(namespaces []string) {
	allowedGatewayNamespaces = namespaces
}

const envAllowedGatewayNamespaces = "HTTPROUTE_ALLOWED_GATEWAY_NAMESPACES"

// ValidGatewayNamespace reports whether ns is a syntactically valid namespace
// name (RFC 1123 label). It is the single source of truth for namespace-name
// validity, shared by the startup allowlist filtering and the reconciler's
// ParentRef gating so both apply identical rules.
func ValidGatewayNamespace(ns string) bool {
	return len(validation.IsDNS1123Label(ns)) == 0
}

// ValidGatewayName reports whether name is a syntactically valid resource name
// (RFC 1123 subdomain), used for the gateway name supplied in the binding's
// ConfigMap.
func ValidGatewayName(name string) bool {
	return len(validation.IsDNS1123Subdomain(name)) == 0
}

// ParseAllowedGatewayNamespaces reads the trusted gateway-namespace allowlist
// from flagValue. flagSet reports whether the --httproute-allowed-gateway-namespaces
// flag was explicitly provided (via flag.Visit): when it was, flagValue wins even
// if empty, so an operator can override an inherited env var with an explicit ""
// to disable the control. Only when the flag was not provided does it fall back
// to the HTTPROUTE_ALLOWED_GATEWAY_NAMESPACES env var. The value is a
// comma-separated list of namespace names; blank entries are ignored. A nil
// result means the control is disabled (unrestricted).
func ParseAllowedGatewayNamespaces(flagValue string, flagSet bool) []string {
	raw := flagValue
	if !flagSet {
		raw = os.Getenv(envAllowedGatewayNamespaces)
	}
	var out []string
	for ns := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(ns); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ResolveAllowedGatewayNamespaces parses the allowlist (flag with env fallback,
// see ParseAllowedGatewayNamespaces) and validates each entry as an RFC 1123
// namespace label. It returns the valid entries and the dropped invalid entries
// (for logging). It fails closed: if the configuration is non-empty but yields no
// valid entries, it returns an error instead of silently collapsing to the
// unrestricted default, so a typo (or an uppercase value that can never match a
// lowercase namespace) is a fatal startup error rather than a silent fail-open. An
// empty/unset configuration returns nil with no error (control disabled).
func ResolveAllowedGatewayNamespaces(flagValue string, flagSet bool) (allowed, invalid []string, err error) {
	parsed := ParseAllowedGatewayNamespaces(flagValue, flagSet)
	for _, ns := range parsed {
		if ValidGatewayNamespace(ns) {
			allowed = append(allowed, ns)
		} else {
			invalid = append(invalid, ns)
		}
	}
	if len(parsed) > 0 && len(allowed) == 0 {
		return nil, invalid, fmt.Errorf(
			"httproute gateway-namespace allowlist was configured (%v) but contains no valid RFC 1123 namespace names",
			parsed)
	}
	return allowed, invalid, nil
}

// Setup creates the httproute provider controller and registers it with the manager.
func Setup(mgr ctrl.Manager) error {
	return (&Reconciler{
		Client:                   mgr.GetClient(),
		APIReader:                mgr.GetAPIReader(),
		Scheme:                   mgr.GetScheme(),
		AllowedGatewayNamespaces: allowedGatewayNamespaces,
	}).SetupWithManager(mgr)
}

const (
	// ProviderName is the provider name for the reference Gateway API HTTPRoute
	// integration controller.
	ProviderName = "httproute"

	configKeyGatewayName      = "gateway-name"
	configKeyGatewayNamespace = "gateway-namespace"
	configKeyRouteHostname    = "route-hostname"
	configKeyPublicHostname   = "public-hostname"

	reasonRouteNotAccepted = "RouteNotAccepted"
)

// Reconciler reconciles MCPGatewayBinding resources with provider "httproute".
// It creates Gateway API HTTPRoute resources that route traffic from a Gateway
// to the MCPServer's Service.
type Reconciler struct {
	client.Client
	// APIReader is an uncached reader used for ConfigMap content reads, so the
	// controller does not start a full ConfigMap informer that would cache every
	// ConfigMap's data. ConfigMaps are watched metadata-only.
	APIReader client.Reader
	Scheme    *runtime.Scheme

	// AllowedGatewayNamespaces is an opt-in allowlist of namespaces the provider
	// may target with an HTTPRoute ParentRef. The gateway namespace (and name) is
	// supplied by a tenant-controlled ConfigMap, so when this list is non-empty
	// any namespace outside it is rejected to prevent a tenant from attaching the
	// route to another tenant's Gateway. Empty means no restriction.
	AllowedGatewayNamespaces []string
}

// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpgatewaybindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpgatewaybindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpgatewaybindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mcp.x-k8s.io,resources=mcpservers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch

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

	configMap := &corev1.ConfigMap{}
	if binding.Spec.ConfigRef == "" {
		return ctrl.Result{}, r.setNotRegistered(ctx, binding,
			"spec.configRef is required for httproute provider")
	}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: binding.Spec.ConfigRef, Namespace: binding.Namespace}, configMap); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q not found", binding.Spec.ConfigRef))
	}

	gwName, gwNamespace, done, refErr := r.resolveGatewayRef(ctx, binding, configMap.Data)
	if done {
		return ctrl.Result{}, refErr
	}

	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = mcpcontroller.DefaultMCPPath
	}
	pathType := gatewayv1.PathMatchPathPrefix

	gwNS := gatewayv1.Namespace(gwNamespace)
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
						Group:     ptr.To(gatewayv1.Group(gatewayv1.GroupName)),
						Kind:      ptr.To(gatewayv1.Kind("Gateway")),
						Name:      gatewayv1.ObjectName(gwName),
						Namespace: &gwNS,
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

	if routeHostname, ok := configMap.Data[configKeyRouteHostname]; ok && routeHostname != "" {
		if err := providers.ValidateHostname(routeHostname); err != nil {
			return ctrl.Result{}, r.setNotRegistered(ctx, binding,
				fmt.Sprintf("invalid %q in ConfigMap %q: %v", configKeyRouteHostname, binding.Spec.ConfigRef, err))
		}
		httpRoute.Spec.Hostnames = []gatewayv1.Hostname{gatewayv1.Hostname(routeHostname)}
	}

	if err := controllerutil.SetControllerReference(binding, httpRoute, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("setting controller reference on HTTPRoute: %w", err)
	}

	existing := &gatewayv1.HTTPRoute{}
	err := r.Get(ctx, client.ObjectKey{Name: httpRoute.Name, Namespace: httpRoute.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		logger.Info("Creating HTTPRoute", "name", httpRoute.Name)
		if createErr := r.Create(ctx, httpRoute); createErr != nil {
			return ctrl.Result{}, createErr
		}
	} else if err != nil {
		return ctrl.Result{}, err
	} else {
		ownersBefore := existing.OwnerReferences
		if err := controllerutil.SetControllerReference(binding, existing, r.Scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("setting controller reference on existing HTTPRoute: %w", err)
		}
		ownersChanged := !equality.Semantic.DeepEqual(ownersBefore, existing.OwnerReferences)
		if ownersChanged || !equality.Semantic.DeepEqual(existing.Spec, httpRoute.Spec) {
			logger.Info("Updating HTTPRoute", "name", httpRoute.Name)
			existing.Spec = httpRoute.Spec
			if updateErr := r.Update(ctx, existing); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
		}
	}

	route := &gatewayv1.HTTPRoute{}
	if err := r.Get(ctx, client.ObjectKey{Name: httpRoute.Name, Namespace: httpRoute.Namespace}, route); err != nil {
		return ctrl.Result{}, err
	}

	if !providers.IsHTTPRouteAccepted(route, gwName, gwNamespace) {
		statusErr := providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionFalse,
			reasonRouteNotAccepted, "Waiting for gateway to accept HTTPRoute", "")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, statusErr
	}

	publicHost, addrErr := r.resolvePublicHost(ctx, configMap.Data, gwName, gwNamespace)
	if addrErr != nil {
		return ctrl.Result{}, addrErr
	}
	if publicHost == "" {
		statusErr := providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionFalse,
			mcpcontroller.ReasonPublicAddressPending,
			"Waiting for public address: no public-hostname in ConfigMap and no Gateway status address available", "")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, statusErr
	}

	scheme, schemeErr := providers.SchemeFromAcceptedRoute(ctx, r.Client, route, gwName, gwNamespace)
	if schemeErr != nil {
		return ctrl.Result{}, schemeErr
	}
	statusURL := fmt.Sprintf("%s://%s%s", scheme, providers.FormatHost(publicHost), path)

	return ctrl.Result{}, providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionTrue,
		mcpcontroller.ReasonGatewayRegistered, "HTTPRoute accepted by gateway", statusURL)
}

// resolveGatewayRef extracts and validates the gateway ParentRef coordinates
// (gateway-name and gateway-namespace) from the binding's ConfigMap data. These
// values are tenant-controlled and used verbatim as the HTTPRoute ParentRef, so
// they are format-validated (RFC 1123) and, when an allowlist is configured,
// gated against it. When the returned done flag is true the binding has already
// been handled via setNotRegistered and Reconcile should return err immediately.
func (r *Reconciler) resolveGatewayRef(
	ctx context.Context,
	binding *mcpv1alpha1.MCPGatewayBinding,
	data map[string]string,
) (gwName, gwNamespace string, done bool, err error) {
	gwName, ok := data[configKeyGatewayName]
	if !ok || gwName == "" {
		return "", "", true, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q missing required key %q", binding.Spec.ConfigRef, configKeyGatewayName))
	}
	gwNamespace, ok = data[configKeyGatewayNamespace]
	if !ok || gwNamespace == "" {
		return "", "", true, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q missing required key %q", binding.Spec.ConfigRef, configKeyGatewayNamespace))
	}

	// Always-on, cheap format validation: both values are read from a
	// tenant-controlled ConfigMap and used verbatim as the HTTPRoute ParentRef, so
	// reject syntactically invalid names before building the route. Resource names
	// are RFC 1123 subdomains; namespace names are RFC 1123 labels.
	if !ValidGatewayName(gwName) {
		return "", "", true, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("%q value %q is not a valid name (must be an RFC 1123 subdomain)", configKeyGatewayName, gwName))
	}
	if !ValidGatewayNamespace(gwNamespace) {
		return "", "", true, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("%q value %q is not a valid namespace name (must be an RFC 1123 label)", configKeyGatewayNamespace, gwNamespace))
	}

	// Opt-in trust gate: when an allowlist is configured, refuse to attach the
	// HTTPRoute to a Gateway in any namespace outside it. A tenant that controls
	// the ConfigMap could otherwise point gateway-namespace/gateway-name at
	// another tenant's Gateway, hijacking the server's routing.
	if len(r.AllowedGatewayNamespaces) > 0 && !slices.Contains(r.AllowedGatewayNamespaces, gwNamespace) {
		return "", "", true, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("%q %q is not in the operator's trusted gateway-namespace allowlist", configKeyGatewayNamespace, gwNamespace))
	}

	return gwName, gwNamespace, false, nil
}

func (r *Reconciler) setNotRegistered(
	ctx context.Context,
	binding *mcpv1alpha1.MCPGatewayBinding,
	message string,
) error {
	if err := r.deleteStaleHTTPRoute(ctx, binding); err != nil {
		return err
	}
	return providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionFalse, mcpcontroller.ReasonGatewayNotRegistered, message, "")
}

func (r *Reconciler) resolvePublicHost(ctx context.Context, configData map[string]string, gwName, gwNamespace string) (string, error) {
	if host := configData[configKeyPublicHostname]; host != "" {
		return host, nil
	}
	if host := configData[configKeyRouteHostname]; host != "" {
		return host, nil
	}
	return providers.GatewayAddress(ctx, r.Client, gwName, gwNamespace)
}

func (r *Reconciler) deleteStaleHTTPRoute(ctx context.Context, binding *mcpv1alpha1.MCPGatewayBinding) error {
	route := &gatewayv1.HTTPRoute{}
	if err := r.Get(ctx, client.ObjectKey{Name: binding.Name, Namespace: binding.Namespace}, route); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("checking for stale HTTPRoute: %w", err)
	}
	if !metav1.IsControlledBy(route, binding) {
		return nil
	}
	if err := r.Delete(ctx, route); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting stale HTTPRoute: %w", err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPGatewayBinding{}, builder.WithPredicates(providers.MatchesProvider(ProviderName))).
		Owns(&gatewayv1.HTTPRoute{}).
		WatchesMetadata(
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
		Named("mcpgatewaybinding-httproute").
		Complete(r)
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
		if err := r.APIReader.Get(ctx, client.ObjectKey{Name: b.Spec.ConfigRef, Namespace: b.Namespace}, cm); err != nil {
			// The read is uncached, so a transient failure must not silently drop
			// this binding - that would be a missed reconcile until an unrelated
			// event re-triggers it. Enqueue it anyway and let the idempotent
			// Reconcile surface and requeue the error.
			log.FromContext(ctx).V(1).Info("enqueuing binding despite uncached ConfigMap read error",
				"binding", client.ObjectKeyFromObject(b), "configRef", b.Spec.ConfigRef, "error", err)
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(b),
			})
			continue
		}
		if cm.Data[configKeyGatewayName] == obj.GetName() &&
			cm.Data[configKeyGatewayNamespace] == obj.GetNamespace() {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(b),
			})
		}
	}
	return requests
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
