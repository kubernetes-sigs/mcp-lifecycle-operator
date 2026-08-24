//go:build e2e

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

package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

func generateTLSCertificates(t *testing.T, serviceDNS string) (caCertPEM, serverCertPEM, serverKeyPEM []byte) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "e2e-tls-handshake-ca"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create CA certificate: %v", err)
	}
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("failed to parse CA certificate: %v", err)
	}
	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate server key: %v", err)
	}
	serverTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: serviceDNS},
		DNSNames:     []string{serviceDNS},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTmpl, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create server certificate: %v", err)
	}
	serverCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER})

	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		t.Fatalf("failed to marshal server key: %v", err)
	}
	serverKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER})

	return caCertPEM, serverCertPEM, serverKeyPEM
}

func TestTLSHandshake(t *testing.T) {
	t.Parallel()
	const (
		serverName    = "tls-handshake"
		caSecretName  = "tls-ca-bundle"
		tlsSecretName = "tls-server-cert"
		serverPort    = int32(8080)
	)

	feature := features.New("TLS handshake with kubernetes-mcp-server").
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Moderate).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context")
			}
			r := cfg.Client().Resources()

			serviceDNS := fmt.Sprintf("%s.%s.svc.cluster.local", serverName, ns)
			caCertPEM, serverCertPEM, serverKeyPEM := generateTLSCertificates(t, serviceDNS)

			tlsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tlsSecretName,
					Namespace: ns,
				},
				Data: map[string][]byte{
					"tls.crt": serverCertPEM,
					"tls.key": serverKeyPEM,
				},
			}
			if err := r.Create(ctx, tlsSecret); err != nil {
				t.Fatalf("failed to create TLS server Secret: %v", err)
			}
			t.Log("created server TLS Secret")

			caSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      caSecretName,
					Namespace: ns,
				},
				Data: map[string][]byte{
					"ca.crt": caCertPEM,
				},
			}
			if err := r.Create(ctx, caSecret); err != nil {
				t.Fatalf("failed to create CA bundle Secret: %v", err)
			}
			t.Log("created CA bundle Secret")

			return f.SetupMCPServer(ctx, t, cfg, serverName, false,
				f.WithPort(serverPort),
				f.WithArguments(
					"--port", fmt.Sprintf("%d", serverPort),
					"--read-only",
					"--tls-cert", "/certs/tls.crt",
					"--tls-key", "/certs/tls.key",
				),
				f.WithStorage(mcpv1alpha1.StorageMount{
					Path: "/certs",
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeSecret,
						Secret: &corev1.SecretVolumeSource{
							SecretName: tlsSecretName,
						},
					},
				}),
				f.WithTransport(&mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{
						Enabled:        true,
						CABundleSecret: &mcpv1alpha1.SecretReference{Name: caSecretName},
					},
				}),
			)
		}).
		Assess("Ready=True with successful TLS handshake", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerCondition(ctx, t, r, server,
				"Ready", metav1.ConditionTrue, 5*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			ready := f.GetMCPServerCondition(server, "Ready")
			t.Logf("Ready condition: status=%s reason=%s", ready.Status, ready.Reason)

			return ctx
		}).
		Assess("address URL uses HTTPS scheme", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			if server.Status.Address == nil || server.Status.Address.URL == "" {
				t.Fatal("expected status.address.url to be set")
			}
			if !strings.HasPrefix(server.Status.Address.URL, "https://") {
				t.Fatalf("expected HTTPS URL, got %q", server.Status.Address.URL)
			}
			t.Logf("MCPServer address: %s", server.Status.Address.URL)

			return ctx
		}).
		Assess("HTTPS request verifies cert chain via port-forward", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			caSecret := &corev1.Secret{}
			if err := r.Get(ctx, caSecretName, server.Namespace, caSecret); err != nil {
				t.Fatalf("failed to get CA Secret: %v", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caSecret.Data["ca.crt"]) {
				t.Fatal("failed to parse CA cert PEM into pool")
			}

			pod := f.FindPodByLabel(ctx, t, cfg, server.Namespace,
				fmt.Sprintf("mcp-server=%s", server.Name))

			restCfg := cfg.Client().Resources().GetConfig()
			transport, upgrader, err := spdy.RoundTripperFor(restCfg)
			if err != nil {
				t.Fatalf("failed to create SPDY round tripper: %v", err)
			}
			pfURL := fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s/portforward",
				restCfg.Host, server.Namespace, pod.Name)
			dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport},
				http.MethodPost, mustParseURL(t, pfURL))

			stopCh := make(chan struct{})
			readyCh := make(chan struct{})
			defer close(stopCh)

			pf, err := portforward.New(dialer,
				[]string{fmt.Sprintf("0:%d", serverPort)},
				stopCh, readyCh, nil, nil)
			if err != nil {
				t.Fatalf("failed to create port-forward: %v", err)
			}

			errCh := make(chan error, 1)
			go func() { errCh <- pf.ForwardPorts() }()

			select {
			case <-readyCh:
			case err := <-errCh:
				t.Fatalf("port-forward failed: %v", err)
			case <-time.After(30 * time.Second):
				t.Fatal("timed out waiting for port-forward")
			}

			ports, err := pf.GetPorts()
			if err != nil || len(ports) == 0 {
				t.Fatalf("failed to get forwarded ports: %v", err)
			}
			localPort := ports[0].Local

			serviceDNS := fmt.Sprintf("%s.%s.svc.cluster.local", server.Name, server.Namespace)
			httpClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs:    caCertPool,
						ServerName: serviceDNS,
					},
				},
				Timeout: 10 * time.Second,
			}

			targetURL := fmt.Sprintf("https://127.0.0.1:%d/mcp", localPort)
			req, err := http.NewRequestWithContext(ctx, http.MethodHead, targetURL, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				t.Fatalf("HTTPS request failed (cert chain not verified): %v", err)
			}
			defer resp.Body.Close()

			t.Logf("HTTPS HEAD %s returned %d (TLS cert chain verified via CA bundle)", targetURL, resp.StatusCode)

			return ctx
		}).
		Assess("serverInfo populated from TLS handshake", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			if server.Status.ServerInfo == nil {
				t.Fatal("expected serverInfo to be populated after TLS handshake")
			}
			t.Logf("serverInfo: name=%s version=%s protocol=%s",
				server.Status.ServerInfo.Name,
				server.Status.ServerInfo.Version,
				server.Status.ServerInfo.ProtocolVersion)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
