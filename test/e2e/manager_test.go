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
	"fmt"
	"strings"
	"testing"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

			// Create ClusterRoleBinding for metrics access.
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
				t.Fatalf("failed to create ClusterRoleBinding: %v", err)
			}
			t.Log("created ClusterRoleBinding for metrics access")

			return ctx
		}).
		Assess("controller pod is ready and serving metrics", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Find the controller pod.
			pod := f.FindPodByLabel(ctx, t, cfg, operatorNamespace, "control-plane=controller-manager")

			// Verify the metrics server has started by checking logs.
			logs := f.PodLogs(ctx, t, cfg, pod.Name, operatorNamespace)
			if !strings.Contains(logs, "Serving metrics server") {
				t.Fatal("controller logs do not contain 'Serving metrics server'")
			}
			t.Log("controller is serving metrics server")

			// Get SA token.
			cs := f.Clientset(t, cfg)
			tokenReq, err := cs.CoreV1().ServiceAccounts(operatorNamespace).CreateToken(
				ctx, serviceAccountName, &authv1.TokenRequest{}, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("failed to create SA token: %v", err)
			}
			token := tokenReq.Status.Token
			t.Log("obtained SA token")

			// Create a curl pod to access the metrics endpoint from inside the cluster.
			curlPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "curl-metrics",
					Namespace: operatorNamespace,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: serviceAccountName,
					Containers: []corev1.Container{{
						Name:    "curl",
						Image:   "curlimages/curl:latest",
						Command: []string{"/bin/sh", "-c"},
						Args: []string{
							fmt.Sprintf("curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics",
								token, metricsServiceName, operatorNamespace),
						},
						SecurityContext: &corev1.SecurityContext{
							ReadOnlyRootFilesystem:   ptrBool(true),
							AllowPrivilegeEscalation: ptrBool(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							RunAsNonRoot: ptrBool(true),
							RunAsUser:    ptrInt64(1000),
							SeccompProfile: &corev1.SeccompProfile{
								Type: corev1.SeccompProfileTypeRuntimeDefault,
							},
						},
					}},
				},
			}
			if err := cfg.Client().Resources().Create(ctx, curlPod); err != nil {
				t.Fatalf("failed to create curl-metrics pod: %v", err)
			}
			t.Log("created curl-metrics pod")

			// Wait for the curl pod to complete.
			f.WaitForPodPhase(ctx, t, cfg, curlPod, corev1.PodSucceeded)
			t.Log("curl-metrics pod succeeded")

			// Read curl pod logs and verify metrics response.
			curlLogs := f.PodLogs(ctx, t, cfg, curlPod.Name, operatorNamespace)
			if !strings.Contains(curlLogs, "HTTP/1.1 200 OK") {
				t.Fatalf("metrics response does not contain 200 OK:\n%s", curlLogs)
			}
			t.Log("metrics endpoint returned 200 OK")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()

			// Delete curl pod.
			curlPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "curl-metrics", Namespace: operatorNamespace},
			}
			_ = r.Delete(ctx, curlPod)

			// Delete ClusterRoleBinding.
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

func ptrBool(b bool) *bool    { return &b }
func ptrInt64(i int64) *int64 { return &i }
