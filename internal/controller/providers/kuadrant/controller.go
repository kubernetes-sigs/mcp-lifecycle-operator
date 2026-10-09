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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
)

func init() {
	providers.Register(ProviderName, providers.Registration{
		Factory: Setup,
		RequiredCRDs: []schema.GroupVersionKind{
			{Group: gatewayv1.GroupName, Version: "v1", Kind: "HTTPRoute"},
			{Group: gatewayv1.GroupName, Version: "v1", Kind: "Gateway"},
			{Group: kuadrantapi.SchemeGroupVersion.Group, Version: kuadrantapi.SchemeGroupVersion.Version, Kind: "MCPServerRegistration"},
			{Group: kuadrantapi.SchemeGroupVersion.Group, Version: kuadrantapi.SchemeGroupVersion.Version, Kind: "MCPGatewayExtension"},
		},
	})
}

// allowedExtensionNamespaces is the operator-level allowlist of namespaces the
// provider is permitted to read MCPGatewayExtension routing config from. It is
// configured once at startup via SetAllowedExtensionNamespaces and consumed by
// Setup when the reconciler is constructed. Empty means unrestricted (the
// historical default behavior).
var allowedExtensionNamespaces []string

// SetAllowedExtensionNamespaces configures the trusted extension-namespace
// allowlist applied to kuadrant reconcilers created by Setup. It must be called
// before providers.SetupAll. An empty or nil slice disables the control.
func SetAllowedExtensionNamespaces(namespaces []string) {
	allowedExtensionNamespaces = namespaces
}

const envAllowedExtensionNamespaces = "KUADRANT_ALLOWED_EXTENSION_NAMESPACES"

