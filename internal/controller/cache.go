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

package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CacheOptions returns the manager cache configuration.
//
// The controller watches Pods to surface container-level failure diagnostics
// (image pull failures, crash loops, OOM kills) on the Ready condition. Pods are
// the only object type where an unscoped informer would make memory scale with
// cluster size rather than with MCPServer count, so the informer is both:
//
//   - restricted server-side to pods this operator manages, via the presence of
//     LabelKeyMCPServer, and
//   - trimmed to the fields the diagnostics actually read.
//
// Tests should build their manager from this function rather than duplicating the
// configuration, otherwise they validate a copy instead of what production runs.
func CacheOptions() (cache.Options, error) {
	managedPods, err := labels.NewRequirement(LabelKeyMCPServer, selection.Exists, nil)
	if err != nil {
		return cache.Options{}, fmt.Errorf("building managed pod selector: %w", err)
	}

	return cache.Options{
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Pod{}: {
				Label:     labels.NewSelector().Add(*managedPods),
				Transform: stripPodForDiagnostics,
			},
		},
	}, nil
}

// stripPodForDiagnostics strips away all fields not needed for error diagnostics.
//
// This keeps the memory footprint of the pod cache small. Note this applies to
// every Pod read through the cache: anything that later needs Pod.Spec must widen
// this transform rather than assume the field is populated.
func stripPodForDiagnostics(obj any) (any, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return obj, nil
	}

	pod.Spec = corev1.PodSpec{}
	pod.ManagedFields = nil
	pod.Annotations = nil
	pod.Status = corev1.PodStatus{
		ContainerStatuses:     pod.Status.ContainerStatuses,
		InitContainerStatuses: pod.Status.InitContainerStatuses,
	}

	return pod, nil
}
