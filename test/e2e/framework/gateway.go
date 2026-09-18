//go:build e2e && e2e_gateway

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

package framework

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	kuadrantapi "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant/api"
)

// ProviderConfig describes a gateway provider for conformance testing.
type ProviderConfig struct {
	Name       string
	ConfigData map[string]string
}

var providers = map[string]ProviderConfig{
	"httproute": {
		Name: "httproute",
		ConfigData: map[string]string{
			"gateway-name":      "e2e-gateway",
			"gateway-namespace": "gateway-system",
			"gateway-class":     "eg",
			"route-hostname":    "mcp.e2e.test",
			"public-hostname":   "mcp.e2e.test",
		},
	},
	"kuadrant": {
		Name: "kuadrant",
		ConfigData: map[string]string{
			"gateway-name":      "mcp-gateway",
			"gateway-namespace": "gateway-system",
			"gateway-class":     "istio",
			"route-hostname":    "mcp.e2e.test",
			"public-hostname":   "mcp.e2e.test",
			"prefix":            "e2e_",
		},
	},
}

// ActiveProvider returns the ProviderConfig selected by the GATEWAY_PROVIDER
// environment variable. It fatals if the variable is unset or unknown.
func ActiveProvider(t *testing.T) ProviderConfig {
	t.Helper()
	name := os.Getenv("GATEWAY_PROVIDER")
	if name == "" {
		t.Fatal("GATEWAY_PROVIDER environment variable is not set")
	}
	prov, ok := providers[name]
	if !ok {
		t.Fatalf("unknown gateway provider %q, available: %v", name, providerNames())
	}
	return prov
}

func providerNames() []string {
	names := make([]string, 0, len(providers))
	for n := range providers {
		names = append(names, n)
	}
	return names
}

// NewGatewayRequest builds an *http.Request that targets the gateway's
// LoadBalancer address directly. The Host header is set to hostname so
// the gateway's listener can perform host-based routing.
func NewGatewayRequest(t *testing.T, gatewayAddress, method, path, hostname string) *http.Request {
	t.Helper()

	u := fmt.Sprintf("http://%s%s", gatewayAddress, path)
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		t.Fatalf("failed to create gateway request: %v", err)
	}
	req.Host = hostname
	return req
}

type hostOverrideTransport struct {
	base http.RoundTripper
	host string
}

func (t *hostOverrideTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Host = t.host
	return t.base.RoundTrip(req)
}

// WithHostOverride wraps the given http.Client's transport so that every
// outgoing request sets the Host header to the given value. This is needed
// when the MCP SDK client creates its own requests internally and the
// gateway performs host-based routing.
func WithHostOverride(c *http.Client, host string) *http.Client {
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Transport: &hostOverrideTransport{base: base, host: host},
	}
}

// MCPGatewayExtensionOption configures an MCPGatewayExtension for testing.
type MCPGatewayExtensionOption func(*kuadrantapi.MCPGatewayExtension)

// WithPublicHost sets the publicHost on the MCPGatewayExtension.
func WithPublicHost(host string) MCPGatewayExtensionOption {
	return func(ext *kuadrantapi.MCPGatewayExtension) {
		ext.Spec.PublicHost = host
	}
}

// WithSectionName sets the sectionName on the MCPGatewayExtension's targetRef.
func WithSectionName(name string) MCPGatewayExtensionOption {
	return func(ext *kuadrantapi.MCPGatewayExtension) {
		ext.Spec.TargetRef.SectionName = name
	}
}

// CreateMCPGatewayExtension creates an MCPGatewayExtension that targets the
// given Gateway. The caller must ensure the kuadrant scheme is registered.
func CreateMCPGatewayExtension(ctx context.Context, t *testing.T, cfg *envconf.Config,
	name, namespace, gatewayName, gatewayNamespace string, opts ...MCPGatewayExtensionOption) *kuadrantapi.MCPGatewayExtension {
	t.Helper()

	ext := &kuadrantapi.MCPGatewayExtension{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "mcp.kuadrant.io/v1alpha1",
			Kind:       "MCPGatewayExtension",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: kuadrantapi.MCPGatewayExtensionSpec{
			TargetRef: kuadrantapi.TargetReference{
				Group:     "gateway.networking.k8s.io",
				Kind:      "Gateway",
				Name:      gatewayName,
				Namespace: gatewayNamespace,
			},
		},
	}
	for _, opt := range opts {
		opt(ext)
	}

	if err := cfg.Client().Resources().Create(ctx, ext); err != nil {
		t.Fatalf("failed to create MCPGatewayExtension %s/%s: %v", namespace, name, err)
	}
	t.Logf("created MCPGatewayExtension %s/%s (gateway=%s/%s)", namespace, name, gatewayNamespace, gatewayName)

	t.Cleanup(func() {
		cleanupExt := &kuadrantapi.MCPGatewayExtension{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		}
		r := cfg.Client().Resources()
		if err := r.Get(context.Background(), name, namespace, cleanupExt); err != nil {
			return
		}
		if len(cleanupExt.Finalizers) > 0 {
			cleanupExt.Finalizers = nil
			_ = r.Update(context.Background(), cleanupExt)
		}
		_ = r.Delete(context.Background(), cleanupExt)
	})

	return ext
}