// ParseAllowedExtensionNamespaces reads the trusted extension-namespace
// allowlist. When flagSet is true the flag was provided on the command line and
// its value wins (even an explicit empty value, which disables the control);
// only when flagSet is false does it fall back to the
// KUADRANT_ALLOWED_EXTENSION_NAMESPACES env var. The value is a comma-separated
// list of namespace names; blank entries are ignored. A nil result means the
// control is disabled (unrestricted).
func ParseAllowedExtensionNamespaces(flagValue string, flagSet bool) []string {
	raw := flagValue
	if !flagSet {
		raw = os.Getenv(envAllowedExtensionNamespaces)
	}
	var out []string
	for ns := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(ns); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ValidExtensionNamespace reports whether ns is a syntactically valid extension
// namespace name (an RFC 1123 label). This is the single source of truth for
// the namespace-name rule, applied both to allowlist entries at startup and to
// the tenant-supplied extension-namespace at reconcile time.
func ValidExtensionNamespace(ns string) bool {
	return len(validation.IsDNS1123Label(ns)) == 0
}

// ValidExtensionName reports whether name is a syntactically valid extension
// resource name (an RFC 1123 subdomain).
func ValidExtensionName(name string) bool {
	return len(validation.IsDNS1123Subdomain(name)) == 0
}

// PartitionExtensionNamespaces splits allowlist entries into valid and invalid
// namespace names using ValidExtensionNamespace. It is pure so the startup
// fail-closed decision (non-empty allowlist that yields no valid entries) can
// be unit-tested without a process exit.
func PartitionExtensionNamespaces(entries []string) (valid, invalid []string) {
	for _, ns := range entries {
		if ValidExtensionNamespace(ns) {
			valid = append(valid, ns)
		} else {
			invalid = append(invalid, ns)
		}
	}
	return valid, invalid
}

// ResolveAllowedExtensionNamespaces parses the allowlist (flag with env fallback,
// see ParseAllowedExtensionNamespaces) and validates each entry as an RFC 1123
// namespace label. It returns the valid entries and the dropped invalid entries
// (for logging). It fails closed: if the configuration is non-empty but yields no
// valid entries, it returns an error instead of silently collapsing to the
// unrestricted default, so a typo (or an uppercase value that can never match a
// lowercase namespace) is a fatal startup error rather than a silent fail-open. An
// empty/unset configuration returns nil with no error (control disabled).
func ResolveAllowedExtensionNamespaces(flagValue string, flagSet bool) (allowed, invalid []string, err error) {
	parsed := ParseAllowedExtensionNamespaces(flagValue, flagSet)
	allowed, invalid = PartitionExtensionNamespaces(parsed)
	if len(parsed) > 0 && len(allowed) == 0 {
		return nil, invalid, fmt.Errorf(
			"kuadrant extension-namespace allowlist was configured (%v) but contains no valid RFC 1123 namespace names",
			parsed)
	}
	return allowed, invalid, nil
}

// Setup creates the kuadrant provider controller and registers it with the manager.
func Setup(mgr ctrl.Manager) error {
	return (&Reconciler{
		Client:                     mgr.GetClient(),
		APIReader:                  mgr.GetAPIReader(),
		Scheme:                     mgr.GetScheme(),
		AllowedExtensionNamespaces: allowedExtensionNamespaces,
	}).SetupWithManager(mgr)
}

const (
	ProviderName = "kuadrant"

	configKeyExtensionName      = "extension-name"
	configKeyExtensionNamespace = "extension-namespace"
	configKeyRouteHostname      = "route-hostname"
	configKeyPrefix             = "prefix"
	configKeySectionName        = "section-name"

	reasonExtensionNotReady    = "ExtensionNotReady"
	reasonRouteNotAccepted     = "RouteNotAccepted"
	reasonRegistrationNotReady = "RegistrationNotReady"
)

// Reconciler reconciles MCPGatewayBinding resources with provider "kuadrant".
// It creates Gateway API HTTPRoute resources and Kuadrant MCPServerRegistration
// resources that register MCP servers with the Kuadrant MCP Gateway.
type Reconciler struct {
	client.Client
	// APIReader is an uncached reader used for ConfigMap content reads, so the
	// controller does not start a full ConfigMap informer that would cache every
	// ConfigMap's data. ConfigMaps are watched metadata-only.
	APIReader client.Reader
	Scheme    *runtime.Scheme

	// AllowedExtensionNamespaces is an opt-in allowlist of namespaces from which
	// the provider may read an MCPGatewayExtension. The extension namespace is
	// supplied by a tenant-controlled ConfigMap, so when this list is non-empty
	// any namespace outside it is rejected to prevent routing config from being
	// read out of an attacker-planted namespace. Empty means no restriction.
	AllowedExtensionNamespaces []string
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
	extensionName, extensionNamespace string
	sectionName                       string // optional override
	routeHostname                     string // optional
	prefix, path                      string
	prefixAutoGenerated               bool
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
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: binding.Spec.ConfigRef, Namespace: binding.Namespace}, configMap); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q not found", binding.Spec.ConfigRef))
	}

	extName, ok := configMap.Data[configKeyExtensionName]
	if !ok || extName == "" {
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q missing required key %q", binding.Spec.ConfigRef, configKeyExtensionName))
	}
	extNamespace, ok := configMap.Data[configKeyExtensionNamespace]
	if !ok || extNamespace == "" {
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("ConfigMap %q missing required key %q", binding.Spec.ConfigRef, configKeyExtensionNamespace))
	}

	// Always-on, cheap format validation: both values are read from a
	// tenant-controlled ConfigMap and used to build an object key, so reject
	// syntactically invalid names before touching the API server. Resource names
	// are RFC 1123 subdomains; namespace names are RFC 1123 labels.
	if !ValidExtensionName(extName) {
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("%q value %q is not a valid RFC 1123 name", configKeyExtensionName, extName))
	}
	if !ValidExtensionNamespace(extNamespace) {
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("%q value %q is not a valid RFC 1123 namespace name", configKeyExtensionNamespace, extNamespace))
	}

	// Opt-in trust gate: when an allowlist is configured, refuse to read routing
	// config from any namespace outside it. A tenant that controls the ConfigMap
	// could otherwise point extension-namespace at a namespace they planted an
	// MCPGatewayExtension in, hijacking the server's routing.
	if len(r.AllowedExtensionNamespaces) > 0 && !slices.Contains(r.AllowedExtensionNamespaces, extNamespace) {
		return nil, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("%q %q is not in the operator's trusted extension-namespace allowlist", configKeyExtensionNamespace, extNamespace))
	}

	prefix := configMap.Data[configKeyPrefix]
	prefixAutoGenerated := false
	if prefix == "" {
		h := sha256.Sum256([]byte(mcpServer.Name + "/" + mcpServer.Namespace))
		prefix = "mcp_" + hex.EncodeToString(h[:4]) + "_"
		prefixAutoGenerated = true
	}

	path := mcpServer.Spec.Config.Path
	if path == "" {
		path = mcpcontroller.DefaultMCPPath
	}

	return &parsedConfig{
		extensionName:       extName,
		extensionNamespace:  extNamespace,
		sectionName:         configMap.Data[configKeySectionName],
		routeHostname:       configMap.Data[configKeyRouteHostname],
		prefix:              prefix,
		path:                path,
		prefixAutoGenerated: prefixAutoGenerated,
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

	ext := &kuadrantapi.MCPGatewayExtension{}
	if err := r.Get(ctx, client.ObjectKey{Name: cfg.extensionName, Namespace: cfg.extensionNamespace}, ext); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, r.setNotRegistered(ctx, binding,
				fmt.Sprintf("MCPGatewayExtension %q not found in namespace %q", cfg.extensionName, cfg.extensionNamespace))
		}
		return ctrl.Result{}, err
	}
	if !isExtensionReady(ext) {
		msg := fmt.Sprintf("MCPGatewayExtension %q is not ready", cfg.extensionName)
		if readyCond := meta.FindStatusCondition(ext.Status.Conditions, "Ready"); readyCond != nil && readyCond.Message != "" {
			msg = readyCond.Message
		}
		statusErr := providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionFalse,
			reasonExtensionNotReady, msg, "")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, statusErr
	}

	ref := ext.Spec.TargetRef
	gwName := ref.Name
	gwNamespace := ref.Namespace
	if gwNamespace == "" {
		gwNamespace = ext.Namespace
	}

	sectionName := ref.SectionName
	if cfg.sectionName != "" {
		sectionName = cfg.sectionName
	}
	if sectionName == "" {
		return ctrl.Result{}, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("MCPGatewayExtension %q has no targetRef.sectionName; set %q in the ConfigMap", cfg.extensionName, configKeySectionName))
	}

	routeHostname := cfg.routeHostname
	if routeHostname == "" {
		var resolveErr error
		routeHostname, resolveErr = r.resolveHostname(ctx, mcpServer.Name, mcpServer.Namespace, gwName, gwNamespace, sectionName)
		if resolveErr != nil {
			return ctrl.Result{}, r.setNotRegistered(ctx, binding, resolveErr.Error())
		}
	} else if err := providers.ValidateHostname(routeHostname); err != nil {
		return ctrl.Result{}, r.setNotRegistered(ctx, binding,
			fmt.Sprintf("invalid %q in ConfigMap %q: %v", configKeyRouteHostname, binding.Spec.ConfigRef, err))
	}

	if err := r.reconcileHTTPRoute(ctx, binding, mcpServer, gwName, gwNamespace, routeHostname, sectionName, cfg.path); err != nil {
		return ctrl.Result{}, err
	}

	if msg, err := r.checkPrefixConflict(ctx, binding, cfg); err != nil {
		return ctrl.Result{}, err
	} else if msg != "" {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.setNotRegistered(ctx, binding, msg)
	}

	if err := r.reconcileMCPServerRegistration(ctx, binding, cfg.path, cfg.prefix); err != nil {
		return ctrl.Result{}, err
	}

	route := &gatewayv1.HTTPRoute{}
	if err := r.Get(ctx, client.ObjectKey{Name: binding.Name, Namespace: binding.Namespace}, route); err != nil {
		return ctrl.Result{}, err
	}

	if !providers.IsHTTPRouteAccepted(route, gwName, gwNamespace) {
		statusErr := providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionFalse,
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
		statusErr := providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionFalse,
			reasonRegistrationNotReady, msg, "")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, statusErr
	}

	ep, resolveErr := r.resolvePublicEndpoint(ctx, ext)
	if resolveErr != nil {
		return ctrl.Result{}, providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionFalse,
			mcpcontroller.ReasonGatewayNotRegistered, resolveErr.Error(), "")
	}
	if ep == nil {
		statusErr := providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionFalse,
			mcpcontroller.ReasonPublicAddressPending,
			"Waiting for public address: MCPGatewayExtension has no publicHost and no public listener hostname available", "")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, statusErr
	}

	statusURL := fmt.Sprintf("%s://%s%s", ep.scheme, providers.FormatHost(ep.host), cfg.path)

	return ctrl.Result{}, providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionTrue,
		mcpcontroller.ReasonGatewayRegistered, "HTTPRoute accepted and MCPServerRegistration ready", statusURL)
}

