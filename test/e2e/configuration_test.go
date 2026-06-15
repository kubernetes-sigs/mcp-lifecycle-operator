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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

// --- Storage Tests ---

func TestStorageConfigMap(t *testing.T) {
	feature := features.New("MCPServer with ConfigMap storage").
		WithLabel("type", "configuration").
		WithLabel("config", "storage-configmap").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: ns},
				Data:       map[string]string{"config.json": "{}"},
			}
			if err := r.Create(ctx, cm); err != nil {
				t.Fatalf("failed to create ConfigMap: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "storage-cm", true,
				f.WithStorage(mcpv1alpha1.StorageMount{
					Path:        "/etc/mcp-config",
					Permissions: mcpv1alpha1.MountPermissionsReadOnly,
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "test-config"},
						},
					},
				}),
			)
		}).
		Assess("Deployment has ConfigMap volume and mount", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			foundVolume := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.ConfigMap != nil && v.ConfigMap.Name == "test-config" {
					foundVolume = true
					break
				}
			}
			if !foundVolume {
				t.Fatal("expected volume with ConfigMap source 'test-config'")
			}

			foundMount := false
			for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
				if m.MountPath == "/etc/mcp-config" && m.ReadOnly {
					foundMount = true
					break
				}
			}
			if !foundMount {
				t.Fatal("expected read-only volume mount at /etc/mcp-config")
			}

			t.Log("Deployment has correct ConfigMap volume and mount")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestStorageSecret(t *testing.T) {
	feature := features.New("MCPServer with Secret storage").
		WithLabel("type", "configuration").
		WithLabel("config", "storage-secret").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: ns},
				Data:       map[string][]byte{"token": []byte("test-value")},
			}
			if err := r.Create(ctx, secret); err != nil {
				t.Fatalf("failed to create Secret: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "storage-secret", true,
				f.WithStorage(mcpv1alpha1.StorageMount{
					Path:        "/etc/mcp-secrets",
					Permissions: mcpv1alpha1.MountPermissionsReadOnly,
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeSecret,
						Secret: &corev1.SecretVolumeSource{
							SecretName: "test-secret",
						},
					},
				}),
			)
		}).
		Assess("Deployment has Secret volume and mount", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			foundVolume := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Secret != nil && v.Secret.SecretName == "test-secret" {
					foundVolume = true
					break
				}
			}
			if !foundVolume {
				t.Fatal("expected volume with Secret source 'test-secret'")
			}

			foundMount := false
			for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
				if m.MountPath == "/etc/mcp-secrets" && m.ReadOnly {
					foundMount = true
					break
				}
			}
			if !foundMount {
				t.Fatal("expected read-only volume mount at /etc/mcp-secrets")
			}

			t.Log("Deployment has correct Secret volume and mount")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestStorageEmptyDir(t *testing.T) {
	feature := features.New("MCPServer with EmptyDir storage").
		WithLabel("type", "configuration").
		WithLabel("config", "storage-emptydir").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			sizeLimit := resource.MustParse("100Mi")
			return f.SetupMCPServer(ctx, t, cfg, "storage-empty", true,
				f.WithStorage(mcpv1alpha1.StorageMount{
					Path:        "/tmp/scratch",
					Permissions: mcpv1alpha1.MountPermissionsReadWrite,
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeEmptyDir,
						EmptyDir: &corev1.EmptyDirVolumeSource{
							SizeLimit: &sizeLimit,
						},
					},
				}),
			)
		}).
		Assess("Deployment has EmptyDir volume and writable mount", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			foundVolume := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.EmptyDir != nil {
					foundVolume = true
					break
				}
			}
			if !foundVolume {
				t.Fatal("expected volume with EmptyDir source")
			}

			foundMount := false
			for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
				if m.MountPath == "/tmp/scratch" && !m.ReadOnly {
					foundMount = true
					break
				}
			}
			if !foundMount {
				t.Fatal("expected writable volume mount at /tmp/scratch")
			}

			t.Log("Deployment has correct EmptyDir volume and writable mount")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestStorageRecursiveReadOnly(t *testing.T) {
	feature := features.New("MCPServer with RecursiveReadOnly storage").
		WithLabel("type", "configuration").
		WithLabel("config", "storage-recursive-readonly").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "rro-config", Namespace: ns},
				Data:       map[string]string{"key": "value"},
			}
			if err := r.Create(ctx, cm); err != nil {
				t.Fatalf("failed to create ConfigMap: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "storage-rro", true,
				f.WithStorage(mcpv1alpha1.StorageMount{
					Path:        "/etc/rro-config",
					Permissions: mcpv1alpha1.MountPermissionsRecursiveReadOnly,
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "rro-config"},
						},
					},
				}),
			)
		}).
		Assess("Deployment has RecursiveReadOnly volume mount", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			foundMount := false
			for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
				if m.MountPath == "/etc/rro-config" {
					if !m.ReadOnly {
						t.Fatal("expected ReadOnly=true for RecursiveReadOnly mount")
					}
					if m.RecursiveReadOnly == nil || *m.RecursiveReadOnly != corev1.RecursiveReadOnlyEnabled {
						t.Fatal("expected RecursiveReadOnly=Enabled")
					}
					foundMount = true
					break
				}
			}
			if !foundMount {
				t.Fatal("expected volume mount at /etc/rro-config")
			}

			t.Log("Deployment has correct RecursiveReadOnly volume mount")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestStorageMultipleMounts(t *testing.T) {
	feature := features.New("MCPServer with multiple storage mounts").
		WithLabel("type", "configuration").
		WithLabel("config", "storage-multi").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "multi-cm", Namespace: ns},
				Data:       map[string]string{"key": "value"},
			}
			if err := r.Create(ctx, cm); err != nil {
				t.Fatalf("failed to create ConfigMap: %v", err)
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "multi-secret", Namespace: ns},
				Data:       map[string][]byte{"key": []byte("secret")},
			}
			if err := r.Create(ctx, secret); err != nil {
				t.Fatalf("failed to create Secret: %v", err)
			}

			sizeLimit := resource.MustParse("50Mi")
			return f.SetupMCPServer(ctx, t, cfg, "storage-multi", true,
				f.WithStorage(
					mcpv1alpha1.StorageMount{
						Path:        "/etc/config",
						Permissions: mcpv1alpha1.MountPermissionsReadOnly,
						Source: mcpv1alpha1.StorageSource{
							Type: mcpv1alpha1.StorageTypeConfigMap,
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "multi-cm"},
							},
						},
					},
					mcpv1alpha1.StorageMount{
						Path:        "/etc/secrets",
						Permissions: mcpv1alpha1.MountPermissionsReadOnly,
						Source: mcpv1alpha1.StorageSource{
							Type: mcpv1alpha1.StorageTypeSecret,
							Secret: &corev1.SecretVolumeSource{
								SecretName: "multi-secret",
							},
						},
					},
					mcpv1alpha1.StorageMount{
						Path:        "/tmp/data",
						Permissions: mcpv1alpha1.MountPermissionsReadWrite,
						Source: mcpv1alpha1.StorageSource{
							Type:     mcpv1alpha1.StorageTypeEmptyDir,
							EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &sizeLimit},
						},
					},
				),
			)
		}).
		Assess("Deployment has all three volumes and mounts", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			volumes := dep.Spec.Template.Spec.Volumes
			if len(volumes) < 3 {
				t.Fatalf("expected at least 3 volumes, got %d", len(volumes))
			}

			mounts := dep.Spec.Template.Spec.Containers[0].VolumeMounts
			if len(mounts) < 3 {
				t.Fatalf("expected at least 3 volume mounts, got %d", len(mounts))
			}

			expectedPaths := map[string]bool{"/etc/config": false, "/etc/secrets": false, "/tmp/data": false}
			for _, m := range mounts {
				if _, ok := expectedPaths[m.MountPath]; ok {
					expectedPaths[m.MountPath] = true
				}
			}
			for path, found := range expectedPaths {
				if !found {
					t.Fatalf("expected mount at %s not found", path)
				}
			}

			t.Log("Deployment has all three volumes and mounts")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// --- Port Configuration Tests ---

func TestCustomPort(t *testing.T) {
	feature := features.New("MCPServer with custom non-default port").
		WithLabel("type", "configuration").
		WithLabel("config", "port-custom").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Use port 9090 at creation time. The test image only listens on 3001,
			// so the pod won't pass readiness, but we verify port propagation.
			return f.SetupMCPServer(ctx, t, cfg, "custom-port", false,
				f.WithPort(9090),
			)
		}).
		Assess("Accepted condition is True", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerCondition(ctx, t, r, server, "Accepted", metav1.ConditionTrue)
			t.Log("configuration accepted with custom port 9090")
			return ctx
		}).
		Assess("Deployment container port is 9090", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			containerPort := dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort
			if containerPort != 9090 {
				t.Fatalf("expected container port 9090, got %d", containerPort)
			}
			t.Log("Deployment has container port 9090")
			return ctx
		}).
		Assess("Service port is 9090", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			svc := &corev1.Service{}
			if err := r.Get(ctx, server.Name, server.Namespace, svc); err != nil {
				t.Fatalf("Service not found: %v", err)
			}

			if svc.Spec.Ports[0].Port != 9090 {
				t.Fatalf("expected Service port 9090, got %d", svc.Spec.Ports[0].Port)
			}
			t.Log("Service has port 9090")
			return ctx
		}).
		Assess("status address URL contains port 9090", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			f.AssertAddressURL(t, server, 9090)
			t.Logf("status address URL: %s", server.Status.Address.URL)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestSamePortDifferentNamespaces(t *testing.T) {
	feature := features.New("MCPServers with same port in different namespaces").
		WithLabel("type", "configuration").
		WithLabel("config", "port-namespaces").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Server A in the default test namespace
			ctx = f.SetupMCPServer(ctx, t, cfg, "server-a", true)

			// Create a second namespace for server B
			nsB := envconf.RandomName("e2e-ns2", 16)
			nsBObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsB}}
			if err := cfg.Client().Resources().Create(ctx, nsBObj); err != nil {
				t.Fatalf("failed to create second namespace: %v", err)
			}
			t.Logf("created second namespace %s", nsB)
			ctx = context.WithValue(ctx, f.ContextKey("ns2"), nsB)

			// Create server B in the second namespace
			serverB := f.NewMCPServer("server-b", nsB)
			if err := cfg.Client().Resources().Create(ctx, serverB); err != nil {
				t.Fatalf("failed to create MCPServer in second namespace: %v", err)
			}
			ctx = context.WithValue(ctx, f.ContextKey("serverB"), serverB)

			r := cfg.Client().Resources()
			f.WaitForMCPServerCondition(ctx, t, r, serverB, "Ready", metav1.ConditionTrue)
			t.Log("both MCPServers are Ready")

			return ctx
		}).
		Assess("both servers have independent Deployments and Services", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			serverA := f.ServerFromContext(ctx)
			serverB := ctx.Value(f.ContextKey("serverB")).(*mcpv1alpha1.MCPServer)
			r := cfg.Client().Resources()

			for _, server := range []*mcpv1alpha1.MCPServer{serverA, serverB} {
				dep := &appsv1.Deployment{}
				if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
					t.Fatalf("Deployment not found for %s/%s: %v", server.Namespace, server.Name, err)
				}
				if dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort != 3001 {
					t.Fatalf("expected container port 3001 for %s, got %d",
						server.Name, dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort)
				}

				svc := &corev1.Service{}
				if err := r.Get(ctx, server.Name, server.Namespace, svc); err != nil {
					t.Fatalf("Service not found for %s/%s: %v", server.Namespace, server.Name, err)
				}
				if svc.Spec.Ports[0].Port != 3001 {
					t.Fatalf("expected Service port 3001 for %s, got %d",
						server.Name, svc.Spec.Ports[0].Port)
				}
			}

			t.Log("both servers have independent Deployments and Services with port 3001")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			// Clean up server B and its namespace
			serverB := ctx.Value(f.ContextKey("serverB")).(*mcpv1alpha1.MCPServer)
			r := cfg.Client().Resources()
			if err := r.Delete(ctx, serverB); err != nil {
				t.Logf("failed to delete server B: %v", err)
			}

			nsB := ctx.Value(f.ContextKey("ns2")).(string)
			nsBObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsB}}
			if err := r.Delete(ctx, nsBObj); err != nil {
				t.Logf("failed to delete second namespace: %v", err)
			}

			// Clean up server A via standard teardown
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// --- Security Context Tests ---

