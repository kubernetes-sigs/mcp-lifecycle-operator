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
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

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

type contextKey string

const curlPodNameKey contextKey = "curlPodName"

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
			// Find the controller pod.
			pod := f.FindPodByLabel(ctx, t, cfg, operatorNamespace, "control-plane=controller-manager")

			// Poll controller logs until the metrics server line appears.
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

			// Create a curl pod to access the metrics endpoint from inside the cluster.
			// The pod uses the auto-mounted SA token instead of embedding it in args.
			curlPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "curl-metrics-",
					Namespace:    operatorNamespace,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: serviceAccountName,
					Containers: []corev1.Container{{
						Name:    "curl",
						Image:   "curlimages/curl:8.20.0",
						Command: []string{"/bin/sh", "-c"},
						Args: []string{
							fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' -k -H \"Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)\" https://%s.%s.svc.cluster.local:8443/metrics",
								metricsServiceName, operatorNamespace),
						},
						SecurityContext: &corev1.SecurityContext{
							ReadOnlyRootFilesystem:   ptr.To(true),
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							RunAsNonRoot: ptr.To(true),
							RunAsUser:    ptr.To(int64(1000)),
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
			ctx = context.WithValue(ctx, curlPodNameKey, curlPod.Name)
			t.Logf("created curl-metrics pod %s", curlPod.Name)

			// Wait for the curl pod to complete.
			f.WaitForPodPhase(ctx, t, cfg, curlPod, corev1.PodSucceeded)
			t.Log("curl-metrics pod succeeded")

			// Read curl pod logs and verify HTTP status code.
			curlLogs := f.PodLogs(ctx, t, cfg, curlPod.Name, operatorNamespace)
			if !strings.Contains(curlLogs, "200") {
				t.Fatalf("expected HTTP status 200, got: %s", curlLogs)
			}
			t.Log("metrics endpoint returned 200")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			r := cfg.Client().Resources()

			// Delete curl pod.
			if name, ok := ctx.Value(curlPodNameKey).(string); ok {
				curlPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: operatorNamespace},
				}
				_ = r.Delete(ctx, curlPod)
			}

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
