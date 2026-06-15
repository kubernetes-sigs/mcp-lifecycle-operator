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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
)

const configHashAnnotation = "mcp.x-k8s.io/config-hash"

// --- Spec Update Tests ---

func TestImageUpdate(t *testing.T) {
	digestRef := "quay.io/matzew/mcp-everything@sha256:537cdedad807bb56140caca9c332d3577b16e533584164bbc3f27abac7b5ba15"

	feature := features.New("MCPServer image update").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "image-update").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "img-update", true)
		}).
		Assess("update image to digest ref", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.UpdateWithRetry(ctx, t, r, server, func(s *mcpv1alpha1.MCPServer) {
				s.Spec.Source.ContainerImage.Ref = digestRef
			})
			t.Log("updated image to digest ref")

			return ctx
		}).
		Assess("Deployment reflects new image after reconciliation", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			if len(dep.Spec.Template.Spec.Containers) == 0 {
				t.Fatal("expected at least one container in Deployment pod template")
			}
			actualImage := dep.Spec.Template.Spec.Containers[0].Image
			if actualImage != digestRef {
				t.Fatalf("expected image %q, got %q", digestRef, actualImage)
			}

			t.Logf("Deployment image updated to %s", actualImage)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestStorageAddition(t *testing.T) {
	feature := features.New("MCPServer storage addition").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "storage-add").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "add-config", Namespace: ns},
				Data:       map[string]string{"key": "value"},
			}
			if err := r.Create(ctx, cm); err != nil {
				t.Fatalf("failed to create ConfigMap: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "storage-add", true)
		}).
		Assess("add storage mount to existing MCPServer", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.UpdateWithRetry(ctx, t, r, server, func(s *mcpv1alpha1.MCPServer) {
				s.Spec.Config.Storage = []mcpv1alpha1.StorageMount{
					{
						Path:        "/etc/added-config",
						Permissions: mcpv1alpha1.MountPermissionsReadOnly,
						Source: mcpv1alpha1.StorageSource{
							Type: mcpv1alpha1.StorageTypeConfigMap,
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "add-config"},
							},
						},
					},
				}
			})
			t.Log("added ConfigMap storage mount")

			return ctx
		}).
		Assess("Deployment has new volume and mount", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			foundVolume := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.ConfigMap != nil && v.ConfigMap.Name == "add-config" {
					foundVolume = true
					break
				}
			}
			if !foundVolume {
				t.Fatal("expected ConfigMap volume 'add-config' after storage addition")
			}

			if len(dep.Spec.Template.Spec.Containers) == 0 {
				t.Fatal("expected at least one container in Deployment pod template")
			}
			foundMount := false
			for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
				if m.MountPath == "/etc/added-config" {
					foundMount = true
					break
				}
			}
			if !foundMount {
				t.Fatal("expected volume mount at /etc/added-config")
			}

			t.Log("storage addition verified on Deployment")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestStorageRemoval(t *testing.T) {
	feature := features.New("MCPServer storage removal").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "storage-remove").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "remove-config", Namespace: ns},
				Data:       map[string]string{"key": "value"},
			}
			if err := r.Create(ctx, cm); err != nil {
				t.Fatalf("failed to create ConfigMap: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "storage-rm", true,
				f.WithStorage(mcpv1alpha1.StorageMount{
					Path:        "/etc/remove-config",
					Permissions: mcpv1alpha1.MountPermissionsReadOnly,
					Source: mcpv1alpha1.StorageSource{
						Type: mcpv1alpha1.StorageTypeConfigMap,
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "remove-config"},
						},
					},
				}),
			)
		}).
		Assess("remove storage mount", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.UpdateWithRetry(ctx, t, r, server, func(s *mcpv1alpha1.MCPServer) {
				s.Spec.Config.Storage = nil
			})
			t.Log("removed storage mount")

			return ctx
		}).
		Assess("Deployment no longer has the volume", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.ConfigMap != nil && v.ConfigMap.Name == "remove-config" {
					t.Fatal("ConfigMap volume 'remove-config' should have been removed")
				}
			}
			if len(dep.Spec.Template.Spec.Containers) == 0 {
				t.Fatal("expected at least one container in Deployment pod template")
			}
			for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
				if m.MountPath == "/etc/remove-config" {
					t.Fatal("volume mount at /etc/remove-config should have been removed")
				}
			}

			t.Log("storage removal verified on Deployment")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// --- Drift Detection Tests ---

