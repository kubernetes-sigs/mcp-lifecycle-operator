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
	"crypto/x509"
	"fmt"
	"net"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1alpha1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1alpha1"
)

// ValidationError represents a permanent configuration validation error.
// These errors indicate the MCPServer configuration is invalid and should not be retried.
type ValidationError struct {
	Reason  string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// validateConfig validates the MCPServer configuration.
// Returns ValidationError for permanent configuration errors, wrapped error for transient errors, or nil for success.
func (r *MCPServerReconciler) validateConfig(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
) error {
	// Validate storage mounts
	for i, storage := range mcpServer.Spec.Config.Storage {
		if err := r.validateStorageMount(ctx, mcpServer, storage, i); err != nil {
			return err
		}
	}

	// Validate envFrom references
	for i, envFrom := range mcpServer.Spec.Config.EnvFrom {
		if err := r.validateEnvFrom(ctx, mcpServer, envFrom, i); err != nil {
			return err
		}
	}

	// Validate env valueFrom references
	for i, env := range mcpServer.Spec.Config.Env {
		if err := r.validateEnvValueFrom(ctx, mcpServer, env, i); err != nil {
			return err
		}
	}

	// Validate network ingressFrom peers
	if mcpServer.Spec.Network != nil {
		for i, peer := range mcpServer.Spec.Network.IngressFrom {
			if err := validateIngressFromPeer(peer, i); err != nil {
				return err
			}
		}
	}

	// Validate TLS configuration
	if mcpServer.Spec.Transport != nil &&
		mcpServer.Spec.Transport.TLS != nil &&
		mcpServer.Spec.Transport.TLS.Enabled {
		tlsCfg := mcpServer.Spec.Transport.TLS
		if tlsCfg.InsecureSkipVerify && tlsCfg.CABundleSecret != nil {
			return &ValidationError{
				Reason:  ReasonInvalid,
				Message: "insecureSkipVerify and caBundleSecret are mutually exclusive",
			}
		}
		if tlsCfg.CABundleSecret != nil {
			if err := r.validateCABundleSecret(
				ctx,
				mcpServer.Namespace,
				tlsCfg.CABundleSecret.Name,
			); err != nil {
				return err
			}
		}
	}

	// All validation passed
	return nil
}

// validateReferencedConfigMap returns a permanent ValidationError on NotFound/BadRequest,
// or a wrapped transient error for other API failures.
func (r *MCPServerReconciler) validateReferencedConfigMap(
	ctx context.Context,
	namespace, name, resourceDesc string,
) error {
	cm := &corev1.ConfigMap{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, cm); err != nil {
		return classifyAPIError(resourceDesc, namespace, err)
	}
	return nil
}

// validateReferencedSecret returns a permanent ValidationError on NotFound/BadRequest,
// or a wrapped transient error for other API failures.
func (r *MCPServerReconciler) validateReferencedSecret(
	ctx context.Context,
	namespace, name, resourceDesc string,
) error {
	secret := &corev1.Secret{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, secret); err != nil {
		return classifyAPIError(resourceDesc, namespace, err)
	}
	return nil
}

// validateCABundleSecret validates that the referenced Secret exists, contains
// the "ca.crt" key, and that the value is parseable PEM. This catches permanent
// config errors at validation time instead of burning handshake retries.
func (r *MCPServerReconciler) validateCABundleSecret(
	ctx context.Context,
	namespace, name string,
) error {
	secret := &corev1.Secret{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, secret); err != nil {
		return classifyAPIError("TLS CA bundle Secret", namespace, err)
	}
	caPEM, ok := secret.Data[caBundleKey]
	if !ok {
		return &ValidationError{
			Reason:  ReasonInvalid,
			Message: fmt.Sprintf("TLS CA bundle Secret %q does not contain key %q", name, caBundleKey),
		}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return &ValidationError{
			Reason:  ReasonInvalid,
			Message: fmt.Sprintf("TLS CA bundle Secret %q key %q contains no valid PEM certificates", name, caBundleKey),
		}
	}
	return nil
}

// validateStorageMount validates a single storage mount configuration.
// Returns ValidationError for permanent configuration errors, wrapped error for transient errors, or nil for success.
func (r *MCPServerReconciler) validateStorageMount(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	storage mcpv1alpha1.StorageMount,
	index int,
) error {
	switch storage.Source.Type {
	case mcpv1alpha1.StorageTypeConfigMap:
		if storage.Source.ConfigMap == nil {
			return &ValidationError{
				Reason:  ReasonInvalid,
				Message: fmt.Sprintf("ConfigMap must be set for storage mount at index %d", index),
			}
		}
		if storage.Source.ConfigMap.Name == "" {
			return &ValidationError{
				Reason:  ReasonInvalid,
				Message: fmt.Sprintf("ConfigMap name must not be empty for storage mount at index %d", index),
			}
		}
		// Skip validation if optional
		if storage.Source.ConfigMap.Optional != nil && *storage.Source.ConfigMap.Optional {
			return nil
		}
		return r.validateReferencedConfigMap(ctx, mcpServer.Namespace, storage.Source.ConfigMap.Name,
			fmt.Sprintf("ConfigMap '%s'", storage.Source.ConfigMap.Name))

	case mcpv1alpha1.StorageTypeSecret:
		if storage.Source.Secret == nil {
			return &ValidationError{
				Reason:  ReasonInvalid,
				Message: fmt.Sprintf("Secret must be set for storage mount at index %d", index),
			}
		}
		if storage.Source.Secret.SecretName == "" {
			return &ValidationError{
				Reason:  ReasonInvalid,
				Message: fmt.Sprintf("Secret name must not be empty for storage mount at index %d", index),
			}
		}
		// Skip validation if optional
		if storage.Source.Secret.Optional != nil && *storage.Source.Secret.Optional {
			return nil
		}
		return r.validateReferencedSecret(ctx, mcpServer.Namespace, storage.Source.Secret.SecretName,
			fmt.Sprintf("Secret '%s'", storage.Source.Secret.SecretName))

	case mcpv1alpha1.StorageTypeEmptyDir:
		// Validate EmptyDir configuration is present
		if storage.Source.EmptyDir == nil {
			return &ValidationError{
				Reason:  ReasonInvalid,
				Message: fmt.Sprintf("EmptyDir must be set for storage mount at index %d", index),
			}
		}

	default:
		// Unknown/unsupported storage type
		return &ValidationError{
			Reason:  ReasonInvalid,
			Message: fmt.Sprintf("Unsupported storage type '%s' at index %d", storage.Source.Type, index),
		}
	}
	return nil
}

// validateEnvFrom validates a single envFrom configuration.
// Returns ValidationError for permanent configuration errors, wrapped error for transient errors, or nil for success.
func (r *MCPServerReconciler) validateEnvFrom(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	envFrom corev1.EnvFromSource,
	index int,
) error {
	if ref := envFrom.ConfigMapRef; ref != nil {
		if ref.Optional == nil || !*ref.Optional {
			if err := r.validateReferencedConfigMap(ctx, mcpServer.Namespace, ref.Name,
				fmt.Sprintf("ConfigMap '%s' (envFrom index %d)", ref.Name, index)); err != nil {
				return err
			}
		}
	}
	if ref := envFrom.SecretRef; ref != nil {
		if ref.Optional == nil || !*ref.Optional {
			if err := r.validateReferencedSecret(ctx, mcpServer.Namespace, ref.Name,
				fmt.Sprintf("Secret '%s' (envFrom index %d)", ref.Name, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateEnvValueFrom validates a single env var's valueFrom configuration.
// Returns ValidationError for permanent configuration errors, wrapped error for transient errors, or nil for success.
func (r *MCPServerReconciler) validateEnvValueFrom(
	ctx context.Context,
	mcpServer *mcpv1alpha1.MCPServer,
	env corev1.EnvVar,
	index int,
) error {
	if env.ValueFrom == nil {
		return nil
	}
	if ref := env.ValueFrom.ConfigMapKeyRef; ref != nil {
		if ref.Optional == nil || !*ref.Optional {
			if err := r.validateReferencedConfigMap(ctx, mcpServer.Namespace, ref.Name,
				fmt.Sprintf("ConfigMap '%s' referenced by env var '%s' (env index %d)", ref.Name, env.Name, index)); err != nil {
				return err
			}
		}
	}
	if ref := env.ValueFrom.SecretKeyRef; ref != nil {
		if ref.Optional == nil || !*ref.Optional {
			if err := r.validateReferencedSecret(ctx, mcpServer.Namespace, ref.Name,
				fmt.Sprintf("Secret '%s' referenced by env var '%s' (env index %d)", ref.Name, env.Name, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

// classifyAPIError classifies a Kubernetes API error as either a permanent ValidationError
// or a transient error that should be retried.
// NotFound and BadRequest are permanent — NotFound is safe to treat as permanent because the
// controller watches ConfigMaps/Secrets and will re-reconcile when the missing resource is created.
// All other errors (Forbidden, Unauthorized, 500, 503, 429, timeouts...) are transient.
func classifyAPIError(resourceDesc string, namespace string, err error) error {
	if apierrors.IsNotFound(err) {
		return &ValidationError{
			Reason:  ReasonInvalid,
			Message: fmt.Sprintf("%s not found in namespace '%s'", resourceDesc, namespace),
		}
	}
	if apierrors.IsBadRequest(err) {
		return &ValidationError{
			Reason:  ReasonInvalid,
			Message: fmt.Sprintf("Invalid %s reference: %v", resourceDesc, err),
		}
	}
	return fmt.Errorf("transient error validating %s: %w", resourceDesc, err)
}

// validateIngressFromPeer validates a single NetworkPolicyPeer entry.
func validateIngressFromPeer(peer networkingv1.NetworkPolicyPeer, index int) *ValidationError {
	if peer.IPBlock != nil {
		if peer.PodSelector != nil || peer.NamespaceSelector != nil {
			return &ValidationError{
				Reason:  ReasonInvalid,
				Message: fmt.Sprintf("network.ingressFrom[%d]: ipBlock cannot be combined with podSelector or namespaceSelector", index),
			}
		}
		if peer.IPBlock.CIDR == "" {
			return &ValidationError{
				Reason:  ReasonInvalid,
				Message: fmt.Sprintf("network.ingressFrom[%d]: ipBlock.cidr must not be empty", index),
			}
		}
		_, cidrNet, err := net.ParseCIDR(peer.IPBlock.CIDR)
		if err != nil {
			return &ValidationError{
				Reason:  ReasonInvalid,
				Message: fmt.Sprintf("network.ingressFrom[%d]: invalid ipBlock.cidr %q: %v", index, peer.IPBlock.CIDR, err),
			}
		}
		parentOnes, parentBits := cidrNet.Mask.Size()
		for j, except := range peer.IPBlock.Except {
			_, exceptNet, err := net.ParseCIDR(except)
			if err != nil {
				return &ValidationError{
					Reason:  ReasonInvalid,
					Message: fmt.Sprintf("network.ingressFrom[%d]: invalid ipBlock.except[%d] %q: %v", index, j, except, err),
				}
			}
			exceptOnes, exceptBits := exceptNet.Mask.Size()
			if parentBits != exceptBits || parentOnes > exceptOnes || !cidrNet.Contains(exceptNet.IP) {
				return &ValidationError{
					Reason:  ReasonInvalid,
					Message: fmt.Sprintf("network.ingressFrom[%d]: ipBlock.except[%d] %q is not within cidr %q", index, j, except, peer.IPBlock.CIDR),
				}
			}
		}
	}
	return nil
}
