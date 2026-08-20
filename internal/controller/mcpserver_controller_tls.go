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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func cloneDefaultTransport() *http.Transport {
	return http.DefaultTransport.(*http.Transport).Clone()
}

func buildTLSTransport(ctx context.Context, reader client.Reader, namespace string, tlsConfig *mcpv1alpha1.TLSClientConfig) (*http.Transport, error) {
	if tlsConfig == nil || !tlsConfig.Enabled {
		return nil, nil
	}

	transport := cloneDefaultTransport()

	if tlsConfig.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // user-requested via spec
		}
		return transport, nil
	}

	if tlsConfig.CABundleSecret == nil {
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		return transport, nil
	}

	secret := &corev1.Secret{}
	if err := reader.Get(ctx, client.ObjectKey{
		Name:      tlsConfig.CABundleSecret.Name,
		Namespace: namespace,
	}, secret); err != nil {
		return nil, fmt.Errorf("fetching CA bundle Secret %q: %w", tlsConfig.CABundleSecret.Name, err)
	}

	caPEM, ok := secret.Data[caBundleKey]
	if !ok {
		return nil, fmt.Errorf("secret %q does not contain key %q", tlsConfig.CABundleSecret.Name, caBundleKey)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("secret %q key %q contains no valid PEM certificates", tlsConfig.CABundleSecret.Name, caBundleKey)
	}

	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}
	return transport, nil
}

// updateTLSCABundleHash persists the CA bundle hash in-memory only after the
// status write succeeded and the handshake passed. A failed handshake preserves
// the previous hash so re-verification is forced on the next reconcile.
func (r *MCPServerReconciler) updateTLSCABundleHash(
	mcpServer *mcpv1alpha1.MCPServer,
	hash string,
	readyCondition metav1.Condition,
) {
	key := mcpServer.Namespace + "/" + mcpServer.Name
	if hash == "" {
		r.tlsCABundleHashes.Delete(key)
	} else if readyCondition.Status == metav1.ConditionTrue &&
		readyCondition.Reason == ReasonAvailable {
		r.tlsCABundleHashes.Store(key, hash)
	}
}

func computeTLSCABundleHash(ctx context.Context, reader client.Reader, namespace string, tlsConfig *mcpv1alpha1.TLSClientConfig) string {
	if tlsConfig == nil || !tlsConfig.Enabled || tlsConfig.InsecureSkipVerify || tlsConfig.CABundleSecret == nil {
		return ""
	}
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, client.ObjectKey{
		Name:      tlsConfig.CABundleSecret.Name,
		Namespace: namespace,
	}, secret); err != nil {
		return ""
	}
	caPEM, ok := secret.Data[caBundleKey]
	if !ok {
		return ""
	}
	h := sha256.Sum256(caPEM)
	return fmt.Sprintf("%x", h[:])
}
