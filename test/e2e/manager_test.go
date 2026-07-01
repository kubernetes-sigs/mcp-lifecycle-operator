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
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

const (
	operatorNamespace  = "mcp-lifecycle-operator-system"
	serviceAccountName = "mcp-lifecycle-operator-controller-manager"
	metricsServiceName = "mcp-lifecycle-operator-controller-manager-metrics-service"
	metricsRoleBinding = "mcp-lifecycle-operator-metrics-binding"
)

func TestManagerPodRunning(t *testing.T) {
	feature := features.New("Manager pod is running").
		WithLabel("type", "manager").
		Assess("controller-manager pod is Running", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			pod := f.FindPodByLabel(ctx, t, cfg, operatorNamespace, "control-plane=controller-manager")
			t.Logf("controller-manager pod %s is Running", pod.Name)
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestMetricsEndpoint(t *testing.T) {
	feature := features.New("Metrics endpoint serves data").
		WithLabel("type", "manager").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()

			crb := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: metricsRoleBinding},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "mcp-lifecycle-operator-metrics-reader",
				},
				Subjects: []rbacv1.Subject{{
					Kind:      "ServiceAccount",
					Name:      serviceAccountName,
					Namespace: operatorNamespace,
				}},
			}
			if err := r.Create(ctx, crb); err != nil {
				if !apierrors.IsAlreadyExists(err) {
					t.Fatalf("failed to create ClusterRoleBinding: %v", err)
				}
				t.Log("ClusterRoleBinding for metrics access already exists")
			} else {
				t.Log("created ClusterRoleBinding for metrics access")
			}

			return ctx
		}).
		Assess("controller pod is ready and serving metrics", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			pod := f.FindPodByLabel(ctx, t, cfg, operatorNamespace, "control-plane=controller-manager")

			deadline := time.Now().Add(1 * time.Minute)
			for {
				logs := f.PodLogs(ctx, t, cfg, pod.Name, operatorNamespace)
				if strings.Contains(logs, "Serving metrics server") {
					t.Log("controller is serving metrics server")
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("timed out waiting for 'Serving metrics server' in controller logs")
				}
				time.Sleep(2 * time.Second)
			}

			f.WaitForEndpointsReady(ctx, t, cfg, operatorNamespace, metricsServiceName)

			// Create a short-lived token for the controller-manager SA.
			cs := f.Clientset(t, cfg)
			tr, err := cs.CoreV1().ServiceAccounts(operatorNamespace).
				CreateToken(ctx, serviceAccountName, &authv1.TokenRequest{
					Spec: authv1.TokenRequestSpec{
						ExpirationSeconds: func() *int64 { v := int64(600); return &v }(),
					},
				}, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("failed to create token for SA %s: %v", serviceAccountName, err)
			}

			// Port-forward to the controller-manager pod so we can send the
			// bearer token directly (the API server proxy strips auth headers).
			restCfg := cfg.Client().Resources().GetConfig()
			transport, upgrader, err := spdy.RoundTripperFor(restCfg)
			if err != nil {
				t.Fatalf("failed to create SPDY round tripper: %v", err)
			}
			pfURL := fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s/portforward",
				restCfg.Host, operatorNamespace, pod.Name)
			dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, mustParseURL(t, pfURL))

			stopCh := make(chan struct{})
			readyCh := make(chan struct{})
			defer close(stopCh)

			pf, err := portforward.New(dialer, []string{"0:8443"}, stopCh, readyCh, nil, nil)
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
				t.Fatal("timed out waiting for port-forward to be ready")
			}

			ports, err := pf.GetPorts()
			if err != nil || len(ports) == 0 {
				t.Fatalf("failed to get forwarded ports: %v", err)
			}
			localPort := ports[0].Local

			httpClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
				},
			}
			metricsURL := fmt.Sprintf("https://localhost:%d/metrics", localPort)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+tr.Status.Token)

			resp, err := httpClient.Do(req)
			if err != nil {
				t.Fatalf("failed to GET metrics via port-forward: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				t.Fatalf("expected HTTP status 200, got %d", resp.StatusCode)
			}
			t.Logf("metrics endpoint returned %d via port-forward", resp.StatusCode)

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()
			crb := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: metricsRoleBinding},
			}
			_ = r.Delete(ctx, crb)
			t.Log("cleaned up metrics test resources")
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", raw, err)
	}
	return u
}
