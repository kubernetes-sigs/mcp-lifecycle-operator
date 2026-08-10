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
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

const (
	DefaultNamespace          = "mcp-lifecycle-operator-system"
	DefaultServiceAccountName = "mcp-lifecycle-operator-controller-manager"
	DefaultMetricsServiceName = "mcp-lifecycle-operator-controller-manager-metrics-service"
)

func (r *Registry) ensureDefaults() {
	if _, ok := r.tiers[Explicit]; !ok {
		r.Register(Explicit, FromEnvVars)
	}
	if _, ok := r.tiers[Platform]; !ok {
		r.Register(Platform, FromPodLabels)
	}
	if _, ok := r.tiers[Fallback]; !ok {
		r.Register(Fallback, FromDefaults)
	}
}

func FromDefaults(_ context.Context, _ *envconf.Config) (OperatorRef, error) {
	return OperatorRef{
		Namespace:          DefaultNamespace,
		ServiceAccountName: DefaultServiceAccountName,
		MetricsServiceName: DefaultMetricsServiceName,
	}, nil
}

func FromEnvVars(_ context.Context, _ *envconf.Config) (OperatorRef, error) {
	return OperatorRef{
		Namespace:          os.Getenv("MCPLO_NAMESPACE"),
		ServiceAccountName: os.Getenv("MCPLO_SA_NAME"),
		MetricsServiceName: os.Getenv("MCPLO_METRICS_SERVICE"),
	}, nil
}

func FromPodLabels(ctx context.Context, cfg *envconf.Config) (OperatorRef, error) {
	if cfg == nil {
		return OperatorRef{}, nil
	}
	r := cfg.Client().Resources()
	var pods corev1.PodList
	if err := r.List(ctx, &pods,
		resources.WithLabelSelector("control-plane=controller-manager,app.kubernetes.io/name=mcp-lifecycle-operator"),
	); err != nil {
		return OperatorRef{}, fmt.Errorf("listing pods: %w", err)
	}
	var pod *corev1.Pod
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp == nil {
			pod = p
			break
		}
	}
	if pod == nil {
		return OperatorRef{}, nil
	}

	ref := OperatorRef{Namespace: pod.Namespace}
	ref.ServiceAccountName = pod.Spec.ServiceAccountName

	rNs := cfg.Client().Resources(pod.Namespace)
	var svcs corev1.ServiceList
	if err := rNs.List(ctx, &svcs); err != nil {
		return OperatorRef{}, fmt.Errorf("listing services in %s: %w", pod.Namespace, err)
	}
	for _, svc := range svcs.Items {
		if !strings.Contains(svc.Name, "metrics") {
			continue
		}
		if selectorMatchesLabels(svc.Spec.Selector, pod.Labels) {
			ref.MetricsServiceName = svc.Name
			break
		}
	}

	return ref, nil
}

func selectorMatchesLabels(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}
