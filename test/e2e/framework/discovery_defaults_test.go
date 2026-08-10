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

package framework

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

func setupEnvtest(t *testing.T) *envconf.Config {
	t.Helper()
	te := &envtest.Environment{}
	restCfg, err := te.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := te.Stop(); err != nil {
			t.Logf("failed to stop envtest: %v", err)
		}
	})
	cl, err := klient.New(restCfg)
	if err != nil {
		t.Fatalf("failed to create klient: %v", err)
	}
	return envconf.New().WithClient(cl)
}

// --- FromEnvVars ---

func TestFromEnvVars_NoVars_ReturnsZero(t *testing.T) {
	t.Setenv("MCPLO_NAMESPACE", "")
	t.Setenv("MCPLO_SA_NAME", "")
	t.Setenv("MCPLO_METRICS_SERVICE", "")

	ref, err := FromEnvVars(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != (OperatorRef{}) {
		t.Errorf("ref = %+v, want zero", ref)
	}
}

func TestFromEnvVars_NamespaceOnly_ReturnsPartialRef(t *testing.T) {
	t.Setenv("MCPLO_NAMESPACE", "my-ns")
	t.Setenv("MCPLO_SA_NAME", "")
	t.Setenv("MCPLO_METRICS_SERVICE", "")

	ref, err := FromEnvVars(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Namespace != "my-ns" {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, "my-ns")
	}
	if ref.ServiceAccountName != "" {
		t.Errorf("ServiceAccountName = %q, want empty", ref.ServiceAccountName)
	}
	if ref.MetricsServiceName != "" {
		t.Errorf("MetricsServiceName = %q, want empty", ref.MetricsServiceName)
	}
}

func TestFromEnvVars_AllEnvVars_ReturnsFullRef(t *testing.T) {
	t.Setenv("MCPLO_NAMESPACE", "my-ns")
	t.Setenv("MCPLO_SA_NAME", "my-sa")
	t.Setenv("MCPLO_METRICS_SERVICE", "my-svc")

	ref, err := FromEnvVars(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := OperatorRef{
		Namespace:          "my-ns",
		ServiceAccountName: "my-sa",
		MetricsServiceName: "my-svc",
	}
	if ref != want {
		t.Errorf("ref = %+v, want %+v", ref, want)
	}
}

// --- FromPodLabels ---

func TestFromPodLabels_NilConfig_ReturnsZero(t *testing.T) {
	ref, err := FromPodLabels(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != (OperatorRef{}) {
		t.Errorf("ref = %+v, want zero", ref)
	}
}

func TestFromPodLabels_NoMatchingPod_ReturnsZero(t *testing.T) {
	cfg := setupEnvtest(t)

	ref, err := FromPodLabels(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != (OperatorRef{}) {
		t.Errorf("ref = %+v, want zero", ref)
	}
}

func TestFromPodLabels_RunningPod_ReturnsNamespaceAndSA(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()
	r := cfg.Client().Resources()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns-sa"}}
	if err := r.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-lifecycle-controller-manager-abc",
			Namespace: "test-ns-sa",
			Labels: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "mcp-lifecycle-operator",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "my-custom-sa",
			Containers:         []corev1.Container{{Name: "manager", Image: "test:latest"}},
		},
	}
	if err := r.Create(ctx, pod); err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}
	pod.Status.Phase = corev1.PodRunning
	cs := Clientset(t, cfg)
	if _, err := cs.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update pod status: %v", err)
	}

	ref, err := FromPodLabels(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Namespace != "test-ns-sa" {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, "test-ns-sa")
	}
	if ref.ServiceAccountName != "my-custom-sa" {
		t.Errorf("ServiceAccountName = %q, want %q", ref.ServiceAccountName, "my-custom-sa")
	}
}

