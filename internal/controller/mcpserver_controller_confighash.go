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
	"context"
	"crypto/sha256"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// applyConfigHash computes the config hash and sets it as a pod template
// annotation on the deployment. This is extracted from reconcileDeployment
// to keep cyclomatic complexity in check.
func (r *MCPServerReconciler) applyConfigHash(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	deployment *appsv1.Deployment,
) error {
	configHash, err := r.computeConfigHash(ctx, mcpServer)
	if err != nil {
		return err
	}
	if configHash != "" {
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}
		deployment.Spec.Template.Annotations[configHashAnnotation] = configHash
	}
	return nil
}

// computeConfigHash computes a SHA-256 hash of all ConfigMap and Secret data
// referenced by the MCPServer. This hash is placed in a pod template annotation
// so that changes to referenced resource data trigger a rolling update.
// Returns "" if no refs are listed or all referenced resources are not found.
func (r *MCPServerReconciler) computeConfigHash(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) (string, error) {
	configMapNames := extractConfigMapNames(mcpServer)
	secretNames := extractPodSecretNames(mcpServer)

	if len(configMapNames) == 0 && len(secretNames) == 0 {
		return "", nil
	}

	h := sha256.New()
	dataWritten := false

	// APIReader bypasses the cache to avoid starting a full structured informer
	// for ConfigMaps/Secrets, which would negate the metadata-only watches.
	sort.Strings(configMapNames)
	for _, name := range configMapNames {
		cm := &corev1.ConfigMap{}
		if err := r.APIReader.Get(ctx, client.ObjectKey{
			Name:      name,
			Namespace: mcpServer.Namespace,
		}, cm); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", err
		}
		dataWritten = true
		keys := make([]string, 0, len(cm.Data)+len(cm.BinaryData))
		for k := range cm.Data {
			keys = append(keys, k)
		}
		for k := range cm.BinaryData {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = fmt.Fprintf(h, "configmap/%s/%s=", name, k)
			if v, ok := cm.Data[k]; ok {
				_, _ = fmt.Fprint(h, v)
			} else {
				_, _ = h.Write(cm.BinaryData[k])
			}
			_, _ = h.Write([]byte{0})
		}
	}

	sort.Strings(secretNames)
	for _, name := range secretNames {
		secret := &corev1.Secret{}
		if err := r.APIReader.Get(ctx, client.ObjectKey{
			Name:      name,
			Namespace: mcpServer.Namespace,
		}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", err
		}
		dataWritten = true
		keys := make([]string, 0, len(secret.Data))
		for k := range secret.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = fmt.Fprintf(h, "secret/%s/%s=", name, k)
			_, _ = h.Write(secret.Data[k])
			_, _ = h.Write([]byte{0})
		}
	}

	if !dataWritten {
		return "", nil
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// findMCPServersForResource is a generic helper that finds all MCPServers
// referencing a given resource by name using the specified field index.
func (r *MCPServerReconciler) findMCPServersForResource(
	ctx context.Context,
	resourceName string,
	namespace string,
	indexKey string,
) []reconcile.Request {
	logger := log.FromContext(ctx)
	var mcpServers mcpv1alpha1.MCPServerList

	// Use the index to find MCPServers that reference this resource
	if err := r.List(ctx, &mcpServers,
		client.InNamespace(namespace),
		client.MatchingFields{indexKey: resourceName},
	); err != nil {
		logger.Error(err, "Failed to list MCPServers for resource",
			"resourceName", resourceName,
			keyNamespace, namespace,
			"indexKey", indexKey)
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0, len(mcpServers.Items))
	for _, mcpServer := range mcpServers.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&mcpServer),
		})
	}
	return requests
}

// findMCPServersForConfigMap finds all MCPServers that reference the given ConfigMap
// using the field index for efficient lookup.
func (r *MCPServerReconciler) findMCPServersForConfigMap(ctx context.Context, configMap client.Object) []reconcile.Request {
	return r.findMCPServersForResource(ctx, configMap.GetName(), configMap.GetNamespace(), configMapIndexKey)
}

// findMCPServersForSecret finds all MCPServers that reference the given Secret
// using the field index for efficient lookup.
func (r *MCPServerReconciler) findMCPServersForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	return r.findMCPServersForResource(ctx, secret.GetName(), secret.GetNamespace(), secretIndexKey)
}

// findMCPServersForPVC finds all MCPServers that reference the given PersistentVolumeClaim
// using the field index for efficient lookup.
func (r *MCPServerReconciler) findMCPServersForPVC(ctx context.Context, pvc client.Object) []reconcile.Request {
	return r.findMCPServersForResource(ctx, pvc.GetName(), pvc.GetNamespace(), pvcIndexKey)
}

