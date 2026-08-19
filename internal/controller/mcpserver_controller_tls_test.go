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
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

func generateSelfSignedCAPEM() ([]byte, *x509.Certificate) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	cert, err := x509.ParseCertificate(certDER)
	Expect(err).NotTo(HaveOccurred())
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return pemBytes, cert
}

var _ = Describe("buildTLSTransport", func() {
	var (
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
	})

	It("should return nil transport when config is nil", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		transport, err := buildTLSTransport(context.Background(), c, "test-ns", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(transport).To(BeNil())
	})

	It("should return nil transport when Enabled is false", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
			Enabled:            false,
			InsecureSkipVerify: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(transport).To(BeNil())
	})

	It("should set InsecureSkipVerify with TLS 1.2 minimum", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		transport, err := buildTLSTransport(context.Background(), c, "default", &mcpv1alpha1.TLSClientConfig{
			Enabled:            true,
			InsecureSkipVerify: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(transport).NotTo(BeNil())
		Expect(transport.TLSClientConfig).NotTo(BeNil())
		Expect(transport.TLSClientConfig.InsecureSkipVerify).To(BeTrue())
		Expect(transport.TLSClientConfig.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
	})

	It("should use system CAs when no CABundleSecret is set", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
			Enabled: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(transport).NotTo(BeNil())
		Expect(transport.TLSClientConfig.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
	})

	It("should load CA bundle and verify the cert is in the pool", func() {
		caPEM, expectedCert := generateSelfSignedCAPEM()
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "my-ca", Namespace: "test-ns"},
			Data:       map[string][]byte{"ca.crt": caPEM},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1alpha1.SecretReference{Name: "my-ca"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(transport).NotTo(BeNil())
		Expect(transport.TLSClientConfig).NotTo(BeNil())
		Expect(transport.TLSClientConfig.RootCAs).NotTo(BeNil())
		Expect(transport.TLSClientConfig.MinVersion).To(Equal(uint16(tls.VersionTLS12)))

		subjects := transport.TLSClientConfig.RootCAs.Subjects() //nolint:staticcheck // x509.CertPool.Subjects is the only way to inspect pool contents
		found := false
		for _, s := range subjects {
			if string(s) == string(expectedCert.RawSubject) {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "expected CA certificate to be present in the pool")
	})

	It("should not include system CAs when CA bundle is set", func() {
		caPEM, _ := generateSelfSignedCAPEM()
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "my-ca", Namespace: "test-ns"},
			Data:       map[string][]byte{"ca.crt": caPEM},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1alpha1.SecretReference{Name: "my-ca"},
		})
		Expect(err).NotTo(HaveOccurred())
		subjects := transport.TLSClientConfig.RootCAs.Subjects() //nolint:staticcheck // x509.CertPool.Subjects is the only way to inspect pool contents
		Expect(subjects).To(HaveLen(1), "pool should contain only the supplied CA, not system CAs")
	})

	It("should error when Secret is missing", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		_, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1alpha1.SecretReference{Name: "nonexistent"},
		})
		Expect(err).To(HaveOccurred())
	})

	It("should error when ca.crt key is missing from Secret", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-secret", Namespace: "test-ns"},
			Data:       map[string][]byte{"wrong-key": []byte("data")},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		_, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1alpha1.SecretReference{Name: "bad-secret"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ca.crt"))
	})

	It("should error when PEM data is invalid", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-pem", Namespace: "test-ns"},
			Data:       map[string][]byte{"ca.crt": []byte("not-a-valid-pem")},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		_, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1alpha1.SecretReference{Name: "bad-pem"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no valid PEM"))
	})

	It("should clone http.DefaultTransport preserving proxy and timeout settings", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1alpha1.TLSClientConfig{
			Enabled: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(transport.Proxy).NotTo(BeNil(), "cloned transport should preserve Proxy from DefaultTransport")
	})
})

var _ = Describe("urlScheme", func() {
	It("should return http when no transport config", func() {
		mcpServer := &mcpv1alpha1.MCPServer{}
		Expect(urlScheme(mcpServer)).To(Equal("http"))
	})

	It("should return http when TLS is not enabled", func() {
		mcpServer := &mcpv1alpha1.MCPServer{
			Spec: mcpv1alpha1.MCPServerSpec{
				Transport: &mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{Enabled: false},
				},
			},
		}
		Expect(urlScheme(mcpServer)).To(Equal("http"))
	})

	It("should return https when TLS is enabled", func() {
		mcpServer := &mcpv1alpha1.MCPServer{
			Spec: mcpv1alpha1.MCPServerSpec{
				Transport: &mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{Enabled: true},
				},
			},
		}
		Expect(urlScheme(mcpServer)).To(Equal("https"))
	})
})