func TestDefaultSecurityContext(t *testing.T) {
	feature := features.New("MCPServer with default security context").
		WithLabel("type", "configuration").
		WithLabel("config", "security-default").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "sec-default", true)
		}).
		Assess("Deployment has restricted default security context", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			sc := dep.Spec.Template.Spec.Containers[0].SecurityContext
			if sc == nil {
				t.Fatal("expected container security context to be set")
			}

			if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
				t.Fatal("expected AllowPrivilegeEscalation=false")
			}
			if sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
				t.Fatal("expected ReadOnlyRootFilesystem=true")
			}
			if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
				t.Fatal("expected RunAsNonRoot=true")
			}
			if sc.Capabilities == nil || len(sc.Capabilities.Drop) == 0 || sc.Capabilities.Drop[0] != "ALL" {
				t.Fatal("expected Capabilities.Drop=[ALL]")
			}
			if sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
				t.Fatal("expected SeccompProfile.Type=RuntimeDefault")
			}

			t.Log("default restricted security context is correctly applied")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestCustomSecurityContext(t *testing.T) {
	feature := features.New("MCPServer with custom security context").
		WithLabel("type", "configuration").
		WithLabel("config", "security-custom").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "sec-custom", true,
				f.WithSecurityContext(&corev1.SecurityContext{
					RunAsNonRoot:             ptr.To(true),
					ReadOnlyRootFilesystem:   ptr.To(true),
					AllowPrivilegeEscalation: ptr.To(false),
				}),
			)
		}).
		Assess("Deployment uses custom security context without defaults injected", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			sc := dep.Spec.Template.Spec.Containers[0].SecurityContext
			if sc == nil {
				t.Fatal("expected container security context to be set")
			}

			if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
				t.Fatal("expected RunAsNonRoot=true")
			}
			if sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem {
				t.Fatal("expected ReadOnlyRootFilesystem=true")
			}
			if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
				t.Fatal("expected AllowPrivilegeEscalation=false")
			}
			// Custom context was provided, so defaults like Capabilities and SeccompProfile
			// should NOT be injected by the controller.
			if sc.Capabilities != nil {
				t.Fatal("expected Capabilities to be nil (custom context, no defaults)")
			}
			if sc.SeccompProfile != nil {
				t.Fatal("expected SeccompProfile to be nil (custom context, no defaults)")
			}

			t.Log("custom security context applied without default injection")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestPodSecurityContext(t *testing.T) {
	feature := features.New("MCPServer with pod security context").
		WithLabel("type", "configuration").
		WithLabel("config", "security-pod").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "sec-pod", true,
				f.WithPodSecurityContext(&corev1.PodSecurityContext{
					RunAsNonRoot: ptr.To(true),
					RunAsUser:    ptr.To(int64(1000)),
					FSGroup:      ptr.To(int64(2000)),
				}),
			)
		}).
		Assess("Deployment has pod security context", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			psc := dep.Spec.Template.Spec.SecurityContext
			if psc == nil {
				t.Fatal("expected pod security context to be set")
			}

			if psc.RunAsNonRoot == nil || !*psc.RunAsNonRoot {
				t.Fatal("expected RunAsNonRoot=true")
			}
			if psc.RunAsUser == nil || *psc.RunAsUser != 1000 {
				t.Fatal("expected RunAsUser=1000")
			}
			if psc.FSGroup == nil || *psc.FSGroup != 2000 {
				t.Fatal("expected FSGroup=2000")
			}

			t.Log("pod security context is correctly applied")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// --- Custom Metadata Tests ---

func TestCustomLabelsAndAnnotations(t *testing.T) {
	feature := features.New("MCPServer with custom labels and annotations").
		WithLabel("type", "configuration").
		WithLabel("config", "metadata-custom").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "meta-custom", true,
				f.WithExtraLabels(map[string]string{"team": "platform", "env": "test"}),
				f.WithExtraAnnotations(map[string]string{"note": "e2e-test"}),
			)
		}).
		Assess("Deployment has custom labels and annotations", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			// Deployment metadata
			if dep.Labels["team"] != "platform" {
				t.Fatalf("expected Deployment label team=platform, got %q", dep.Labels["team"])
			}
			if dep.Labels["env"] != "test" {
				t.Fatalf("expected Deployment label env=test, got %q", dep.Labels["env"])
			}
			if dep.Annotations["note"] != "e2e-test" {
				t.Fatalf("expected Deployment annotation note=e2e-test, got %q", dep.Annotations["note"])
			}

			// Pod template metadata
			if dep.Spec.Template.Labels["team"] != "platform" {
				t.Fatalf("expected Pod template label team=platform, got %q", dep.Spec.Template.Labels["team"])
			}
			if dep.Spec.Template.Labels["env"] != "test" {
				t.Fatalf("expected Pod template label env=test, got %q", dep.Spec.Template.Labels["env"])
			}
			if dep.Spec.Template.Annotations["note"] != "e2e-test" {
				t.Fatalf("expected Pod template annotation note=e2e-test, got %q", dep.Spec.Template.Annotations["note"])
			}

			t.Log("Deployment has correct custom labels and annotations")
			return ctx
		}).
		Assess("Service has custom labels and annotations", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			svc := &corev1.Service{}
			if err := r.Get(ctx, server.Name, server.Namespace, svc); err != nil {
				t.Fatalf("Service not found: %v", err)
			}

			if svc.Labels["team"] != "platform" {
				t.Fatalf("expected Service label team=platform, got %q", svc.Labels["team"])
			}
			if svc.Labels["env"] != "test" {
				t.Fatalf("expected Service label env=test, got %q", svc.Labels["env"])
			}
			if svc.Annotations["note"] != "e2e-test" {
				t.Fatalf("expected Service annotation note=e2e-test, got %q", svc.Annotations["note"])
			}

			t.Log("Service has correct custom labels and annotations")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestReservedLabelFiltering(t *testing.T) {
	feature := features.New("MCPServer reserved label filtering").
		WithLabel("type", "configuration").
		WithLabel("config", "metadata-reserved").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "meta-reserved", true,
				f.WithExtraLabels(map[string]string{
					"app":        "my-custom-app",
					"mcp-server": "custom-value",
					"valid-key":  "valid-value",
				}),
			)
		}).
		Assess("reserved labels are not overridden", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			if dep.Labels["app"] != "mcp-server" {
				t.Fatalf("expected reserved label app=mcp-server, got %q", dep.Labels["app"])
			}
			if dep.Labels["mcp-server"] != server.Name {
				t.Fatalf("expected reserved label mcp-server=%s, got %q", server.Name, dep.Labels["mcp-server"])
			}
			if dep.Labels["valid-key"] != "valid-value" {
				t.Fatalf("expected non-reserved label valid-key=valid-value, got %q", dep.Labels["valid-key"])
			}

			t.Log("reserved labels protected, non-reserved labels applied")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestCustomMetadataUpdate(t *testing.T) {
	feature := features.New("MCPServer custom metadata update").
		WithLabel("type", "configuration").
		WithLabel("config", "metadata-update").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "meta-update", true,
				f.WithExtraLabels(map[string]string{"team": "alpha"}),
			)
		}).
		Assess("record Deployment UID", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			if dep.Labels["team"] != "alpha" {
				t.Fatalf("expected initial label team=alpha, got %q", dep.Labels["team"])
			}

			ctx = context.WithValue(ctx, f.ContextKey("depUID"), dep.UID)
			t.Logf("recorded Deployment UID: %s", dep.UID)
			return ctx
		}).
		Assess("update labels on MCPServer", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.UpdateWithRetry(ctx, t, r, server, func(s *mcpv1alpha1.MCPServer) {
				s.Spec.ExtraLabels = map[string]string{"team": "beta", "tier": "backend"}
			})
			t.Log("updated MCPServer labels to {team:beta, tier:backend}")

			return ctx
		}).
		Assess("labels propagated without Deployment recreation", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			// Verify labels updated
			if dep.Labels["team"] != "beta" {
				t.Fatalf("expected label team=beta, got %q", dep.Labels["team"])
			}
			if dep.Labels["tier"] != "backend" {
				t.Fatalf("expected label tier=backend, got %q", dep.Labels["tier"])
			}

			// Verify Deployment was not recreated
			originalUID := ctx.Value(f.ContextKey("depUID"))
			if dep.UID != originalUID {
				t.Fatalf("Deployment was recreated (UID changed from %v to %s)", originalUID, dep.UID)
			}

			// Verify Service also has updated labels
			svc := &corev1.Service{}
			if err := r.Get(ctx, server.Name, server.Namespace, svc); err != nil {
				t.Fatalf("Service not found: %v", err)
			}
			if svc.Labels["team"] != "beta" {
				t.Fatalf("expected Service label team=beta, got %q", svc.Labels["team"])
			}
			if svc.Labels["tier"] != "backend" {
				t.Fatalf("expected Service label tier=backend, got %q", svc.Labels["tier"])
			}

			t.Log("labels updated on Deployment and Service without recreation")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
