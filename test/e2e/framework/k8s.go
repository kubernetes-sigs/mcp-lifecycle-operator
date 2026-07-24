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

package framework

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	netpath "path"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// Clientset returns a kubernetes.Clientset from the e2e-framework config.
func Clientset(t *testing.T, cfg *envconf.Config) *kubernetes.Clientset {
	t.Helper()
	cs, err := kubernetes.NewForConfig(cfg.Client().Resources().GetConfig())
	if err != nil {
		t.Fatalf("failed to create clientset: %v", err)
	}
	return cs
}

// FindPodByLabel polls until a Running pod matching the label selector is found in the given namespace.
// An optional timeout can be provided; defaults to 3 minutes.
func FindPodByLabel(ctx context.Context, t *testing.T, cfg *envconf.Config,
	namespace, labelSelector string, timeout ...time.Duration) *corev1.Pod {
	t.Helper()
	d := 3 * time.Minute
	if len(timeout) > 0 {
		d = timeout[0]
	}
	r := cfg.Client().Resources(namespace)
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			t.Fatalf("context cancelled waiting for pod with selector %q: %v", labelSelector, ctx.Err())
		}
		var pods corev1.PodList
		if err := r.List(ctx, &pods, resources.WithLabelSelector(labelSelector)); err != nil {
			t.Fatalf("failed to list pods with selector %q: %v", labelSelector, err)
		}
		for i := range pods.Items {
			if pods.Items[i].Status.Phase == corev1.PodRunning && pods.Items[i].DeletionTimestamp == nil {
				return &pods.Items[i]
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for pod with selector %q: %v", labelSelector, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	t.Fatalf("timed out waiting for a Running pod with selector %q in namespace %s", labelSelector, namespace)
	return nil
}

// PodLogs returns the log output of a pod's first container.
func PodLogs(ctx context.Context, t *testing.T, cfg *envconf.Config,
	podName, namespace string) string {
	t.Helper()
	cs := Clientset(t, cfg)
	stream, err := cs.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{}).Stream(ctx)
	if err != nil {
		t.Fatalf("failed to get logs for pod %s/%s: %v", namespace, podName, err)
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("failed to read logs for pod %s/%s: %v", namespace, podName, err)
	}
	return string(data)
}

// WaitForPodPhase waits until a pod reaches the given phase.
// An optional timeout can be provided; defaults to 3 minutes.
func WaitForPodPhase(ctx context.Context, t *testing.T, cfg *envconf.Config,
	pod *corev1.Pod, phase corev1.PodPhase, timeout ...time.Duration) {
	t.Helper()
	d := 3 * time.Minute
	if len(timeout) > 0 {
		d = timeout[0]
	}
	r := cfg.Client().Resources(pod.Namespace)
	err := wait.For(
		conditions.New(r).ResourceMatch(pod, func(obj k8s.Object) bool {
			p := obj.(*corev1.Pod)
			return p.Status.Phase == phase
		}),
		wait.WithTimeout(d),
		wait.WithInterval(2*time.Second),
	)
	if err != nil {
		t.Fatalf("pod %s/%s: timed out waiting for phase %s: %v",
			pod.Namespace, pod.Name, phase, err)
	}
}

// ProxyOption configures optional parameters for ServiceProxyHTTPClient.
type ProxyOption func(*proxyOptions)

type proxyOptions struct {
	scheme string
}

// WithScheme sets the service scheme used in the API server proxy URL.
// Defaults to "http".
func WithScheme(scheme string) ProxyOption {
	return func(o *proxyOptions) {
		o.scheme = scheme
	}
}

// ServiceProxyHTTPClient returns an *http.Client and the full proxy URL for accessing
// a ClusterIP service via the Kubernetes API server proxy.
// The returned client is authenticated for the API server; the URL routes through
// /api/v1/namespaces/{ns}/services/{scheme}:{name}:{port}/proxy/{path}.
func ServiceProxyHTTPClient(t *testing.T, cfg *envconf.Config,
	namespace, serviceName string, port int, path string, opts ...ProxyOption) (*http.Client, string) {
	t.Helper()

	o := proxyOptions{scheme: "http"}
	for _, fn := range opts {
		fn(&o)
	}

	restCfg := cfg.Client().Resources().GetConfig()

	httpClient, err := rest.HTTPClientFor(restCfg)
	if err != nil {
		t.Fatalf("failed to create HTTP client from REST config: %v", err)
	}

	base, err := url.Parse(restCfg.Host)
	if err != nil {
		t.Fatalf("failed to parse REST config host %q: %v", restCfg.Host, err)
	}
	base.Path = netpath.Join(base.Path,
		fmt.Sprintf("api/v1/namespaces/%s/services/%s:%s:%d/proxy", namespace, o.scheme, serviceName, port),
		netpath.Clean("/"+path),
	)
	return httpClient, base.String()
}

// PrewarmImages returns an env.Func that pulls the given images on every node
// before tests start. Uses a DaemonSet per image so multi-node clusters are
// fully warmed. This prevents parallel tests from thundering-herd the registry
// with concurrent pulls of the same image.
func PrewarmImages(images ...string) env.Func {
	const prewarmNs = "e2e-prewarm"
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		r := cfg.Client().Resources()

		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: prewarmNs}}
		if err := r.Delete(ctx, nsObj); err == nil {
			_ = wait.For(
				conditions.New(r).ResourceDeleted(nsObj),
				wait.WithTimeout(1*time.Minute),
				wait.WithContext(ctx),
			)
		}
		if err := r.Create(ctx, nsObj); err != nil {
			return ctx, fmt.Errorf("creating prewarm namespace: %w", err)
		}
		defer cleanupPrewarmNamespace(ctx, r, nsObj)

		log.Printf("pre-warming %d images on all nodes...", len(images))
		for i, img := range images {
			ds := prewarmDaemonSet(fmt.Sprintf("prewarm-%d", i), prewarmNs, img)
			if err := r.Create(ctx, ds); err != nil {
				return ctx, fmt.Errorf("creating prewarm DaemonSet for %s: %w", img, err)
			}
		}

		for i, img := range images {
			ds := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("prewarm-%d", i),
					Namespace: prewarmNs,
				},
			}
			err := wait.For(
				conditions.New(r).ResourceMatch(ds, func(obj k8s.Object) bool {
					d := obj.(*appsv1.DaemonSet)
					return d.Status.DesiredNumberScheduled > 0 &&
						d.Status.NumberReady == d.Status.DesiredNumberScheduled
				}),
				wait.WithTimeout(5*time.Minute),
				wait.WithInterval(2*time.Second),
				wait.WithContext(ctx),
			)
			if err != nil {
				return ctx, fmt.Errorf("prewarm image %s: %w", img, err)
			}
			log.Printf("  pulled %s", img)
		}

		return ctx, nil
	}
}

func cleanupPrewarmNamespace(ctx context.Context, r *resources.Resources, nsObj *corev1.Namespace) {
	if err := r.Delete(ctx, nsObj); err != nil {
		log.Printf("failed to delete prewarm namespace: %v", err)
		return
	}
	if err := wait.For(
		conditions.New(r).ResourceDeleted(nsObj),
		wait.WithTimeout(1*time.Minute),
		wait.WithContext(ctx),
	); err != nil {
		log.Printf("timed out waiting for prewarm namespace deletion: %v", err)
	}
}

func prewarmDaemonSet(name, namespace, image string) *appsv1.DaemonSet {
	labels := map[string]string{"app": name}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "pull",
						Image:   image,
						Command: []string{"sleep", "infinity"},
					}},
				},
			},
		},
	}
}
