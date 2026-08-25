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

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	uzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestSetupLogLevelFromConfigMap_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping envtest integration test in short mode")
	}

	testEnv := &envtest.Environment{}
	if dir := envTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("failed to stop envtest: %v", err)
		}
	})

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 kubescheme.Scheme,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	atomicLevel := uzap.NewAtomicLevelAt(uzap.InfoLevel)
	const (
		namespace = "default"
		name      = "mcp-lifecycle-operator-config"
		key       = "log-level"
	)

	if err := setupLogLevelFromConfigMap(mgr, atomicLevel, namespace, name, key); err != nil {
		t.Fatalf("setupLogLevelFromConfigMap() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Errorf("manager start error: %v", err)
		}
	}()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatal("failed to sync manager cache")
	}

	client := mgr.GetClient()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			key: "debug",
		},
	}
	if err := client.Create(ctx, cm); err != nil {
		t.Fatalf("failed to create ConfigMap: %v", err)
	}

	waitForLogLevel(t, atomicLevel, uzap.DebugLevel)

	cm.Data[key] = "error"
	if err := client.Update(ctx, cm); err != nil {
		t.Fatalf("failed to update ConfigMap: %v", err)
	}

	waitForLogLevel(t, atomicLevel, uzap.ErrorLevel)
}

func waitForLogLevel(t *testing.T, level uzap.AtomicLevel, want zapcore.Level) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if level.Level() == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for log level %v, got %v", want, level.Level())
}

func envTestBinaryDir() string {
	entries, err := filepath.Glob(filepath.Join("..", "bin", "k8s", "*"))
	if err != nil || len(entries) == 0 {
		return ""
	}
	return entries[0]
}