func isExtensionReady(ext *kuadrantapi.MCPGatewayExtension) bool {
	cond := meta.FindStatusCondition(ext.Status.Conditions, "Ready")
	return cond != nil && cond.Status == metav1.ConditionTrue
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
				Group: gatewayv1.GroupName,
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

func (r *Reconciler) checkPrefixConflict(ctx context.Context, binding *mcpv1alpha1.MCPGatewayBinding, cfg *parsedConfig) (string, error) {
	regList := &kuadrantapi.MCPServerRegistrationList{}
	if err := r.List(ctx, regList); err != nil {
		return "", err
	}
	for i := range regList.Items {
		reg := &regList.Items[i]
		if (reg.Name != binding.Name || reg.Namespace != binding.Namespace) && reg.Spec.Prefix == cfg.prefix {
			if cfg.prefixAutoGenerated {
				return fmt.Sprintf("auto-generated prefix %q is already in use by MCPServerRegistration %s/%s; "+
					"set a custom %q in ConfigMap %q to resolve this",
					cfg.prefix, reg.Namespace, reg.Name, configKeyPrefix, binding.Spec.ConfigRef), nil
			}
			return fmt.Sprintf("prefix %q is already in use by MCPServerRegistration %s/%s",
				cfg.prefix, reg.Namespace, reg.Name), nil
		}
	}
	return "", nil
}

func (r *Reconciler) setNotRegistered(
	ctx context.Context,
	binding *mcpv1alpha1.MCPGatewayBinding,
	message string,
) error {
	if err := r.deleteStaleResources(ctx, binding); err != nil {
		return err
	}
	return providers.UpdateBindingStatus(ctx, r.Status(), binding, metav1.ConditionFalse, mcpcontroller.ReasonGatewayNotRegistered, message, "")
}

func (r *Reconciler) deleteStaleResources(ctx context.Context, binding *mcpv1alpha1.MCPGatewayBinding) error {
	key := client.ObjectKey{Name: binding.Name, Namespace: binding.Namespace}

	route := &gatewayv1.HTTPRoute{}
	if err := r.Get(ctx, key, route); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("checking for stale HTTPRoute: %w", err)
		}
	} else if metav1.IsControlledBy(route, binding) {
		if err := r.Delete(ctx, route); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting stale HTTPRoute: %w", err)
		}
	}

	reg := &kuadrantapi.MCPServerRegistration{}
	reg.SetGroupVersionKind(kuadrantapi.SchemeGroupVersion.WithKind("MCPServerRegistration"))
	if err := r.Get(ctx, key, reg); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("checking for stale MCPServerRegistration: %w", err)
		}
	} else if metav1.IsControlledBy(reg, binding) {
		if err := r.Delete(ctx, reg); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting stale MCPServerRegistration: %w", err)
		}
	}

	return nil
}