func (r *MCPServerReconciler) findMCPServersForPod(ctx context.Context, pod client.Object) []reconcile.Request {
	name, ok := pod.GetLabels()[LabelKeyMCPServer]
	if !ok || name == "" {
		return nil
	}

	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: name, Namespace: pod.GetNamespace()},
	}}
}

// extractConfigMapNames is an index extractor that returns all ConfigMap names
// referenced by an MCPServer. Used for efficient ConfigMap watch lookups.
// This returns both required and optional ConfigMap references, matching Kubernetes
// semantics where optional resources are still used when available.
func extractConfigMapNames(obj client.Object) []string {
	mcpServer := obj.(*mcpv1alpha1.MCPServer)
	var configMaps []string
	seen := make(map[string]bool)

	// Extract from storage mounts
	for _, storage := range mcpServer.Spec.Config.Storage {
		if storage.Source.Type == mcpv1alpha1.StorageTypeConfigMap &&
			storage.Source.ConfigMap != nil {
			name := storage.Source.ConfigMap.Name
			if !seen[name] {
				configMaps = append(configMaps, name)
				seen[name] = true
			}
		}
	}

	// Extract from envFrom
	for _, envFrom := range mcpServer.Spec.Config.EnvFrom {
		if envFrom.ConfigMapRef != nil {
			name := envFrom.ConfigMapRef.Name
			if !seen[name] {
				configMaps = append(configMaps, name)
				seen[name] = true
			}
		}
	}

	// Extract from env valueFrom
	for _, env := range mcpServer.Spec.Config.Env {
		if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
			name := env.ValueFrom.ConfigMapKeyRef.Name
			if !seen[name] {
				configMaps = append(configMaps, name)
				seen[name] = true
			}
		}
	}

	return configMaps
}

// extractPVCNames returns all PersistentVolumeClaim names referenced by an MCPServer.
// Used as the field index extractor for PVC watch lookups.
func extractPVCNames(obj client.Object) []string {
	mcpServer := obj.(*mcpv1alpha1.MCPServer)
	var pvcs []string
	seen := make(map[string]bool)

	for _, storage := range mcpServer.Spec.Config.Storage {
		if storage.Source.Type == mcpv1alpha1.StorageTypePersistentVolumeClaim &&
			storage.Source.PersistentVolumeClaim != nil {
			name := storage.Source.PersistentVolumeClaim.ClaimName
			if !seen[name] {
				pvcs = append(pvcs, name)
				seen[name] = true
			}
		}
	}

	return pvcs
}

// extractSecretNames returns all Secret names referenced by an MCPServer,
// including pod-mounted secrets and the TLS CA bundle. Used as the field
// index extractor for Secret watch lookups.
func extractSecretNames(obj client.Object) []string {
	secrets := extractPodSecretNames(obj)
	seen := make(map[string]bool, len(secrets))
	for _, s := range secrets {
		seen[s] = true
	}

	mcpServer := obj.(*mcpv1alpha1.MCPServer)
	if mcpServer.Spec.Transport != nil &&
		mcpServer.Spec.Transport.TLS != nil &&
		mcpServer.Spec.Transport.TLS.CABundleSecret != nil {
		name := mcpServer.Spec.Transport.TLS.CABundleSecret.Name
		if !seen[name] {
			secrets = append(secrets, name)
		}
	}

	return secrets
}

// extractPodSecretNames returns Secret names that the pod actually mounts
// or references via env. Used for the deployment config hash so that CA
// bundle rotation (an operator-only concern) does not roll pods.
func extractPodSecretNames(obj client.Object) []string {
	mcpServer := obj.(*mcpv1alpha1.MCPServer)
	var secrets []string
	seen := make(map[string]bool)

	for _, storage := range mcpServer.Spec.Config.Storage {
		if storage.Source.Type == mcpv1alpha1.StorageTypeSecret &&
			storage.Source.Secret != nil {
			name := storage.Source.Secret.SecretName
			if !seen[name] {
				secrets = append(secrets, name)
				seen[name] = true
			}
		}
	}

	for _, envFrom := range mcpServer.Spec.Config.EnvFrom {
		if envFrom.SecretRef != nil {
			name := envFrom.SecretRef.Name
			if !seen[name] {
				secrets = append(secrets, name)
				seen[name] = true
			}
		}
	}

	for _, env := range mcpServer.Spec.Config.Env {
		if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			name := env.ValueFrom.SecretKeyRef.Name
			if !seen[name] {
				secrets = append(secrets, name)
				seen[name] = true
			}
		}
	}

	return secrets
}
