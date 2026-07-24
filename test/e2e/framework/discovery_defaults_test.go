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

func TestEnvVarLocator_Priority(t *testing.T) {
	d := &EnvVarLocator{}
	if got := d.Priority(); got != 5000 {
		t.Errorf("Priority() = %d, want 5000", got)
	}
}

func TestEnvVarLocator_Namespace(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		wantVal string
		wantOK  bool
	}{
		{
			name:    "returns value and true when MCPLO_NAMESPACE is set",
			envVal:  "my-namespace",
			wantVal: "my-namespace",
			wantOK:  true,
		},
		{
			name:    "returns empty string and false when MCPLO_NAMESPACE is unset",
			envVal:  "",
			wantVal: "",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envVal != "" {
				t.Setenv("MCPLO_NAMESPACE", tc.envVal)
			} else {
				t.Setenv("MCPLO_NAMESPACE", "")
			}

			d := &EnvVarLocator{}
			got, ok := d.Namespace(context.Background(), nil)
			if got != tc.wantVal {
				t.Errorf("Namespace() value = %q, want %q", got, tc.wantVal)
			}
			if ok != tc.wantOK {
				t.Errorf("Namespace() ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

func TestEnvVarLocator_ServiceAccount(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		wantVal string
		wantOK  bool
	}{
		{
			name:    "returns value and true when MCPLO_SA_NAME is set",
			envVal:  "my-service-account",
			wantVal: "my-service-account",
			wantOK:  true,
		},
		{
			name:    "returns empty string and false when MCPLO_SA_NAME is unset",
			envVal:  "",
			wantVal: "",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MCPLO_SA_NAME", tc.envVal)

			d := &EnvVarLocator{}
			got, ok := d.ServiceAccount(context.Background(), nil, "")
			if got != tc.wantVal {
				t.Errorf("ServiceAccount() value = %q, want %q", got, tc.wantVal)
			}
			if ok != tc.wantOK {
				t.Errorf("ServiceAccount() ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

func TestEnvVarLocator_MetricsService(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		wantVal string
		wantOK  bool
	}{
		{
			name:    "returns value and true when MCPLO_METRICS_SERVICE is set",
			envVal:  "my-metrics-service",
			wantVal: "my-metrics-service",
			wantOK:  true,
		},
		{
			name:    "returns empty string and false when MCPLO_METRICS_SERVICE is unset",
			envVal:  "",
			wantVal: "",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MCPLO_METRICS_SERVICE", tc.envVal)

			d := &EnvVarLocator{}
			got, ok := d.MetricsService(context.Background(), nil, "")
			if got != tc.wantVal {
				t.Errorf("MetricsService() value = %q, want %q", got, tc.wantVal)
			}
			if ok != tc.wantOK {
				t.Errorf("MetricsService() ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

func TestPodLabelLocator_Priority(t *testing.T) {
	d := &PodLabelLocator{}
	if got := d.Priority(); got != 50 {
		t.Errorf("Priority() = %d, want 50", got)
	}
}

func TestPodLabelLocator_Namespace(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()
	r := cfg.Client().Resources()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-operator-ns"}}
	if err := r.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	tests := []struct {
		name   string
		pod    *corev1.Pod
		wantNS string
		wantOK bool
	}{
		{
			name: "finds running pod with mcp-lifecycle in name and correct label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mcp-lifecycle-operator-controller-manager-abc",
					Namespace: "test-operator-ns",
					Labels: map[string]string{
						"control-plane":          "controller-manager",
						"app.kubernetes.io/name": "mcp-lifecycle-operator",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "manager", Image: "test:latest"}},
				},
			},
			wantNS: "test-operator-ns",
			wantOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := r.Create(ctx, tc.pod); err != nil {
				t.Fatalf("failed to create pod: %v", err)
			}
			t.Cleanup(func() { _ = r.Delete(ctx, tc.pod) })

			// envtest doesn't run kubelet, so pods stay Pending.
			// Force status to Running via the status subresource.
			tc.pod.Status.Phase = corev1.PodRunning
			cs := Clientset(t, cfg)
			if _, err := cs.CoreV1().Pods(tc.pod.Namespace).UpdateStatus(ctx, tc.pod, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("failed to update pod status: %v", err)
			}

			d := &PodLabelLocator{}
			got, ok := d.Namespace(ctx, cfg)
			if got != tc.wantNS {
				t.Errorf("Namespace() = %q, want %q", got, tc.wantNS)
			}
			if ok != tc.wantOK {
				t.Errorf("Namespace() ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

func TestPodLabelLocator_Namespace_NoMatch(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()

	d := &PodLabelLocator{}
	got, ok := d.Namespace(ctx, cfg)
	if got != "" {
		t.Errorf("Namespace() = %q, want empty", got)
	}
	if ok {
		t.Error("Namespace() ok = true, want false")
	}
}

func TestPodLabelLocator_ServiceAccount(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()
	r := cfg.Client().Resources()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-sa-ns"}}
	if err := r.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-lifecycle-controller-manager-xyz",
			Namespace: "test-sa-ns",
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

	d := &PodLabelLocator{}
	ns_, ok := d.Namespace(ctx, cfg)
	if !ok {
		t.Fatal("Namespace() ok = false, want true")
	}

	got, ok := d.ServiceAccount(ctx, cfg, ns_)
	if !ok {
		t.Fatal("ServiceAccount() ok = false, want true")
	}
	if got != "my-custom-sa" {
		t.Errorf("ServiceAccount() = %q, want %q", got, "my-custom-sa")
	}
}

func TestPodLabelLocator_ServiceAccount_NoMatch(t *testing.T) {
	d := &PodLabelLocator{}
	got, ok := d.ServiceAccount(context.Background(), nil, "any-ns")
	if ok {
		t.Errorf("ServiceAccount() ok = true, want false (got %q)", got)
	}
}

func TestPodLabelLocator_MetricsService(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()
	r := cfg.Client().Resources()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-svc-ns"}}
	if err := r.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-lifecycle-controller-manager-xyz",
			Namespace: "test-svc-ns",
			Labels: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "mcp-lifecycle-operator",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "manager", Image: "test:latest"}},
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
			Namespace: "test-svc-ns",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"control-plane": "controller-manager"},
			Ports:    []corev1.ServicePort{{Port: 8443}},
		},
	}
	if err := r.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	d := &PodLabelLocator{}
	ns_, ok := d.Namespace(ctx, cfg)
	if !ok {
		t.Fatal("Namespace() ok = false, want true")
	}

	got, ok := d.MetricsService(ctx, cfg, ns_)
	if !ok {
		t.Fatal("MetricsService() ok = false, want true")
	}
	if got != "my-operator-metrics-service" {
		t.Errorf("MetricsService() = %q, want %q", got, "my-operator-metrics-service")
	}
}

func TestPodLabelLocator_MetricsService_NoMatch(t *testing.T) {
	d := &PodLabelLocator{}
	got, ok := d.MetricsService(context.Background(), nil, "any-ns")
	if ok {
		t.Errorf("MetricsService() ok = true, want false (got %q)", got)
	}
}

func TestPodLabelLocator_ServiceAccount_WithNamespaceOnly(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()
	r := cfg.Client().Resources()

	namespace := "test-lazy-sa-ns"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	if err := r.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-lifecycle-controller-manager-lazy",
			Namespace: namespace,
			Labels: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "mcp-lifecycle-operator",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "my-discovered-sa",
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

	d := &PodLabelLocator{}
	got, ok := d.ServiceAccount(ctx, cfg, namespace)
	if !ok {
		t.Fatal("ServiceAccount() ok = false, want true")
	}
	if got != "my-discovered-sa" {
		t.Errorf("ServiceAccount() = %q, want %q", got, "my-discovered-sa")
	}
}

func TestPodLabelLocator_MetricsService_WithNamespaceOnly(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()
	r := cfg.Client().Resources()

	namespace := "test-lazy-svc-ns"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	if err := r.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-lifecycle-controller-manager-lazy",
			Namespace: namespace,
			Labels: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "mcp-lifecycle-operator",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "manager", Image: "test:latest"}},
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
			Name:      "mcp-lifecycle-operator-metrics-service",
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "mcp-lifecycle-operator",
			},
			Ports: []corev1.ServicePort{{Port: 8443}},
		},
	}
	if err := r.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	d := &PodLabelLocator{}
	got, ok := d.MetricsService(ctx, cfg, namespace)
	if !ok {
		t.Fatal("MetricsService() ok = false, want true")
	}
	if got != "mcp-lifecycle-operator-metrics-service" {
		t.Errorf("MetricsService() = %q, want %q", got, "mcp-lifecycle-operator-metrics-service")
	}
}

func TestDiscover_EnvVarNamespace_PodLabelSAAndMetrics(t *testing.T) {
	cfg := setupEnvtest(t)
	ctx := context.Background()
	r := cfg.Client().Resources()

	namespace := "custom-operator-ns"
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	if err := r.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-lifecycle-controller-manager-xyz",
			Namespace: namespace,
			Labels: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "mcp-lifecycle-operator",
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "custom-sa",
			Containers:         []corev1.Container{{Name: "manager", Image: "test:latest"}},
		},
	}
	if err := r.Create(ctx, pod); err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}
	pod.Status.Phase = corev1.PodRunning
	cs := Clientset(t, cfg)
	if _, err := cs.CoreV1().Pods(namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update pod status: %v", err)
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-metrics-service",
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": "mcp-lifecycle-operator",
			},
			Ports: []corev1.ServicePort{{Port: 8443}},
		},
	}
	if err := r.Create(ctx, svc); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	reg := &Registry{}
	reg.RegisterLocator(&EnvVarLocator{})
	reg.RegisterLocator(&PodLabelLocator{})

	t.Setenv("MCPLO_NAMESPACE", namespace)
	t.Setenv("MCPLO_SA_NAME", "")
	t.Setenv("MCPLO_METRICS_SERVICE", "")

	gotNS := reg.DiscoverNamespace(ctx, cfg)
	if gotNS != namespace {
		t.Errorf("DiscoverNamespace() = %q, want %q", gotNS, namespace)
	}

	gotSA := reg.DiscoverServiceAccount(ctx, cfg, gotNS)
	if gotSA != "custom-sa" {
		t.Errorf("DiscoverServiceAccount() = %q, want %q", gotSA, "custom-sa")
	}

	gotSvc := reg.DiscoverMetricsService(ctx, cfg, gotNS)
	if gotSvc != "custom-metrics-service" {
		t.Errorf("DiscoverMetricsService() = %q, want %q", gotSvc, "custom-metrics-service")
	}
}
