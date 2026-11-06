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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpcontroller "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller"
)

func probeTestPod(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace,
			Labels: map[string]string{mcpcontroller.LabelKeyMCPServer: "backend"},
		},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

func probeTestClient(t *testing.T, pods ...*corev1.Pod) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	objects := make([]client.Object, 0, len(pods))
	for _, pod := range pods {
		objects = append(objects, pod)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestListedBackendPodDuringRollout(t *testing.T) {
	oldPod := probeTestPod("backend-old", "test")
	newPod := probeTestPod("backend-new", "test")
	terminating := probeTestPod("backend-terminating", "test")
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"test.example/hold"}
	unready := probeTestPod("backend-unready", "test")
	unready.Status.Conditions[0].Status = corev1.ConditionFalse
	pending := probeTestPod("backend-pending", "test")
	pending.Status.Phase = corev1.PodPending
	unrelated := probeTestPod("unrelated", "test")
	unrelated.Labels[mcpcontroller.LabelKeyMCPServer] = "other"
	foreign := probeTestPod("foreign", "other-namespace")
	reader := probeTestClient(t, oldPod, newPod, terminating, unready, pending, unrelated, foreign)
	selector := map[string]string{mcpcontroller.LabelKeyMCPServer: "backend"}

	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{name: "old ready pod during overlap", text: oldPod.Name, want: oldPod.Name},
		{name: "new ready pod during overlap", text: newPod.Name, want: newPod.Name},
		{name: "terminating pod", text: terminating.Name},
		{name: "unready pod", text: unready.Name},
		{name: "pending pod", text: pending.Name},
		{name: "different workload", text: unrelated.Name},
		{name: "different namespace", text: foreign.Name},
		{name: "no matching response", text: "no pods found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := listedBackendPod(t.Context(), reader, "test", selector, tc.text)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("listedBackendPod() = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestListedBackendPodRefreshesAfterReplacement(t *testing.T) {
	ctx := context.Background()
	oldPod := probeTestPod("original", "test")
	reader := probeTestClient(t, oldPod)
	selector := map[string]string{mcpcontroller.LabelKeyMCPServer: "backend"}
	if _, err := listedBackendPod(ctx, reader, "test", selector, oldPod.Name); err != nil {
		t.Fatal(err)
	}
	if err := reader.Delete(ctx, oldPod); err != nil {
		t.Fatal(err)
	}
	// A retry during the gap should keep waiting rather than fail the test.
	if _, err := listedBackendPod(ctx, reader, "test", selector, oldPod.Name); err == nil {
		t.Fatal("accepted a deleted pod during the replacement gap")
	}
	newPod := probeTestPod("replacement", "test")
	if err := reader.Create(ctx, newPod); err != nil {
		t.Fatal(err)
	}
	if _, err := listedBackendPod(ctx, reader, "test", selector, oldPod.Name); err == nil {
		t.Fatal("accepted a stale response after pod replacement")
	}
	got, err := listedBackendPod(ctx, reader, "test", selector, newPod.Name)
	if err != nil || got != newPod.Name {
		t.Fatalf("replacement probe = %q, %v; want %q", got, err, newPod.Name)
	}
}

func TestListedBackendPodRequiresScope(t *testing.T) {
	reader := probeTestClient(t, probeTestPod("backend", "test"))
	for _, tc := range []struct {
		ns       string
		selector map[string]string
	}{
		{ns: "test"},
		{selector: map[string]string{mcpcontroller.LabelKeyMCPServer: "backend"}},
	} {
		if _, err := listedBackendPod(t.Context(), reader, tc.ns, tc.selector, "backend"); err == nil {
			t.Fatal("accepted an unscoped pod lookup")
		}
	}
}