// ListenerSpec describes a Gateway listener for EnsureMultiListenerGateway.
type ListenerSpec struct {
	Name     string
	Port     int32
	Protocol gatewayv1.ProtocolType
	Hostname *gatewayv1.Hostname
}

// EnsureMultiListenerGateway creates a Gateway with multiple listeners if it
// doesn't already exist. Returns the gateway's LoadBalancer address.
func EnsureMultiListenerGateway(ctx context.Context, t *testing.T, cfg *envconf.Config,
	name, namespace, gatewayClassName string, listeners []ListenerSpec) string {
	t.Helper()
	r := cfg.Client().Resources()

	fromAll := gatewayv1.NamespacesFromAll
	var gwListeners []gatewayv1.Listener
	for _, l := range listeners {
		listener := gatewayv1.Listener{
			Name:     gatewayv1.SectionName(l.Name),
			Protocol: l.Protocol,
			Port:     gatewayv1.PortNumber(l.Port),
			AllowedRoutes: &gatewayv1.AllowedRoutes{
				Namespaces: &gatewayv1.RouteNamespaces{
					From: &fromAll,
				},
			},
		}
		if l.Hostname != nil {
			listener.Hostname = l.Hostname
		}
		gwListeners = append(gwListeners, listener)
	}

	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gatewayClassName),
			Listeners:        gwListeners,
		},
	}
	if err := r.Create(ctx, gw); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("failed to create Gateway %s/%s: %v", namespace, name, err)
	}

	existing := &gatewayv1.Gateway{}
	deadline := time.Now().Add(120 * time.Second)
	var gatewayAddress string
	for {
		if err := r.Get(ctx, name, namespace, existing); err != nil {
			t.Fatalf("failed to read Gateway %s/%s: %v", namespace, name, err)
		}
		if len(existing.Status.Addresses) > 0 {
			gatewayAddress = existing.Status.Addresses[0].Value
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for Gateway %s/%s to receive a LoadBalancer address", namespace, name)
		}
		time.Sleep(2 * time.Second)
	}

	t.Logf("ensured multi-listener Gateway %s/%s (class=%s, listeners=%d, address=%s)",
		namespace, name, gatewayClassName, len(listeners), gatewayAddress)
	return gatewayAddress
}

// AssertMCPReachable performs a full MCP handshake through the gateway,
// proving the server is reachable via the data plane. It connects to the
// gateway's LoadBalancer address with the route hostname as the Host header.
func AssertMCPReachable(ctx context.Context, t *testing.T, gatewayAddress, routeHostname, path string) {
	t.Helper()

	httpClient := WithHostOverride(&http.Client{}, routeHostname)

	mcpClient := mcp.NewClient(
		&mcp.Implementation{
			Name:    "e2e-hostname-test-client",
			Version: "v0.0.1",
		},
		nil,
	)

	transport := &mcp.StreamableClientTransport{
		Endpoint:   fmt.Sprintf("http://%s%s", gatewayAddress, path),
		HTTPClient: httpClient,
	}

	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	session, err := mcpClient.Connect(connectCtx, transport, nil)
	if err != nil {
		t.Fatalf("MCP handshake through gateway failed (address=%s, host=%s, path=%s): %v",
			gatewayAddress, routeHostname, path, err)
	}
	defer session.Close()

	initResult := session.InitializeResult()
	if initResult == nil {
		t.Fatal("InitializeResult is nil")
	}
	t.Logf("MCP handshake succeeded: server=%s version=%s (host=%s)",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version, routeHostname)
}

// EnsureReferenceGrant creates a ReferenceGrant in gatewayNamespace that allows
// MCPGatewayExtensions (and HTTPRoutes) from fromNamespace to reference Gateway
// resources. The mcp-gateway controller requires this for cross-namespace
// extension references. A t.Cleanup is registered to delete the grant.
func EnsureReferenceGrant(ctx context.Context, t *testing.T, cfg *envconf.Config,
	fromNamespace, gatewayNamespace string) {
	t.Helper()
	r := cfg.Client().Resources()

	name := "allow-" + fromNamespace
	grant := &gatewayv1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: gatewayNamespace,
		},
		Spec: gatewayv1.ReferenceGrantSpec{
			From: []gatewayv1.ReferenceGrantFrom{
				{
					Group:     "gateway.networking.k8s.io",
					Kind:      "HTTPRoute",
					Namespace: gatewayv1.Namespace(fromNamespace),
				},
				{
					Group:     "mcp.kuadrant.io",
					Kind:      "MCPGatewayExtension",
					Namespace: gatewayv1.Namespace(fromNamespace),
				},
			},
			To: []gatewayv1.ReferenceGrantTo{
				{
					Group: "",
					Kind:  "Service",
				},
				{
					Group: "gateway.networking.k8s.io",
					Kind:  "Gateway",
				},
			},
		},
	}
	if err := r.Create(ctx, grant); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("failed to create ReferenceGrant %s/%s: %v", gatewayNamespace, name, err)
	}
	t.Logf("created ReferenceGrant %s/%s (from=%s)", gatewayNamespace, name, fromNamespace)

	t.Cleanup(func() {
		_ = r.Delete(context.Background(), grant)
	})
}

func init() {
	for name, p := range providers {
		if p.Name != name {
			panic(fmt.Sprintf("provider config key %q does not match Name %q", name, p.Name))
		}
	}
}
