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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// listedBackendPod checks the current workload pods on each tool response. A
// rollout can have multiple ready pods, and a replacement can change their names
// between retries. Never accept terminating, unready, or unrelated pods.
func listedBackendPod(ctx context.Context, reader client.Reader, ns string, selector map[string]string, text string) (string, error) {
	if ns == "" || len(selector) == 0 {
		return "", fmt.Errorf("backend namespace and workload selector must be set")
	}
	pods := &corev1.PodList{}
	if err := reader.List(ctx, pods, client.InNamespace(ns), client.MatchingLabels(selector)); err != nil {
		return "", fmt.Errorf("listing backend pods: %w", err)
	}
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue && strings.Contains(text, pod.Name) {
				return pod.Name, nil
			}
		}
	}
	return "", fmt.Errorf("response did not contain a current ready backend pod: %s", text)
}
