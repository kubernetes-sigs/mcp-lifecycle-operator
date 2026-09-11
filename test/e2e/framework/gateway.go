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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// GatewayServiceLocator describes how to find the gateway data plane service.
type GatewayServiceLocator struct {
	Namespace     string
	LabelSelector map[string]string
}

// ProviderConfig describes a gateway provider for conformance testing.
type ProviderConfig struct {
	Name           string
	ConfigData     map[string]string
	GatewayService GatewayServiceLocator
}

var providers = map[string]ProviderConfig{
	"httproute": {
		Name: "httproute",
		ConfigData: map[string]string{
			"gateway-name":      "e2e-gateway",
			"gateway-namespace": "gateway-system",
			"gateway-class":     "eg",
			"hostname":          "mcp.e2e.test",
		},
		GatewayService: GatewayServiceLocator{
			Namespace: "envoy-gateway-system",
			LabelSelector: map[string]string{
				"gateway.envoyproxy.io/owning-gateway-name":      "e2e-gateway",
				"gateway.envoyproxy.io/owning-gateway-namespace": "gateway-system",
			},
		},
	},
	"kuadrant": {
		Name: "kuadrant",
		ConfigData: map[string]string{
			"gateway-name":      "mcp-gateway",
			"gateway-namespace": "gateway-system",
			"gateway-class":     "istio",
			"hostname":          "mcp.127-0-0-1.sslip.io",
			"prefix":            "e2e_",
		},
		GatewayService: GatewayServiceLocator{
			Namespace: "gateway-system",
			LabelSelector: map[string]string{
				"gateway.networking.k8s.io/gateway-name": "mcp-gateway",
			},
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

// GatewayProxyHTTPClient discovers the gateway data plane service by label
// selector and returns an *http.Client plus the full API-server proxy URL for
// reaching it on port 80. The caller should set the Host header on requests to
// route through the gateway's data plane.
func GatewayProxyHTTPClient(t *testing.T, cfg *envconf.Config,
	locator GatewayServiceLocator, path string) (*http.Client, string) {
	t.Helper()

	svc := resolveGatewayService(t, cfg, locator)
	return ServiceProxyHTTPClient(t, cfg, svc.Namespace, svc.Name, 80, path)
}

func resolveGatewayService(t *testing.T, cfg *envconf.Config, loc GatewayServiceLocator) corev1.Service {
	t.Helper()
	r := cfg.Client().Resources().WithNamespace(loc.Namespace)
	sel := labels.SelectorFromSet(loc.LabelSelector).String()

	var found corev1.Service
	deadline := time.Now().Add(60 * time.Second)
	for {
		var list corev1.ServiceList
		if err := r.List(context.Background(), &list,
			resources.WithLabelSelector(sel),
		); err != nil {
			t.Fatalf("failed to list gateway services: %v", err)
		}
		if len(list.Items) == 1 {
			found = list.Items[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for gateway data plane service in %s matching %v (found %d)",
				loc.Namespace, loc.LabelSelector, len(list.Items))
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("resolved gateway service: %s/%s", found.Namespace, found.Name)
	return found
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
// when routing through the Kubernetes API server proxy to a gateway that
// performs host-based routing.
func WithHostOverride(c *http.Client, host string) *http.Client {
	return &http.Client{
		Transport: &hostOverrideTransport{base: c.Transport, host: host},
	}
}

func init() {
	for name, p := range providers {
		if p.Name != name {
			panic(fmt.Sprintf("provider config key %q does not match Name %q", name, p.Name))
		}
	}
}