func TestFromPodLabels_RunningPod_WithMetricsService_ReturnsFullRef(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()
	r := cfg.Client().Resources()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns-svc"}}
	if err := r.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-lifecycle-controller-manager-xyz",
			Namespace: "test-ns-svc",
			Labels: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "mcp-lifecycle-operator",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "my-sa",
			Containers:         []corev1.Container{{Name: "manager", Image: "test:latest"}},
		},
	}
	if err := r.Create(ctx, pod); err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}
	pod.Status.Phase = corev1.PodRunning
	cs := Clientset(t, cfg)
	if _, err := cs.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update pod status: %v", err)
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-operator-metrics-service",
			Namespace: "test-ns-svc",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"control-plane": "controller-manager"},
			Ports:    []corev1.ServicePort{{Port: 8443}},
		},
	}
	if err := r.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	ref, err := FromPodLabels(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Namespace != "test-ns-svc" {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, "test-ns-svc")
	}
	if ref.ServiceAccountName != "my-sa" {
		t.Errorf("ServiceAccountName = %q, want %q", ref.ServiceAccountName, "my-sa")
	}
	if ref.MetricsServiceName != "my-operator-metrics-service" {
		t.Errorf("MetricsServiceName = %q, want %q", ref.MetricsServiceName, "my-operator-metrics-service")
	}
}

func TestFromPodLabels_RunningPod_NoMetricsService_ReturnsPartialRef(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()
	r := cfg.Client().Resources()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns-nosvc"}}
	if err := r.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-lifecycle-controller-manager-nosvc",
			Namespace: "test-ns-nosvc",
			Labels: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "mcp-lifecycle-operator",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "my-sa",
			Containers:         []corev1.Container{{Name: "manager", Image: "test:latest"}},
		},
	}
	if err := r.Create(ctx, pod); err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}
	pod.Status.Phase = corev1.PodRunning
	cs := Clientset(t, cfg)
	if _, err := cs.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update pod status: %v", err)
	}

	ref, err := FromPodLabels(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Namespace != "test-ns-nosvc" {
		t.Errorf("Namespace = %q, want %q", ref.Namespace, "test-ns-nosvc")
	}
	if ref.ServiceAccountName != "my-sa" {
		t.Errorf("ServiceAccountName = %q, want %q", ref.ServiceAccountName, "my-sa")
	}
	if ref.MetricsServiceName != "" {
		t.Errorf("MetricsServiceName = %q, want empty (no matching service)", ref.MetricsServiceName)
	}
}

func TestTierOrder_ExplicitMergesWithPlatform(t *testing.T) {
	t.Setenv("MCPLO_NAMESPACE", "custom-ns")
	t.Setenv("MCPLO_SA_NAME", "")
	t.Setenv("MCPLO_METRICS_SERVICE", "")

	platformLocate := func(_ context.Context, _ *envconf.Config) (OperatorRef, error) {
		return OperatorRef{
			Namespace:          "pod-ns",
			ServiceAccountName: "pod-sa",
			MetricsServiceName: "pod-svc",
		}, nil
	}

	r := &Registry{}
	r.Register(Explicit, FromEnvVars)
	r.Register(Platform, platformLocate)

	ref, err := r.DiscoverOperator(context.Background(), nil)
	if err != nil {
		t.Fatalf("DiscoverOperator() returned unexpected error: %v", err)
	}

	if ref.Namespace != "custom-ns" {
		t.Errorf("Namespace = %q, want %q (from Explicit env var)", ref.Namespace, "custom-ns")
	}
	if ref.ServiceAccountName != "pod-sa" {
		t.Errorf("ServiceAccountName = %q, want %q (from Platform)", ref.ServiceAccountName, "pod-sa")
	}
	if ref.MetricsServiceName != "pod-svc" {
		t.Errorf("MetricsServiceName = %q, want %q (from Platform)", ref.MetricsServiceName, "pod-svc")
	}
}

func TestFromEnvVars_SAOnly_ReturnsPartialRef(t *testing.T) {
	t.Setenv("MCPLO_NAMESPACE", "")
	t.Setenv("MCPLO_SA_NAME", "override-sa")
	t.Setenv("MCPLO_METRICS_SERVICE", "")

	ref, err := FromEnvVars(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.ServiceAccountName != "override-sa" {
		t.Errorf("ServiceAccountName = %q, want %q", ref.ServiceAccountName, "override-sa")
	}
	if ref.Namespace != "" {
		t.Errorf("Namespace = %q, want empty", ref.Namespace)
	}
}
