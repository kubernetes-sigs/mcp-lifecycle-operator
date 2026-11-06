//go:build e2e

/*
Copyright 2026 The Kubernetes Authors.

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

package conformance

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
	mcpv1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
	f "github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/category"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scenario"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/scope"
	"github.com/kubernetes-sigs/mcp-lifecycle-operator/test/e2e/framework/labels/speed"
)

type byoConfiguration struct {
	name             string
	externalWorkload bool
	externalService  bool
}

func runBYO(t *testing.T, testenv env.Environment, prov Provider, tc byoConfiguration) {
	const serverName = "conformance-byo"
	const configName = "gw-byo-config"
	var gatewayAddress, serviceName string
	var selector map[string]string
	var servicePort int32
	var deployment *appsv1.Deployment
	var service *corev1.Service

	feature := features.New("Gateway conformance: "+tc.name).
		WithLabel(category.Label, category.Networking).
		WithLabel(speed.Label, speed.Slow).
		WithLabel(scope.Label, scope.GatewayConformance).
		WithLabel(scenario.Label, scenario.BYO).
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ns := ctx.Value(f.NsKey).(string)
			r := cfg.Client().Resources()
			prov.Setup(ctx, t, cfg, ns)
			gatewayAddress = prov.GatewayAddress(ctx, t, cfg)
			config := prov.ConfigData()
			config["route-hostname"] = "byo." + ns + ".mcp.e2e.test"
			delete(config, "public-hostname") // Use the route hostname fallback for this BYO case only.
			f.CreateGatewayConfigMap(ctx, t, cfg, configName, ns, config)
			createBackendPodReader(ctx, t, r, ns)

			serviceName, servicePort = serverName, 8080
			selector = map[string]string{mcpcontroller.LabelKeyMCPServer: serverName}
			opts := []f.MCPServerOption{f.WithGateway(prov.Name(), configName), f.WithPath("/mcp")}
			if tc.externalWorkload {
				deployment = createBYODeployment(ctx, t, r, ns)
				selector = deployment.Spec.Selector.MatchLabels
				opts = append(opts, f.WithWorkloadRef(&mcpv1beta1.WorkloadReference{
					Kind: mcpv1beta1.WorkloadKindDeployment, Name: deployment.Name,
				}))
			}
			if tc.externalService {
				service = createBYOService(ctx, t, r, ns, selector)
				serviceName, servicePort = service.Name, 80
				opts = append(opts, f.WithServiceRef(&mcpv1beta1.ServiceReference{Name: service.Name}))
				if tc.externalWorkload {
					opts = append(opts, f.WithPort(0)) // Infer the named MCP port, not the first port.
				}
			}
			ctx = f.SetupMCPServer(ctx, t, cfg, serverName, false, opts...)
			server := f.ServerFromContext(ctx)
			f.WaitForMCPServerReconciledAndReady(ctx, t, r, server)
			f.WaitForMCPServerGatewayAddress(ctx, t, r, server)
			f.WaitForEndpointsReady(ctx, t, cfg, ns, serviceName)
			return ctx
		}).
		Assess("binding is registered and route targets the resolved Service port", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			r := cfg.Client().Resources()
			binding := &mcpv1alpha1.MCPGatewayBinding{ObjectMeta: metav1.ObjectMeta{
				Name: server.Name + "-gateway-binding", Namespace: server.Namespace,
			}}
			f.WaitForBindingRegistered(ctx, t, r, binding, metav1.ConditionTrue)
			waitForBYORoute(ctx, t, r, binding.Name, server.Namespace, serviceName, servicePort)
			assertBYOResourcesUnchanged(ctx, t, r, deployment, service)
			return ctx
		}).
		Assess("backend tool call succeeds through the gateway", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			server := f.ServerFromContext(ctx)
			if err := cfg.Client().Resources().Get(ctx, server.Name, server.Namespace, server); err != nil {
				t.Fatalf("failed to get MCPServer: %v", err)
			}
			if server.Status.Address == nil || server.Status.Address.URL == "" {
				t.Fatal("MCPServer has no published gateway address")
			}
			address, err := url.Parse(server.Status.Address.URL)
			if err != nil {
				t.Fatalf("invalid gateway URL: %v", err)
			}
			assertBackendPodListed(ctx, t, gatewayAddress, address.Hostname(), address.Path, server.Namespace, cfg.Client().Resources(), selector)
			return ctx
		}).
		Assess("referenced resources survive MCPServer deletion", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ctx = f.TeardownMCPServer(ctx, t, cfg)
			assertBYOResourcesUnchanged(ctx, t, cfg.Client().Resources(), deployment, service)
			return ctx
		}).
		Teardown(f.TeardownMCPServer).
		Feature()

	testenv.Test(t, feature)
}

func createBYODeployment(ctx context.Context, t *testing.T, r *resources.Resources, ns string) *appsv1.Deployment {
	t.Helper()
	labels := map[string]string{"app": "external-mcp"}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "external-mcp", Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "server", Image: f.DefaultMCPServerImage,
					Args: []string{"--port", "8080", "--read-only"},
					// No named container port: the generated Service must use a numeric target.
					ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8080)},
					}},
				}}},
			},
		},
	}
	if err := r.Create(ctx, deployment); err != nil {
		t.Fatalf("failed to create external Deployment: %v", err)
	}
	if err := r.Get(ctx, deployment.Name, ns, deployment); err != nil {
		t.Fatalf("failed to read external Deployment: %v", err)
	}
	return deployment
}

func createBYOService(ctx context.Context, t *testing.T, r *resources.Resources, ns string, selector map[string]string) *corev1.Service {
	t.Helper()
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "external-mcp-service", Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{
				{Name: "other", Port: 9090, TargetPort: intstr.FromInt32(9090)},
				{Name: "mcp", Port: 80, TargetPort: intstr.FromInt32(8080)},
			},
		},
	}
	if err := r.Create(ctx, service); err != nil {
		t.Fatalf("failed to create external Service: %v", err)
	}
	if err := r.Get(ctx, service.Name, ns, service); err != nil {
		t.Fatalf("failed to read external Service: %v", err)
	}
	return service
}

func waitForBYORoute(ctx context.Context, t *testing.T, r *resources.Resources, name, ns, service string, port int32) {
	t.Helper()
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	err := wait.For(conditions.New(r).ResourceMatch(route, func(obj k8s.Object) bool {
		current := obj.(*gatewayv1.HTTPRoute)
		if len(current.Spec.Rules) != 1 || len(current.Spec.Rules[0].BackendRefs) != 1 {
			return false
		}
		backend := current.Spec.Rules[0].BackendRefs[0]
		if string(backend.Name) != service || backend.Port == nil || *backend.Port != port {
			return false
		}
		for _, parent := range current.Status.Parents {
			accepted := meta.FindStatusCondition(parent.Conditions, string(gatewayv1.RouteConditionAccepted))
			resolved := meta.FindStatusCondition(parent.Conditions, string(gatewayv1.RouteConditionResolvedRefs))
			if accepted != nil && accepted.Status == metav1.ConditionTrue && accepted.ObservedGeneration == current.Generation &&
				resolved != nil && resolved.Status == metav1.ConditionTrue && resolved.ObservedGeneration == current.Generation {
				return true
			}
		}
		return false
	}), wait.WithContext(ctx), wait.WithTimeout(3*time.Minute), wait.WithInterval(2*time.Second))
	if err != nil {
		t.Fatalf("HTTPRoute %s/%s did not become ready with backend %s:%d: %v", ns, name, service, port, err)
	}
}

func assertBYOResourcesUnchanged(ctx context.Context, t *testing.T, r *resources.Resources, deployment *appsv1.Deployment, service *corev1.Service) {
	t.Helper()
	if deployment != nil {
		current := &appsv1.Deployment{}
		if err := r.Get(ctx, deployment.Name, deployment.Namespace, current); err != nil {
			t.Fatalf("external Deployment is missing: %v", err)
		}
		if current.UID != deployment.UID || len(current.OwnerReferences) != 0 {
			t.Fatal("external Deployment was replaced or adopted")
		}
		if diff := cmp.Diff(deployment.Spec, current.Spec); diff != "" {
			t.Fatalf("external Deployment spec changed (-before +after):\n%s", diff)
		}
	}
	if service != nil {
		current := &corev1.Service{}
		if err := r.Get(ctx, service.Name, service.Namespace, current); err != nil {
			t.Fatalf("external Service is missing: %v", err)
		}
		if current.UID != service.UID || len(current.OwnerReferences) != 0 {
			t.Fatal("external Service was replaced or adopted")
		}
		if diff := cmp.Diff(service.Spec, current.Spec); diff != "" {
			t.Fatalf("external Service spec changed (-before +after):\n%s", diff)
		}
	}
}
