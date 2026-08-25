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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scenario"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

const byoDeployName = "my-byo-deploy"
const byoServiceName = "my-byo-svc"

func newBYODeployment(name, namespace string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "busybox",
						Image:   f.BusyboxImage,
						Command: []string{"sleep", "3600"},
					}},
				},
			},
		},
	}
}

func newBYOService(name, namespace string, port int32, portName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": byoDeployName},
			Ports: []corev1.ServicePort{{
				Name:     portName,
				Port:     port,
				Protocol: corev1.ProtocolTCP,
			}},
		},
	}
}

func waitForDeploymentReady(ctx context.Context, t *testing.T, cfg *envconf.Config, name, namespace string) {
	t.Helper()
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	err := wait.For(
		conditions.New(cfg.Client().Resources()).ResourceMatch(dep, func(obj k8s.Object) bool {
			d := obj.(*appsv1.Deployment)
			return d.Status.ReadyReplicas > 0 && d.Status.ReadyReplicas >= ptr.Deref(d.Spec.Replicas, 1)
		}),
		wait.WithTimeout(3*time.Minute),
		wait.WithInterval(2*time.Second),
	)
	if err != nil {
		t.Fatalf("Deployment %s/%s never became ready: %v", namespace, name, err)
	}
}

func TestBYOWorkloadHappyPath(t *testing.T) {
	t.Parallel()
	feature := features.New("BYO workload happy path").
		WithLabel(category.Label, category.Lifecycle).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scenario.Label, scenario.BYO).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			dep := newBYODeployment(byoDeployName, ns, 1)
			if err := r.Create(ctx, dep); err != nil {
				t.Fatalf("failed to create BYO Deployment: %v", err)
			}
			t.Logf("created BYO Deployment %s/%s", ns, byoDeployName)

			waitForDeploymentReady(ctx, t, cfg, byoDeployName, ns)

			return f.SetupBYOMCPServer(ctx, t, cfg, "test-byo-wl", &mcpv1alpha1.WorkloadReference{
				Name: byoDeployName,
				Kind: mcpv1alpha1.WorkloadKindDeployment,
			})
		}).
		Assess("status reflects BYO workload", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerReconciled(ctx, t, r, server)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			expectedWorkloadName := "Deployment/" + byoDeployName
			if server.Status.WorkloadName != expectedWorkloadName {
				t.Fatalf("expected workloadName %q, got %q", expectedWorkloadName, server.Status.WorkloadName)
			}

			expectedSummary := "BYO:Deployment/" + byoDeployName
			if server.Status.WorkloadSummary != expectedSummary {
				t.Fatalf("expected workloadSummary %q, got %q", expectedSummary, server.Status.WorkloadSummary)
			}

			accepted := f.GetMCPServerCondition(server, "Accepted")
			if accepted == nil || accepted.Status != metav1.ConditionTrue {
				t.Fatal("Accepted condition is not True")
			}

			t.Logf("BYO status: workloadName=%s, workloadSummary=%s, Accepted=True",
				server.Status.WorkloadName, server.Status.WorkloadSummary)
			return ctx
		}).
		Assess("no operator-managed Deployment created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			operatorDep := &appsv1.Deployment{}
			err := r.Get(ctx, server.Name, server.Namespace, operatorDep)
			if err == nil {
				t.Fatalf("operator-managed Deployment %s should not exist for BYO workload", server.Name)
			}
			if !apierrors.IsNotFound(err) {
				t.Fatalf("unexpected error checking for operator-managed Deployment: %v", err)
			}

			t.Log("confirmed no operator-managed Deployment exists")
			return ctx
		}).
		Assess("no ownerRef on BYO Deployment", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, byoDeployName, server.Namespace, dep); err != nil {
				t.Fatalf("failed to get BYO Deployment: %v", err)
			}
			if len(dep.OwnerReferences) != 0 {
				t.Fatalf("BYO Deployment should have no ownerReferences, got %d", len(dep.OwnerReferences))
			}

			t.Log("confirmed BYO Deployment has no ownerReferences")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			ctx = f.TeardownBYOMCPServer(ctx, t, cfg)

			dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: byoDeployName, Namespace: ns}}
			if err := r.Delete(ctx, dep); err != nil && !apierrors.IsNotFound(err) {
				t.Logf("failed to delete BYO Deployment: %v", err)
			}
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestBYOServiceHappyPath(t *testing.T) {
	t.Parallel()
	feature := features.New("BYO workload and service happy path").
		WithLabel(category.Label, category.Lifecycle).
		WithLabel(speed.Label, speed.Moderate).
		WithLabel(scenario.Label, scenario.BYO).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			dep := newBYODeployment(byoDeployName, ns, 1)
			if err := r.Create(ctx, dep); err != nil {
				t.Fatalf("failed to create BYO Deployment: %v", err)
			}
			waitForDeploymentReady(ctx, t, cfg, byoDeployName, ns)

			svc := newBYOService(byoServiceName, ns, 9090, "mcp")
			if err := r.Create(ctx, svc); err != nil {
				t.Fatalf("failed to create BYO Service: %v", err)
			}
			t.Logf("created BYO Service %s/%s", ns, byoServiceName)

			return f.SetupBYOMCPServer(ctx, t, cfg, "test-byo-svc", &mcpv1alpha1.WorkloadReference{
				Name: byoDeployName,
				Kind: mcpv1alpha1.WorkloadKindDeployment,
			}, f.WithServiceRef(&mcpv1alpha1.ServiceReference{Name: byoServiceName}))
		}).
		Assess("status uses BYO service name", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerReconciled(ctx, t, r, server)

			if err := r.Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}

			if server.Status.ServiceName != byoServiceName {
				t.Fatalf("expected serviceName %q, got %q", byoServiceName, server.Status.ServiceName)
			}

			t.Logf("BYO service status: serviceName=%s", server.Status.ServiceName)
			return ctx
		}).
		Assess("no operator-managed Service created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			operatorSvc := &corev1.Service{}
			err := r.Get(ctx, server.Name, server.Namespace, operatorSvc)
			if err == nil {
				t.Fatalf("operator-managed Service %s should not exist for BYO service", server.Name)
			}
			if !apierrors.IsNotFound(err) {
				t.Fatalf("unexpected error checking for operator-managed Service: %v", err)
			}

			t.Log("confirmed no operator-managed Service exists")
			return ctx
		}).
		Assess("no ownerRef on BYO resources", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, byoDeployName, server.Namespace, dep); err != nil {
				t.Fatalf("failed to get BYO Deployment: %v", err)
			}
			if len(dep.OwnerReferences) != 0 {
				t.Fatalf("BYO Deployment should have no ownerReferences, got %d", len(dep.OwnerReferences))
			}

			svc := &corev1.Service{}
			if err := r.Get(ctx, byoServiceName, server.Namespace, svc); err != nil {
				t.Fatalf("failed to get BYO Service: %v", err)
			}
			if len(svc.OwnerReferences) != 0 {
				t.Fatalf("BYO Service should have no ownerReferences, got %d", len(svc.OwnerReferences))
			}

			t.Log("confirmed BYO resources have no ownerReferences")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			ctx = f.TeardownBYOMCPServer(ctx, t, cfg)

			dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: byoDeployName, Namespace: ns}}
			if err := r.Delete(ctx, dep); err != nil && !apierrors.IsNotFound(err) {
				t.Logf("failed to delete BYO Deployment: %v", err)
			}
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: byoServiceName, Namespace: ns}}
			if err := r.Delete(ctx, svc); err != nil && !apierrors.IsNotFound(err) {
				t.Logf("failed to delete BYO Service: %v", err)
			}
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestBYOWorkloadNotFound(t *testing.T) {
	t.Parallel()
	feature := features.New("BYO workload reference not found").
		WithLabel(category.Label, category.Lifecycle).
		WithLabel(speed.Label, speed.Fast).
		WithLabel(scenario.Label, scenario.BYO).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.SetupBYOMCPServer(ctx, t, cfg, "test-byo-missing", &mcpv1alpha1.WorkloadReference{
				Name: "nonexistent-deploy",
				Kind: mcpv1alpha1.WorkloadKindDeployment,
			})
		}).
		Assess("Accepted is False with not found message", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerConditionMessageContains(ctx, t, r, server,
				"Accepted", metav1.ConditionFalse, "Invalid", "not found")

			t.Log("confirmed Accepted=False for missing BYO workload")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			return f.TeardownBYOMCPServer(ctx, t, cfg)
		}).
		Feature()

	testenv.Test(t, feature)
}

