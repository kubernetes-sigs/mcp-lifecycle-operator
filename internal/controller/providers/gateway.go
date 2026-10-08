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

package providers

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
)

// Gateway API hostname length limits (RFC 1123). A hostname may be at most 253
// characters and each DNS label at most 63 characters. IsDNS1123Subdomain only
// enforces the overall 253-character bound, so the per-label limit (and the
// wildcard-prefix contribution to the overall length) are checked explicitly.
const (
	maxHostnameLength = 253
	maxLabelLength    = 63
)

// ValidateHostname checks that value is a valid Gateway API hostname before it
// is set on an HTTPRoute. A valid hostname is an RFC 1123 DNS subdomain,
// optionally prefixed with a single wildcard label ("*."), for example
// "mcp.example.com" or "*.example.com". Empty values, IP addresses, values
// longer than 253 characters, values with a DNS label longer than 63
// characters, and values containing ports, paths, schemes, spaces, uppercase or
// other invalid characters are rejected. The returned error explains why value
// is invalid.
func ValidateHostname(value string) error {
	if value == "" {
		return fmt.Errorf("hostname must not be empty")
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return fmt.Errorf("hostname %q must be a DNS name, not an IP address", value)
	}
	if len(value) > maxHostnameLength {
		return fmt.Errorf("hostname %q exceeds the maximum length of %d characters", value, maxHostnameLength)
	}
	toCheck := strings.TrimPrefix(value, "*.")
	if errs := validation.IsDNS1123Subdomain(toCheck); len(errs) > 0 {
		return fmt.Errorf("hostname %q is not a valid DNS name: %s", value, strings.Join(errs, "; "))
	}
	for label := range strings.SplitSeq(toCheck, ".") {
		if len(label) > maxLabelLength {
			return fmt.Errorf("hostname %q has a DNS label longer than %d characters", value, maxLabelLength)
		}
	}
	return nil
}

// GatewayAddress returns a public address from the Gateway's status.
// It prefers the first Hostname-type address, falling back to the first
// IPAddress. Returns empty string when no addresses are available.
func GatewayAddress(ctx context.Context, c client.Client, gwName, gwNamespace string) (string, error) {
	gw := &gatewayv1.Gateway{}
	if err := c.Get(ctx, client.ObjectKey{Name: gwName, Namespace: gwNamespace}, gw); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get Gateway %s/%s: %w", gwNamespace, gwName, err)
	}

	var firstIP string
	for _, addr := range gw.Status.Addresses {
		if addr.Type != nil && *addr.Type == gatewayv1.HostnameAddressType {
			return addr.Value, nil
		}
		if firstIP == "" {
			if addr.Type == nil || *addr.Type == gatewayv1.IPAddressType {
				firstIP = addr.Value
			}
		}
	}
	return firstIP, nil
}

const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

// SchemeFromAcceptedRoute determines the URL scheme (http or https) by
// inspecting the Gateway listener that accepted the HTTPRoute. It matches
// the parent status entry for the given Gateway name and namespace, fetches
// the Gateway, and checks the listener protocol. Falls back to "http" when
// the Gateway is not found or no matching TLS listener exists.
func SchemeFromAcceptedRoute(ctx context.Context, c client.Client, route *gatewayv1.HTTPRoute, gwName, gwNamespace string) (string, error) {
	for _, parent := range route.Status.Parents {
		if !isParentAccepted(parent) {
			continue
		}

		if string(parent.ParentRef.Name) != gwName {
			continue
		}
		parentNS := route.Namespace
		if parent.ParentRef.Namespace != nil {
			parentNS = string(*parent.ParentRef.Namespace)
		}
		if parentNS != gwNamespace {
			continue
		}

		gw := &gatewayv1.Gateway{}
		if err := c.Get(ctx, client.ObjectKey{Name: gwName, Namespace: gwNamespace}, gw); err != nil {
			if apierrors.IsNotFound(err) {
				return SchemeHTTP, nil
			}
			return "", fmt.Errorf("failed to get Gateway %s/%s: %w", gwNamespace, gwName, err)
		}

		if parent.ParentRef.SectionName != nil {
			for _, listener := range gw.Spec.Listeners {
				if listener.Name == *parent.ParentRef.SectionName {
					return ProtocolToScheme(listener.Protocol), nil
				}
			}
		}

		for _, listener := range gw.Spec.Listeners {
			if listener.Protocol == gatewayv1.HTTPSProtocolType || listener.Protocol == gatewayv1.TLSProtocolType {
				return SchemeHTTPS, nil
			}
		}

		return SchemeHTTP, nil
	}
	return SchemeHTTP, nil
}

func isParentAccepted(parent gatewayv1.RouteParentStatus) bool {
	for _, cond := range parent.Conditions {
		if cond.Type == string(gatewayv1.RouteConditionAccepted) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func ProtocolToScheme(protocol gatewayv1.ProtocolType) string {
	switch protocol {
	case gatewayv1.HTTPSProtocolType, gatewayv1.TLSProtocolType:
		return SchemeHTTPS
	default:
		return SchemeHTTP
	}
}

// IsHTTPRouteAccepted checks whether the given HTTPRoute has been accepted by
// the specified Gateway. It requires both Accepted and ResolvedRefs conditions
// to be True for the current route generation.
func IsHTTPRouteAccepted(route *gatewayv1.HTTPRoute, gwName, gwNamespace string) bool {
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

// FormatHost brackets IPv6 addresses for use in URL authorities.
// Hostnames and IPv4 addresses are returned unchanged.
func FormatHost(host string) string {
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		return "[" + host + "]"
	}
	return host
}

// UpdateBindingStatus sets the Registered condition and URL on a binding's
// status, skipping the write when nothing has changed.
func UpdateBindingStatus(
	ctx context.Context,
	sw client.StatusWriter,
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

	return sw.Update(ctx, binding)
}
