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

	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
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
		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1beta1.TLSClientConfig{
			Enabled:            false,
			InsecureSkipVerify: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(transport).To(BeNil())
	})

	It("should set InsecureSkipVerify with TLS 1.2 minimum", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		transport, err := buildTLSTransport(context.Background(), c, "default", &mcpv1beta1.TLSClientConfig{
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
		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1beta1.TLSClientConfig{
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

		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1beta1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1beta1.SecretReference{Name: "my-ca"},
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

		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1beta1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1beta1.SecretReference{Name: "my-ca"},
		})
		Expect(err).NotTo(HaveOccurred())
		subjects := transport.TLSClientConfig.RootCAs.Subjects() //nolint:staticcheck // x509.CertPool.Subjects is the only way to inspect pool contents
		Expect(subjects).To(HaveLen(1), "pool should contain only the supplied CA, not system CAs")
	})

	It("should error when Secret is missing", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		_, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1beta1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1beta1.SecretReference{Name: "nonexistent"},
		})
		Expect(err).To(HaveOccurred())
	})

	It("should error when ca.crt key is missing from Secret", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-secret", Namespace: "test-ns"},
			Data:       map[string][]byte{"wrong-key": []byte("data")},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		_, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1beta1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1beta1.SecretReference{Name: "bad-secret"},
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
		_, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1beta1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1beta1.SecretReference{Name: "bad-pem"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no valid PEM"))
	})

	It("should clone http.DefaultTransport preserving proxy and timeout settings", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		transport, err := buildTLSTransport(context.Background(), c, "test-ns", &mcpv1beta1.TLSClientConfig{
			Enabled: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(transport.Proxy).NotTo(BeNil(), "cloned transport should preserve Proxy from DefaultTransport")
	})
})

var _ = Describe("urlScheme", func() {
	It("should return http when no transport config", func() {
		mcpServer := &mcpv1beta1.MCPServer{}
		Expect(urlScheme(mcpServer)).To(Equal("http"))
	})

	It("should return http when TLS is not enabled", func() {
		mcpServer := &mcpv1beta1.MCPServer{
			Spec: mcpv1beta1.MCPServerSpec{
				Transport: &mcpv1beta1.TransportConfig{
					TLS: &mcpv1beta1.TLSClientConfig{Enabled: false},
				},
			},
		}
		Expect(urlScheme(mcpServer)).To(Equal("http"))
	})

	It("should return https when TLS is enabled", func() {
		mcpServer := &mcpv1beta1.MCPServer{
			Spec: mcpv1beta1.MCPServerSpec{
				Transport: &mcpv1beta1.TransportConfig{
					TLS: &mcpv1beta1.TLSClientConfig{Enabled: true},
				},
			},
		}
		Expect(urlScheme(mcpServer)).To(Equal("https"))
	})
})

var _ = Describe("computeTLSCABundleHash", func() {
	ctx := context.Background()

	It("should return empty for nil TLS config", func() {
		cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
		Expect(computeTLSCABundleHash(ctx, cli, "default", nil)).To(BeEmpty())
	})

	It("should return empty when TLS is disabled", func() {
		cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
		cfg := &mcpv1beta1.TLSClientConfig{Enabled: false}
		Expect(computeTLSCABundleHash(ctx, cli, "default", cfg)).To(BeEmpty())
	})

	It("should return empty when insecureSkipVerify is set", func() {
		cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
		cfg := &mcpv1beta1.TLSClientConfig{
			Enabled:            true,
			InsecureSkipVerify: true,
			CABundleSecret:     &mcpv1beta1.SecretReference{Name: "ca"},
		}
		Expect(computeTLSCABundleHash(ctx, cli, "default", cfg)).To(BeEmpty())
	})

	It("should return empty when no caBundleSecret is set", func() {
		cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
		cfg := &mcpv1beta1.TLSClientConfig{Enabled: true}
		Expect(computeTLSCABundleHash(ctx, cli, "default", cfg)).To(BeEmpty())
	})

	It("should return empty when Secret does not exist", func() {
		sch := runtime.NewScheme()
		Expect(corev1.AddToScheme(sch)).To(Succeed())
		cli := fake.NewClientBuilder().WithScheme(sch).Build()
		cfg := &mcpv1beta1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1beta1.SecretReference{Name: "missing"},
		}
		Expect(computeTLSCABundleHash(ctx, cli, "default", cfg)).To(BeEmpty())
	})

	It("should return a stable hash for the same Secret content", func() {
		sch := runtime.NewScheme()
		Expect(corev1.AddToScheme(sch)).To(Succeed())
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "ca-bundle", Namespace: "default"},
			Data:       map[string][]byte{"ca.crt": []byte("test-ca-data")},
		}
		cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(secret).Build()
		cfg := &mcpv1beta1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1beta1.SecretReference{Name: "ca-bundle"},
		}
		hash1 := computeTLSCABundleHash(ctx, cli, "default", cfg)
		hash2 := computeTLSCABundleHash(ctx, cli, "default", cfg)
		Expect(hash1).NotTo(BeEmpty())
		Expect(hash1).To(Equal(hash2))
		Expect(hash1).To(HaveLen(64))
	})

	It("should return a different hash when Secret content changes", func() {
		sch := runtime.NewScheme()
		Expect(corev1.AddToScheme(sch)).To(Succeed())
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "ca-bundle", Namespace: "default"},
			Data:       map[string][]byte{"ca.crt": []byte("original-ca")},
		}
		cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(secret).Build()
		cfg := &mcpv1beta1.TLSClientConfig{
			Enabled:        true,
			CABundleSecret: &mcpv1beta1.SecretReference{Name: "ca-bundle"},
		}
		hash1 := computeTLSCABundleHash(ctx, cli, "default", cfg)

		secret.Data["ca.crt"] = []byte("rotated-ca")
		Expect(cli.Update(ctx, secret)).To(Succeed())

		hash2 := computeTLSCABundleHash(ctx, cli, "default", cfg)
		Expect(hash1).NotTo(Equal(hash2))
	})
})