func TestBYOWorkloadStatusUpdate(t *testing.T) {
	t.Parallel()
	feature := features.New("BYO workload status tracks scaling").
		WithLabel(category.Label, category.Lifecycle).
		WithLabel(speed.Label, speed.Slow).
		WithLabel(scenario.Label, scenario.BYO).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			dep := newBYODeployment(byoDeployName, ns, 1)
			if err := r.Create(ctx, dep); err != nil {
				t.Fatalf("failed to create BYO Deployment: %v", err)
			}
			waitForDeploymentReady(ctx, t, cfg, byoDeployName, ns)

			return f.SetupBYOMCPServer(ctx, t, cfg, "test-byo-scale", &mcpv1alpha1.WorkloadReference{
				Name: byoDeployName,
				Kind: mcpv1alpha1.WorkloadKindDeployment,
			})
		}).
		Assess("initial reconciliation completes", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()

			f.WaitForMCPServerReconciled(ctx, t, r, server)
			t.Log("initial reconciliation complete")
			return ctx
		}).
		Assess("scale BYO workload and verify status update", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			ns := server.Namespace
			r := cfg.Client().Resources()

			dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: byoDeployName, Namespace: ns}}
			f.UpdateWithRetry(ctx, t, r, dep, func(d *appsv1.Deployment) {
				d.Spec.Replicas = ptr.To(int32(2))
			})
			t.Log("scaled BYO Deployment to 2 replicas")

			waitForDeploymentReady(ctx, t, cfg, byoDeployName, ns)

			err := wait.For(
				conditions.New(r).ResourceMatch(server, func(obj k8s.Object) bool {
					s := obj.(*mcpv1alpha1.MCPServer)
					return s.Status.Replicas == 2
				}),
				wait.WithTimeout(3*time.Minute),
				wait.WithInterval(2*time.Second),
			)
			if err != nil {
				t.Fatalf("MCPServer status did not reflect scaled replicas: %v", err)
			}

			t.Log("MCPServer status reflects 2 replicas after BYO Deployment scale")
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()

			ctx = f.TeardownBYOMCPServer(ctx, t, cfg)

			dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: byoDeployName, Namespace: ns}}
			if err := r.Delete(ctx, dep); err != nil && !apierrors.IsNotFound(err) {
				t.Logf("failed to delete BYO Deployment: %v", err)
			}
			return ctx
		}).
		Feature()

	testenv.Test(t, feature)
}