func TestReplicaDrift(t *testing.T) {
	feature := features.New("MCPServer replica drift correction").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "drift-replicas").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "drift-repl", true, f.WithReplicas(1))
		}).
		Assess("manually scale Deployment and verify reconciliation", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace}}
			f.UpdateWithRetry(ctx, t, r, dep, func(d *appsv1.Deployment) {
				d.Spec.Replicas = ptr.To(int32(3))
			})
			t.Log("manually scaled Deployment to 3 replicas")

			expectedReplicas := int32(1)
			if server.Spec.Runtime.Replicas != nil {
				expectedReplicas = *server.Spec.Runtime.Replicas
			}

			err := wait.For(
				conditions.New(r).ResourceMatch(dep, func(obj k8s.Object) bool {
					d := obj.(*appsv1.Deployment)
					return d.Spec.Replicas != nil && *d.Spec.Replicas == expectedReplicas
				}),
				wait.WithTimeout(2*time.Minute),
				wait.WithInterval(2*time.Second),
			)
			if err != nil {
				t.Fatalf("controller did not reconcile replicas back to %d: %v", expectedReplicas, err)
			}

			t.Logf("controller reconciled replicas back to %d", expectedReplicas)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestServicePortDrift(t *testing.T) {
	feature := features.New("MCPServer Service port drift correction").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "drift-service-port").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "drift-port", true)
		}).
		Assess("manually change Service port and verify reconciliation", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace}}
			f.UpdateWithRetry(ctx, t, r, svc, func(s *corev1.Service) {
				if len(s.Spec.Ports) == 0 {
					t.Fatal("expected at least one port in Service spec")
				}
				s.Spec.Ports[0].Port = 9999
			})
			t.Log("manually changed Service port to 9999")

			expectedPort := server.Spec.Config.Port

			err := wait.For(
				conditions.New(r).ResourceMatch(svc, func(obj k8s.Object) bool {
					s := obj.(*corev1.Service)
					return len(s.Spec.Ports) > 0 && s.Spec.Ports[0].Port == expectedPort
				}),
				wait.WithTimeout(2*time.Minute),
				wait.WithInterval(2*time.Second),
			)
			if err != nil {
				t.Fatalf("controller did not reconcile Service port back to %d: %v", expectedPort, err)
			}

			t.Logf("controller reconciled Service port back to %d", expectedPort)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestDeploymentDeletion(t *testing.T) {
	feature := features.New("MCPServer Deployment recreation after deletion").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "drift-deployment-deleted").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "drift-dep", true)
		}).
		Assess("delete Deployment and verify recreation", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}
			originalUID := dep.UID

			if err := r.Delete(ctx, dep); err != nil {
				t.Fatalf("failed to delete Deployment: %v", err)
			}
			t.Logf("deleted Deployment (UID=%s)", originalUID)

			// Wait for a new Deployment to appear
			newDep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
			}
			err := wait.For(
				conditions.New(r).ResourceMatch(newDep, func(obj k8s.Object) bool {
					d := obj.(*appsv1.Deployment)
					return d.UID != originalUID && d.UID != ""
				}),
				wait.WithTimeout(2*time.Minute),
				wait.WithInterval(2*time.Second),
			)
			if err != nil {
				t.Fatalf("controller did not recreate Deployment: %v", err)
			}

			f.WaitForMCPServerCondition(ctx, t, r, server, "Ready", metav1.ConditionTrue)
			t.Logf("Deployment recreated with new UID=%s, MCPServer is Ready", newDep.UID)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestServiceDeletion(t *testing.T) {
	feature := features.New("MCPServer Service recreation after deletion").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "drift-service-deleted").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "drift-svc", true)
		}).
		Assess("delete Service and verify recreation", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			svc := &corev1.Service{}
			if err := r.Get(ctx, server.Name, server.Namespace, svc); err != nil {
				t.Fatalf("Service not found: %v", err)
			}
			originalUID := svc.UID

			if err := r.Delete(ctx, svc); err != nil {
				t.Fatalf("failed to delete Service: %v", err)
			}
			t.Logf("deleted Service (UID=%s)", originalUID)

			newSvc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
			}
			err := wait.For(
				conditions.New(r).ResourceMatch(newSvc, func(obj k8s.Object) bool {
					s := obj.(*corev1.Service)
					return s.UID != originalUID && s.UID != ""
				}),
				wait.WithTimeout(2*time.Minute),
				wait.WithInterval(2*time.Second),
			)
			if err != nil {
				t.Fatalf("controller did not recreate Service: %v", err)
			}

			f.WaitForMCPServerCondition(ctx, t, r, server, "Ready", metav1.ConditionTrue)
			t.Logf("Service recreated with new UID=%s, MCPServer is Ready", newSvc.UID)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

// --- Ownership and Garbage Collection Tests ---

