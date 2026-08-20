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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scenario"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

var testCAPEM = []byte(`-----BEGIN CERTIFICATE-----
MIIBgzCCASmgAwIBAgIUXeHgADE6XFO/hNfBfLGyt/Cw880wCgYIKoZIzj0EAwIw
FjEUMBIGA1UEAwwLZTJlLXRlc3QtY2EwIBcNMjYwODIwMTE1MzAzWhgPMjEyNjA3
MjcxMTUzMDNaMBYxFDASBgNVBAMMC2UyZS10ZXN0LWNhMFkwEwYHKoZIzj0CAQYI
KoZIzj0DAQcDQgAEJsnZ8e9zLYa1egP6tJD0c/JmUPVjwW0T6lJU2frN7mdgrn8o
cgxOZzaiTQRJ2uq0k9C3aPI5BO+LHfsjWS//D6NTMFEwHQYDVR0OBBYEFKE196tU
ZJCrHMN5C3SDH8a/NoGQMB8GA1UdIwQYMBaAFKE196tUZJCrHMN5C3SDH8a/NoGQ
MA8GA1UdEwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDSAAwRQIhAJDaTjLMUzD7RyFz
DTp3kKM7fYOneB7wdmUEuLjKSbzoAiAcfvPPfSLAc3EJ/khzG88fElJco2gGiYlt
Um8SPRQMrQ==
-----END CERTIFICATE-----
`)