// resolveHostname constructs the backend hostname from the Gateway listener's
// wildcard hostname. The namespace is included to avoid collisions when
// multiple namespaces contain MCPServers with the same name. For example,
// if the listener hostname is "*.mcp.local", the MCPServer name is "my-server"
// and its namespace is "team-a", the result is "my-server.team-a.mcp.local".
func (r *Reconciler) resolveHostname(ctx context.Context, mcpServerName, mcpServerNamespace, gwName, gwNamespace, sectionName string) (string, error) {
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
		return fmt.Sprintf("%s.%s%s", mcpServerName, mcpServerNamespace, h[1:]), nil
	}

	return "", fmt.Errorf("gateway %s/%s has no listener named %q", gwNamespace, gwName, sectionName)
}

func (r *Reconciler) resolvePublicEndpoint(ctx context.Context, ext *kuadrantapi.MCPGatewayExtension) (*publicEndpoint, error) {
	ref := ext.Spec.TargetRef
	gwNamespace := ref.Namespace
	if gwNamespace == "" {
		gwNamespace = ext.Namespace
	}

	scheme, err := r.schemeFromListener(ctx, ref.Name, gwNamespace, ref.SectionName)
	if err != nil {
		return nil, err
	}

	if ext.Spec.PublicHost != "" {
		return &publicEndpoint{host: ext.Spec.PublicHost, scheme: scheme}, nil
	}

	listenerHost := r.listenerHostname(ctx, ref.Name, gwNamespace, ref.SectionName)
	if listenerHost != "" {
		return &publicEndpoint{host: listenerHost, scheme: scheme}, nil
	}

	return nil, nil
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
			return providers.ProtocolToScheme(listener.Protocol), nil
		}
	}
	return providers.SchemeHTTP, nil
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

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPGatewayBinding{}, builder.WithPredicates(providers.MatchesProvider(ProviderName))).
		Owns(&gatewayv1.HTTPRoute{}).
		Owns(&kuadrantapi.MCPServerRegistration{}).
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
		extName := cm.Data[configKeyExtensionName]
		extNS := cm.Data[configKeyExtensionNamespace]
		if extName == "" || extNS == "" {
			continue
		}
		ext := &kuadrantapi.MCPGatewayExtension{}
		if err := r.Get(ctx, client.ObjectKey{Name: extName, Namespace: extNS}, ext); err != nil {
			continue
		}
		ref := ext.Spec.TargetRef
		gwNS := ref.Namespace
		if gwNS == "" {
			gwNS = ext.Namespace
		}
		if ref.Name == obj.GetName() && gwNS == obj.GetNamespace() {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(b),
			})
		}
	}
	return requests
}

func (r *Reconciler) findBindingsForGatewayExtension(ctx context.Context, obj client.Object) []ctrl.Request {
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
		if cm.Data[configKeyExtensionName] == obj.GetName() &&
			cm.Data[configKeyExtensionNamespace] == obj.GetNamespace() {
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