func TestOwnerReferences(t *testing.T) {
	feature := features.New("MCPServer OwnerReferences on child resources").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "ownership").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "owner-ref", true)
		}).
		Assess("Deployment and Service have correct OwnerReferences", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}
			assertOwnerReference(t, dep.OwnerReferences, server.Name, server.UID, "Deployment")

			svc := &corev1.Service{}
			if err := r.Get(ctx, server.Name, server.Namespace, svc); err != nil {
				t.Fatalf("Service not found: %v", err)
			}
			assertOwnerReference(t, svc.OwnerReferences, server.Name, server.UID, "Service")

			t.Log("both Deployment and Service have correct OwnerReferences")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func assertOwnerReference(t *testing.T, refs []metav1.OwnerReference, expectedName string, expectedUID types.UID, resourceKind string) {
	t.Helper()
	for _, ref := range refs {
		if ref.Kind == "MCPServer" && ref.Name == expectedName && ref.UID == expectedUID {
			if ref.Controller == nil || !*ref.Controller {
				t.Fatalf("%s OwnerReference has controller=false, expected true", resourceKind)
			}
			return
		}
	}
	t.Fatalf("%s missing OwnerReference to MCPServer %s (UID=%s)", resourceKind, expectedName, expectedUID)
}

func TestCascadingDeletion(t *testing.T) {
	feature := features.New("MCPServer cascading deletion").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "cascading-delete").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupMCPServer(ctx, t, cfg, "cascade-del", true)
		}).
		Assess("delete MCPServer and verify child resources are garbage collected", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			if err := r.Delete(ctx, server); err != nil {
				t.Fatalf("failed to delete MCPServer: %v", err)
			}
			t.Logf("deleted MCPServer %s/%s", server.Namespace, server.Name)

			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
			}
			if err := wait.For(
				conditions.New(r).ResourceDeleted(dep),
				wait.WithTimeout(1*time.Minute),
				wait.WithInterval(2*time.Second),
			); err != nil {
				t.Fatalf("Deployment was not garbage collected: %v", err)
			}

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
			}
			if err := wait.For(
				conditions.New(r).ResourceDeleted(svc),
				wait.WithTimeout(1*time.Minute),
				wait.WithInterval(2*time.Second),
			); err != nil {
				t.Fatalf("Service was not garbage collected: %v", err)
			}

			t.Log("Deployment and Service garbage collected after MCPServer deletion")
			return ctx
		}).
		// No TeardownMCPServer needed — we already deleted the MCPServer in Assess.
		Feature()

	testenv.Test(t, feature)
}

// --- Config Hash Tests ---

func TestConfigMapDataUpdateTriggersRestart(t *testing.T) {
	feature := features.New("MCPServer config hash update on ConfigMap change").
		WithLabel("type", "reconciliation").
		WithLabel("scenario", "config-hash").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "hash-config", Namespace: ns},
				Data:       map[string]string{"setting": "original"},
			}
			if err := r.Create(ctx, cm); err != nil {
				t.Fatalf("failed to create ConfigMap: %v", err)
			}

			return f.SetupMCPServer(ctx, t, cfg, "config-hash", true,
				f.WithEnvFrom(corev1.EnvFromSource{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "hash-config"},
					},
				}),
			)
		}).
		Assess("record initial config hash", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("Deployment not found: %v", err)
			}

			hash := dep.Spec.Template.Annotations[configHashAnnotation]
			if hash == "" {
				t.Fatal("expected config-hash annotation on pod template")
			}
			ctx = context.WithValue(ctx, f.ContextKey("initialHash"), hash)
			t.Logf("initial config hash: %s", hash)

			return ctx
		}).
		Assess("update ConfigMap data", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "hash-config", Namespace: ns}}
			f.UpdateWithRetry(ctx, t, r, cm, func(c *corev1.ConfigMap) {
				c.Data["setting"] = "updated"
			})
			t.Log("updated ConfigMap data")

			return ctx
		}).
		Assess("config hash changed on Deployment", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			initialHash := ctx.Value(f.ContextKey("initialHash")).(string)

			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: server.Name, Namespace: server.Namespace},
			}
			err := wait.For(
				conditions.New(r).ResourceMatch(dep, func(obj k8s.Object) bool {
					d := obj.(*appsv1.Deployment)
					newHash := d.Spec.Template.Annotations[configHashAnnotation]
					return newHash != "" && newHash != initialHash
				}),
				wait.WithTimeout(2*time.Minute),
				wait.WithInterval(2*time.Second),
			)
			if err != nil {
				t.Fatalf("config hash did not change after ConfigMap update: %v", err)
			}

			if err := r.Get(ctx, server.Name, server.Namespace, dep); err != nil {
				t.Fatalf("failed to re-fetch Deployment: %v", err)
			}
			newHash := dep.Spec.Template.Annotations[configHashAnnotation]
			t.Logf("config hash changed from %s to %s", initialHash, newHash)

			f.WaitForMCPServerCondition(ctx, t, r, server, "Ready", metav1.ConditionTrue)
			t.Log("MCPServer is Ready after config hash update")

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}
