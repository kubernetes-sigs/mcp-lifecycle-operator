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
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

func init() {
	RegisterLocator(&EnvVarLocator{})
	RegisterLocator(&PodLabelLocator{})
}

// EnvVarLocator checks MCPLO_NAMESPACE, MCPLO_SA_NAME, MCPLO_METRICS_SERVICE
// environment variables. Priority 5000: always wins when set.
type EnvVarLocator struct{}

func (d *EnvVarLocator) Priority() int { return 5000 }

func (d *EnvVarLocator) Namespace(_ context.Context, _ *envconf.Config) (string, bool) {
	ns := os.Getenv("MCPLO_NAMESPACE")
	return ns, ns != ""
}

func (d *EnvVarLocator) ServiceAccount(_ context.Context, _ *envconf.Config, _ string) (string, bool) {
	sa := os.Getenv("MCPLO_SA_NAME")
	return sa, sa != ""
}

func (d *EnvVarLocator) MetricsService(_ context.Context, _ *envconf.Config, _ string) (string, bool) {
	svc := os.Getenv("MCPLO_METRICS_SERVICE")
	return svc, svc != ""
}

// PodLabelLocator searches the cluster for the operator pod and derives
// namespace, service account, and metrics service from the running resources.
// Priority 50: generic fallback, runs after platform-specific locators.
type PodLabelLocator struct {
	pod *corev1.Pod
}

func (d *PodLabelLocator) Priority() int { return 50 }

func (d *PodLabelLocator) findPod(ctx context.Context, cfg *envconf.Config, namespace string) (*corev1.Pod, bool) {
	if d.pod != nil {
		return d.pod, true
	}
	if cfg == nil {
		return nil, false
	}
	var r *resources.Resources
	if namespace != "" {
		r = cfg.Client().Resources(namespace)
	} else {
		r = cfg.Client().Resources()
	}
	var pods corev1.PodList
	if err := r.List(ctx, &pods,
		resources.WithLabelSelector("control-plane=controller-manager,app.kubernetes.io/name=mcp-lifecycle-operator"),
	); err != nil {
		return nil, false
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp == nil {
			d.pod = p
			return p, true
		}
	}
	return nil, false
}

func (d *PodLabelLocator) Namespace(ctx context.Context, cfg *envconf.Config) (string, bool) {
	p, ok := d.findPod(ctx, cfg, "")
	if !ok {
		return "", false
	}
	return p.Namespace, true
}

func (d *PodLabelLocator) ServiceAccount(ctx context.Context, cfg *envconf.Config, namespace string) (string, bool) {
	p, ok := d.findPod(ctx, cfg, namespace)
	if !ok {
		return "", false
	}
	sa := p.Spec.ServiceAccountName
	if sa == "" {
		sa = "default"
	}
	return sa, true
}

func (d *PodLabelLocator) MetricsService(ctx context.Context, cfg *envconf.Config, namespace string) (string, bool) {
	p, ok := d.findPod(ctx, cfg, namespace)
	if !ok {
		return "", false
	}
	r := cfg.Client().Resources(namespace)
	var svcs corev1.ServiceList
	if err := r.List(ctx, &svcs); err != nil {
		return "", false
	}
	for _, svc := range svcs.Items {
		if !strings.Contains(svc.Name, "metrics") {
			continue
		}
		if selectorMatchesLabels(svc.Spec.Selector, p.Labels) {
			return svc.Name, true
		}
	}
	return "", false
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