var testPrivKeyPEM = []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIKuqL95aBIB8Nfj1xV93YUtmqoMLr6NFDJJhJnQV0abRoAoGCCqGSM49
AwEHoUQDQgAEJsnZ8e9zLYa1egP6tJD0c/JmUPVjwW0T6lJU2frN7mdgrn8ocgxO
ZzaiTQRJ2uq0k9C3aPI5BO+LHfsjWS//Dw==
-----END EC PRIVATE KEY-----
`)

func TestTLSMissingCABundleSecret(t *testing.T) {
	t.Parallel()
	feature := features.New("TLS validation rejects missing CA bundle Secret").
		WithLabel(category.Label, category.Resilience).
		WithLabel(speed.Label, speed.Fast).
		WithLabel(scenario.Label, scenario.Failure).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "tls-missing-ca", false,
				f.WithTransport(&mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{
						Enabled:        true,
						CABundleSecret: &mcpv1alpha1.SecretReference{Name: "nonexistent-ca-secret"},
					},
				}),
			)
		}).
		Assess("Accepted=False with reason Invalid", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid",
				2*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			accepted := f.GetMCPServerCondition(server, "Accepted")
			t.Logf("Accepted condition: status=%s reason=%s message=%q",
				accepted.Status, accepted.Reason, accepted.Message)

			if accepted.Message == "" {
				t.Error("expected Accepted message to mention the missing Secret")
			}

			return ctx
		}).
		Assess("Ready=False with reason ConfigurationInvalid", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Ready", metav1.ConditionFalse, "ConfigurationInvalid",
				30*time.Second)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestTLSInvalidPEMInCABundle(t *testing.T) {
	t.Parallel()
	feature := features.New("TLS validation rejects invalid PEM in CA bundle").
		WithLabel(category.Label, category.Resilience).
		WithLabel(speed.Label, speed.Fast).
		WithLabel(scenario.Label, scenario.Failure).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context")
			}
			r := cfg.Client().Resources()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bad-pem-ca",
					Namespace: ns,
				},
				Data: map[string][]byte{
					"ca.crt": []byte("not-a-valid-pem-certificate"),
				},
			}
			if err := r.Create(ctx, secret); err != nil {
				t.Fatalf("failed to create Secret: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "tls-bad-pem", false,
				f.WithTransport(&mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{
						Enabled:        true,
						CABundleSecret: &mcpv1alpha1.SecretReference{Name: "bad-pem-ca"},
					},
				}),
			)
		}).
		Assess("Accepted=False with reason Invalid", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid",
				2*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			accepted := f.GetMCPServerCondition(server, "Accepted")
			t.Logf("Accepted condition: status=%s reason=%s message=%q",
				accepted.Status, accepted.Reason, accepted.Message)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestTLSMissingCACrtKey(t *testing.T) {
	t.Parallel()
	feature := features.New("TLS validation rejects Secret missing ca.crt key").
		WithLabel(category.Label, category.Resilience).
		WithLabel(speed.Label, speed.Fast).
		WithLabel(scenario.Label, scenario.Failure).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context")
			}
			r := cfg.Client().Resources()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wrong-key-ca",
					Namespace: ns,
				},
				Data: map[string][]byte{
					"tls.crt": testCAPEM,
				},
			}
			if err := r.Create(ctx, secret); err != nil {
				t.Fatalf("failed to create Secret: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "tls-wrong-key", false,
				f.WithTransport(&mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{
						Enabled:        true,
						CABundleSecret: &mcpv1alpha1.SecretReference{Name: "wrong-key-ca"},
					},
				}),
			)
		}).
		Assess("Accepted=False with message mentioning ca.crt", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionMessageContains(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid", "ca.crt",
				2*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			accepted := f.GetMCPServerCondition(server, "Accepted")
			t.Logf("Accepted condition: status=%s reason=%s message=%q",
				accepted.Status, accepted.Reason, accepted.Message)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestTLSInsecureSkipVerifyWithCABundleConflict(t *testing.T) {
	t.Parallel()
	feature := features.New("TLS validation rejects insecureSkipVerify with caBundleSecret").
		WithLabel(category.Label, category.Resilience).
		WithLabel(speed.Label, speed.Fast).
		WithLabel(scenario.Label, scenario.Failure).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context")
			}
			r := cfg.Client().Resources()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "conflict-ca",
					Namespace: ns,
				},
				Data: map[string][]byte{
					"ca.crt": testCAPEM,
				},
			}
			if err := r.Create(ctx, secret); err != nil {
				t.Fatalf("failed to create Secret: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "tls-conflict", false,
				f.WithTransport(&mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{
						Enabled:            true,
						InsecureSkipVerify: true,
						CABundleSecret:     &mcpv1alpha1.SecretReference{Name: "conflict-ca"},
					},
				}),
			)
		}).
		Assess("Accepted=False with message about mutual exclusivity", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionMessageContains(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid", "mutually exclusive",
				2*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			accepted := f.GetMCPServerCondition(server, "Accepted")
			t.Logf("Accepted condition: status=%s reason=%s message=%q",
				accepted.Status, accepted.Reason, accepted.Message)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestTLSRecoveryFromMissingCASecret(t *testing.T) {
	t.Parallel()
	feature := features.New("TLS recovery after creating missing CA bundle Secret").
		WithLabel(category.Label, category.Resilience).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scenario.Label, scenario.Recovery).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "tls-recovery", false,
				f.WithTransport(&mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{
						Enabled:        true,
						CABundleSecret: &mcpv1alpha1.SecretReference{Name: "recovery-ca"},
					},
				}),
			)
		}).
		Assess("initially Accepted=False", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionReason(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid",
				2*time.Minute)
			t.Log("confirmed Accepted=False due to missing CA Secret")

			return ctx
		}).
		Assess("create CA Secret and recover to Accepted=True", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			ns := server.Namespace
			r := cfg.Client().Resources()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recovery-ca",
					Namespace: ns,
				},
				Data: map[string][]byte{
					"ca.crt": testCAPEM,
				},
			}
			if err := r.Create(ctx, secret); err != nil {
				t.Fatalf("failed to create CA Secret: %v", err)
			}
			t.Log("created recovery-ca Secret")

			f.WaitForMCPServerCondition(ctx, t, r, server,
				"Accepted", metav1.ConditionTrue, 2*time.Minute)
			t.Log("Accepted=True after CA Secret creation")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestTLSNonCertificatePEM(t *testing.T) {
	t.Parallel()
	feature := features.New("TLS validation rejects non-certificate PEM block").
		WithLabel(category.Label, category.Resilience).
		WithLabel(speed.Label, speed.Fast).
		WithLabel(scenario.Label, scenario.Failure).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns, ok := ctx.Value(f.NsKey).(string)
			if !ok || ns == "" {
				t.Fatal("namespace not found in context")
			}
			r := cfg.Client().Resources()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "privkey-as-ca",
					Namespace: ns,
				},
				Data: map[string][]byte{
					"ca.crt": testPrivKeyPEM,
				},
			}
			if err := r.Create(ctx, secret); err != nil {
				t.Fatalf("failed to create Secret: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "tls-privkey", false,
				f.WithTransport(&mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{
						Enabled:        true,
						CABundleSecret: &mcpv1alpha1.SecretReference{Name: "privkey-as-ca"},
					},
				}),
			)
		}).
		Assess("Accepted=False with message about no valid PEM certificates", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionMessageContains(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid", "no valid PEM",
				2*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			accepted := f.GetMCPServerCondition(server, "Accepted")
			t.Logf("Accepted condition: status=%s reason=%s message=%q",
				accepted.Status, accepted.Reason, accepted.Message)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestTLSDisabledIgnoresStaleCARef(t *testing.T) {
	t.Parallel()
	feature := features.New("TLS disabled skips CA bundle validation").
		WithLabel(category.Label, category.Configuration).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scenario.Label, scenario.Deploy).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "tls-disabled", true,
				f.WithTransport(&mcpv1alpha1.TransportConfig{
					TLS: &mcpv1alpha1.TLSClientConfig{
						Enabled:        false,
						CABundleSecret: &mcpv1alpha1.SecretReference{Name: "nonexistent-ignored"},
					},
				}),
			)
		}).
		Assess("Accepted=True despite nonexistent CA Secret", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerCondition(ctx, t, r, server,
				"Accepted", metav1.ConditionTrue, 2*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			accepted := f.GetMCPServerCondition(server, "Accepted")
			t.Logf("Accepted=True (TLS disabled, stale CA ref ignored, message=%q)", accepted.Message)

			return ctx
		}).
		Assess("Ready=True with HTTP handshake", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerCondition(ctx, t, r, server,
				"Ready", metav1.ConditionTrue, 2*time.Minute)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertAddressURL(t, server, server.Spec.Config.Port)
			t.Logf("MCPServer ready at %s", server.Status.Address.URL)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
