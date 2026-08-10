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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

func generateSelfSignedCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func TestBuildTLSTransport_InsecureSkipVerify(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	transport, err := buildTLSTransport(context.Background(), c, "default", &mcpv1alpha1.TLSClientConfig{
		Enabled:            true,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport == nil || transport.TLSClientConfig == nil {
		t.Fatal("expected non-nil transport with TLS config")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestBuildTLSTransport_WithCABundle(t *testing.T) {
	caPEM := generateSelfSignedCAPEM(t)

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ca", Namespace: "test-ns"},
		Data:       map[string][]byte{"ca.crt": caPEM},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
		Enabled:        true,
		CABundleSecret: &mcpv1alpha1.SecretReference{Name: "my-ca"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport == nil || transport.TLSClientConfig == nil {
		t.Fatal("expected non-nil transport with TLS config")
	}
	if transport.TLSClientConfig.RootCAs == nil {
		t.Error("expected RootCAs pool to be populated")
	}
}

func TestBuildTLSTransport_MissingSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	_, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
		Enabled:        true,
		CABundleSecret: &mcpv1alpha1.SecretReference{Name: "nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error for missing Secret")
	}
}

func TestBuildTLSTransport_MissingCACrtKey(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-secret", Namespace: "test-ns"},
		Data:       map[string][]byte{"wrong-key": []byte("data")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	_, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
		Enabled:        true,
		CABundleSecret: &mcpv1alpha1.SecretReference{Name: "bad-secret"},
	})
	if err == nil {
		t.Fatal("expected error for missing ca.crt key")
	}
}

func TestBuildTLSTransport_InvalidPEM(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-pem", Namespace: "test-ns"},
		Data:       map[string][]byte{"ca.crt": []byte("not-a-valid-pem")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	_, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
		Enabled:        true,
		CABundleSecret: &mcpv1alpha1.SecretReference{Name: "bad-pem"},
	})
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestBuildTLSTransport_SystemCAs(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestBuildTLSTransport_NilConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	transport, err := buildTLSTransport(context.Background(), c, "test-ns", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport != nil {
		t.Error("expected nil transport for nil config")
	}
}

func TestBuildTLSTransport_EnabledFalse(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
		Enabled:            false,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport != nil {
		t.Error("expected nil transport when Enabled is false")
	}
}
