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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

const caBundleKey = "ca.crt"

func urlScheme(mcpServer *mcpv1alpha1.MCPServer) string {
	if mcpServer.Spec.Transport != nil &&
		mcpServer.Spec.Transport.TLS != nil &&
		mcpServer.Spec.Transport.TLS.Enabled {
		return "https"
	}
	return "http"
}

func buildTLSTransport(ctx context.Context, c client.Client, namespace string, tlsConfig *mcpv1alpha1.TLSClientConfig) (*http.Transport, error) {
	if tlsConfig == nil || !tlsConfig.Enabled {
		return nil, nil
	}

	if tlsConfig.InsecureSkipVerify {
		return &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // user-requested via spec
			},
		}, nil
	}

	if tlsConfig.CABundleSecret == nil {
		return &http.Transport{
			TLSClientConfig: &tls.Config{},
		}, nil
	}

	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{
		Name:      tlsConfig.CABundleSecret.Name,
		Namespace: namespace,
	}, secret); err != nil {
		return nil, fmt.Errorf("fetching CA bundle Secret %q: %w", tlsConfig.CABundleSecret.Name, err)
	}

	caPEM, ok := secret.Data[caBundleKey]
	if !ok {
		return nil, fmt.Errorf("secret %q does not contain key %q", tlsConfig.CABundleSecret.Name, caBundleKey)
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}

	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("secret %q key %q contains no valid PEM certificates", tlsConfig.CABundleSecret.Name, caBundleKey)
	}

	return &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: pool,
		},
	}, nil
}
