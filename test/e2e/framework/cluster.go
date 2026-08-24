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
	"log"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// ArchARM64 is the value reported by node.Status.NodeInfo.Architecture for arm64 nodes.
const ArchARM64 = "arm64"

var (
	clusterArchOnce sync.Once
	clusterArchSet  map[string]bool
	clusterArchErr  error
)

// DetectClusterArchitectures lists cluster nodes and returns the set of
// distinct architectures reported via node.Status.NodeInfo.Architecture
// (e.g. "amd64", "arm64"). The node list is only fetched once per test
// binary run; subsequent calls return the cached result.
func DetectClusterArchitectures(ctx context.Context, cfg *envconf.Config) (map[string]bool, error) {
	clusterArchOnce.Do(func() {
		var nodes corev1.NodeList
		if err := cfg.Client().Resources().List(ctx, &nodes); err != nil {
			clusterArchErr = fmt.Errorf("listing nodes: %w", err)
			return
		}
		archs := make(map[string]bool, len(nodes.Items))
		for _, n := range nodes.Items {
			if a := n.Status.NodeInfo.Architecture; a != "" {
				archs[a] = true
			}
		}
		clusterArchSet = archs
	})
	return clusterArchSet, clusterArchErr
}

// ClusterHasARM64 reports whether the cluster has any arm64 nodes.
func ClusterHasARM64(ctx context.Context, cfg *envconf.Config) (bool, error) {
	archs, err := DetectClusterArchitectures(ctx, cfg)
	if err != nil {
		return false, err
	}
	return archs[ArchARM64], nil
}

// ClusterPredicate evaluates a condition against the target cluster.
type ClusterPredicate func(ctx context.Context, cfg *envconf.Config) (bool, error)

// imageSkipPredicates maps a container image ref to the ClusterPredicate that
// determines whether tests using that image must be skipped on the target
// cluster. All current test images publish multi-arch manifests, so this
// map is empty.
var imageSkipPredicates = map[string]ClusterPredicate{}

// imageUnsupported reports whether imageRef is unsupported on the target
// cluster per imageSkipPredicates. Returns false for unregistered images.
func imageUnsupported(ctx context.Context, cfg *envconf.Config, imageRef string) (bool, error) {
	predicate, ok := imageSkipPredicates[imageRef]
	if !ok {
		return false, nil
	}
	return predicate(ctx, cfg)
}

// SkipIfImageUnsupported skips the current test when imageRef is registered
// in imageSkipPredicates and its predicate matches the target cluster.
func SkipIfImageUnsupported(ctx context.Context, t *testing.T, cfg *envconf.Config, imageRef string) {
	t.Helper()
	skip, err := imageUnsupported(ctx, cfg, imageRef)
	if err != nil {
		t.Fatalf("failed to evaluate skip predicate for image %s: %v", imageRef, err)
	}
	if skip {
		t.Skipf("skipping: image %s is unsupported on this cluster (kubernetes-sigs/mcp-lifecycle-operator#326)", imageRef)
	}
}

// FilterSupportedImages returns imageRefs minus any image whose registered
// ClusterPredicate matches the target cluster (see imageSkipPredicates).
func FilterSupportedImages(ctx context.Context, cfg *envconf.Config, imageRefs ...string) ([]string, error) {
	supported := make([]string, 0, len(imageRefs))
	for _, ref := range imageRefs {
		skip, err := imageUnsupported(ctx, cfg, ref)
		if err != nil {
			return nil, fmt.Errorf("evaluating skip predicate for image %s: %w", ref, err)
		}
		if skip {
			log.Printf("skipping prewarm of %s: unsupported on this cluster", ref)
			continue
		}
		supported = append(supported, ref)
	}
	return supported, nil
}
