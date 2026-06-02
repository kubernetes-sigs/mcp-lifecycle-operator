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

package olm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

var (
	operatorSDKPath     string
	operatorSDKPathOnce sync.Once
)

func operatorSDKBinary() string {
	operatorSDKPathOnce.Do(func() {
		operatorSDKPath = os.Getenv("OPERATOR_SDK")
		if operatorSDKPath == "" {
			operatorSDKPath = "operator-sdk"
		}
	})
	return operatorSDKPath
}

func runOperatorSDK(args ...string) (string, error) {
	cmd := exec.Command(operatorSDKBinary(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

func installBundle(t *testing.T, namespace, installMode string) {
	t.Helper()
	bundleImg := os.Getenv("BUNDLE_IMG")
	if bundleImg == "" {
		t.Fatal("BUNDLE_IMG environment variable must be set")
	}
	args := []string{
		"run", "bundle",
		"--namespace", namespace,
		"--install-mode", installMode,
		"--use-http",
		"--timeout", "5m",
		bundleImg,
	}
	t.Logf("installing bundle: operator-sdk %v", args)
	out, err := runOperatorSDK(args...)
	if err != nil {
		t.Fatalf("failed to install bundle: %v", err)
	}
	t.Logf("bundle installed: %s", out)
}

func cleanupOperator(t *testing.T, namespace string) {
	t.Helper()
	t.Logf("cleaning up operator in namespace %s", namespace)
	out, err := runOperatorSDK("cleanup", "mcp-lifecycle-operator", "--namespace", namespace)
	if err != nil {
		t.Logf("warning: cleanup failed: %v", err)
		return
	}
	t.Logf("operator cleaned up: %s", out)
}


func createNamespace(ctx context.Context, t *testing.T, cfg *envconf.Config, name string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := cfg.Client().Resources().Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace %s: %v", name, err)
	}
	t.Logf("created namespace %s", name)
}

func deleteNamespace(ctx context.Context, t *testing.T, cfg *envconf.Config, name string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := cfg.Client().Resources().Delete(ctx, ns); err != nil {
		t.Logf("warning: failed to delete namespace %s: %v", name, err)
	}
}

// assertNotReconciled verifies that an MCPServer has no status conditions
// after the given duration, indicating the operator did not reconcile it.
func assertNotReconciled(ctx context.Context, t *testing.T, r *resources.Resources, server *mcpv1alpha1.MCPServer, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
			t.Fatalf("failed to get MCPServer: %v", err)
		}
		if len(server.Status.Conditions) > 0 {
			t.Fatalf("MCPServer %s/%s was unexpectedly reconciled: conditions=%v",
				server.Namespace, server.Name, server.Status.Conditions)
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("MCPServer %s/%s was not reconciled (as expected)", server.Namespace, server.Name)
}
