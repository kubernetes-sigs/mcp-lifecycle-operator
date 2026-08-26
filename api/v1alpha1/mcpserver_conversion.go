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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	v1beta1 "github.com/kubernetes-sigs/mcp-lifecycle-operator/api/v1beta1"
)

var _ conversion.Convertible = &MCPServer{}

// ConvertTo converts this MCPServer (v1alpha1) to the Hub version (v1beta1).
//
//nolint:dupl // ConvertTo/ConvertFrom are symmetric by design
func (src *MCPServer) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta1.MCPServer)

	dst.ObjectMeta = src.ObjectMeta

	// Spec - direct field copy (schemas are identical)
	dst.Spec.ExtraLabels = src.Spec.ExtraLabels
	dst.Spec.ExtraAnnotations = src.Spec.ExtraAnnotations
	dst.Spec.Source = convertSourceTo(src.Spec.Source)
	dst.Spec.Config = convertServerConfigTo(src.Spec.Config)
	dst.Spec.Runtime = convertRuntimeConfigTo(src.Spec.Runtime)
	dst.Spec.MCP = convertMCPConfigTo(src.Spec.MCP)
	dst.Spec.Network = convertNetworkConfigTo(src.Spec.Network)
	dst.Spec.Transport = convertTransportConfigTo(src.Spec.Transport)

	// Status
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	dst.Status.DeploymentName = src.Status.DeploymentName
	dst.Status.ServiceName = src.Status.ServiceName
	dst.Status.Address = convertAddressTo(src.Status.Address)
	dst.Status.ServerInfo = convertServerInfoTo(src.Status.ServerInfo)
	dst.Status.HandshakeRetryCount = src.Status.HandshakeRetryCount //nolint:staticcheck // deprecated field must be preserved for round-trip conversion
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.ReadyReplicas = src.Status.ReadyReplicas
	dst.Status.Conditions = convertConditionsTo(src.Status.Conditions)

	return nil
}

// ConvertFrom converts from the Hub version (v1beta1) to this version (v1alpha1).
//
//nolint:dupl // ConvertTo/ConvertFrom are symmetric by design
func (dst *MCPServer) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta1.MCPServer)

	dst.ObjectMeta = src.ObjectMeta

	// Spec - direct field copy (schemas are identical)
	dst.Spec.ExtraLabels = src.Spec.ExtraLabels
	dst.Spec.ExtraAnnotations = src.Spec.ExtraAnnotations
	dst.Spec.Source = convertSourceFrom(src.Spec.Source)
	dst.Spec.Config = convertServerConfigFrom(src.Spec.Config)
	dst.Spec.Runtime = convertRuntimeConfigFrom(src.Spec.Runtime)
	dst.Spec.MCP = convertMCPConfigFrom(src.Spec.MCP)
	dst.Spec.Network = convertNetworkConfigFrom(src.Spec.Network)
	dst.Spec.Transport = convertTransportConfigFrom(src.Spec.Transport)

	// Status
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	dst.Status.DeploymentName = src.Status.DeploymentName
	dst.Status.ServiceName = src.Status.ServiceName
	dst.Status.Address = convertAddressFrom(src.Status.Address)
	dst.Status.ServerInfo = convertServerInfoFrom(src.Status.ServerInfo)
	dst.Status.HandshakeRetryCount = src.Status.HandshakeRetryCount //nolint:staticcheck // deprecated field must be preserved for round-trip conversion
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.ReadyReplicas = src.Status.ReadyReplicas
	dst.Status.Conditions = convertConditionsFrom(src.Status.Conditions)

	return nil
}

func convertSourceTo(in Source) v1beta1.Source {
	out := v1beta1.Source{
		Type: v1beta1.SourceType(in.Type),
	}
	if in.ContainerImage != nil {
		out.ContainerImage = &v1beta1.ContainerImageSource{
			Ref:              in.ContainerImage.Ref,
			PullPolicy:       in.ContainerImage.PullPolicy,
			ImagePullSecrets: in.ContainerImage.ImagePullSecrets,
		}
	}
	return out
}

func convertSourceFrom(in v1beta1.Source) Source {
	out := Source{
		Type: SourceType(in.Type),
	}
	if in.ContainerImage != nil {
		out.ContainerImage = &ContainerImageSource{
			Ref:              in.ContainerImage.Ref,
			PullPolicy:       in.ContainerImage.PullPolicy,
			ImagePullSecrets: in.ContainerImage.ImagePullSecrets,
		}
	}
	return out
}

func convertServerConfigTo(in ServerConfig) v1beta1.ServerConfig {
	out := v1beta1.ServerConfig{
		Port:      in.Port,
		Arguments: in.Arguments,
		Env:       in.Env,
		EnvFrom:   in.EnvFrom,
		Path:      in.Path,
	}
	for _, s := range in.Storage {
		out.Storage = append(out.Storage, convertStorageMountTo(s))
	}
	return out
}

func convertServerConfigFrom(in v1beta1.ServerConfig) ServerConfig {
	out := ServerConfig{
		Port:      in.Port,
		Arguments: in.Arguments,
		Env:       in.Env,
		EnvFrom:   in.EnvFrom,
		Path:      in.Path,
	}
	for _, s := range in.Storage {
		out.Storage = append(out.Storage, convertStorageMountFrom(s))
	}
	return out
}

func convertStorageMountTo(in StorageMount) v1beta1.StorageMount {
	return v1beta1.StorageMount{
		Path:        in.Path,
		Permissions: v1beta1.MountPermissions(in.Permissions),
		Source: v1beta1.StorageSource{
			Type:      v1beta1.StorageType(in.Source.Type),
			ConfigMap: in.Source.ConfigMap,
			Secret:    in.Source.Secret,
			EmptyDir:  in.Source.EmptyDir,
		},
	}
}

func convertStorageMountFrom(in v1beta1.StorageMount) StorageMount {
	return StorageMount{
		Path:        in.Path,
		Permissions: MountPermissions(in.Permissions),
		Source: StorageSource{
			Type:      StorageType(in.Source.Type),
			ConfigMap: in.Source.ConfigMap,
			Secret:    in.Source.Secret,
			EmptyDir:  in.Source.EmptyDir,
		},
	}
}

func convertRuntimeConfigTo(in RuntimeConfig) v1beta1.RuntimeConfig {
	return v1beta1.RuntimeConfig{
		Replicas:  in.Replicas,
		Security:  convertSecurityConfigTo(in.Security),
		Resources: in.Resources,
		Health:    convertHealthConfigTo(in.Health),
	}
}

func convertRuntimeConfigFrom(in v1beta1.RuntimeConfig) RuntimeConfig {
	return RuntimeConfig{
		Replicas:  in.Replicas,
		Security:  convertSecurityConfigFrom(in.Security),
		Resources: in.Resources,
		Health:    convertHealthConfigFrom(in.Health),
	}
}

func convertSecurityConfigTo(in SecurityConfig) v1beta1.SecurityConfig {
	return v1beta1.SecurityConfig{
		ServiceAccountName: in.ServiceAccountName,
		PodSecurityContext: in.PodSecurityContext,
		SecurityContext:    in.SecurityContext,
	}
}

func convertSecurityConfigFrom(in v1beta1.SecurityConfig) SecurityConfig {
	return SecurityConfig{
		ServiceAccountName: in.ServiceAccountName,
		PodSecurityContext: in.PodSecurityContext,
		SecurityContext:    in.SecurityContext,
	}
}

func convertHealthConfigTo(in HealthConfig) v1beta1.HealthConfig {
	return v1beta1.HealthConfig{
		LivenessProbe:  in.LivenessProbe,
		ReadinessProbe: in.ReadinessProbe,
	}
}

func convertHealthConfigFrom(in v1beta1.HealthConfig) HealthConfig {
	return HealthConfig{
		LivenessProbe:  in.LivenessProbe,
		ReadinessProbe: in.ReadinessProbe,
	}
}

func convertMCPConfigTo(in MCPConfig) v1beta1.MCPConfig {
	return v1beta1.MCPConfig{
		Stateless: in.Stateless,
	}
}

func convertMCPConfigFrom(in v1beta1.MCPConfig) MCPConfig {
	return MCPConfig{
		Stateless: in.Stateless,
	}
}

func convertNetworkConfigTo(in *NetworkConfig) *v1beta1.NetworkConfig {
	if in == nil {
		return nil
	}
	return &v1beta1.NetworkConfig{
		IngressFrom: in.IngressFrom,
		EgressTo:    in.EgressTo,
		EgressPorts: in.EgressPorts,
	}
}

func convertNetworkConfigFrom(in *v1beta1.NetworkConfig) *NetworkConfig {
	if in == nil {
		return nil
	}
	return &NetworkConfig{
		IngressFrom: in.IngressFrom,
		EgressTo:    in.EgressTo,
		EgressPorts: in.EgressPorts,
	}
}

func convertTransportConfigTo(in *TransportConfig) *v1beta1.TransportConfig {
	if in == nil {
		return nil
	}
	out := &v1beta1.TransportConfig{}
	if in.TLS != nil {
		out.TLS = &v1beta1.TLSClientConfig{
			Enabled:            in.TLS.Enabled,
			InsecureSkipVerify: in.TLS.InsecureSkipVerify,
		}
		if in.TLS.CABundleSecret != nil {
			out.TLS.CABundleSecret = &v1beta1.SecretReference{
				Name: in.TLS.CABundleSecret.Name,
			}
		}
	}
	return out
}

func convertTransportConfigFrom(in *v1beta1.TransportConfig) *TransportConfig {
	if in == nil {
		return nil
	}
	out := &TransportConfig{}
	if in.TLS != nil {
		out.TLS = &TLSClientConfig{
			Enabled:            in.TLS.Enabled,
			InsecureSkipVerify: in.TLS.InsecureSkipVerify,
		}
		if in.TLS.CABundleSecret != nil {
			out.TLS.CABundleSecret = &SecretReference{
				Name: in.TLS.CABundleSecret.Name,
			}
		}
	}
	return out
}

func convertAddressTo(in *MCPServerAddress) *v1beta1.MCPServerAddress {
	if in == nil {
		return nil
	}
	return &v1beta1.MCPServerAddress{
		URL: in.URL,
	}
}

func convertAddressFrom(in *v1beta1.MCPServerAddress) *MCPServerAddress {
	if in == nil {
		return nil
	}
	return &MCPServerAddress{
		URL: in.URL,
	}
}

func convertServerInfoTo(in *MCPServerInfo) *v1beta1.MCPServerInfo {
	if in == nil {
		return nil
	}
	out := &v1beta1.MCPServerInfo{
		Name:            in.Name,
		Version:         in.Version,
		ProtocolVersion: in.ProtocolVersion,
		Instructions:    in.Instructions,
	}
	if in.Capabilities != nil {
		out.Capabilities = &v1beta1.MCPServerCapabilities{
			Tools:       in.Capabilities.Tools,
			Resources:   in.Capabilities.Resources,
			Prompts:     in.Capabilities.Prompts,
			Logging:     in.Capabilities.Logging, //nolint:staticcheck // deprecated field must be preserved for round-trip conversion
			Completions: in.Capabilities.Completions,
		}
	}
	return out
}

func convertServerInfoFrom(in *v1beta1.MCPServerInfo) *MCPServerInfo {
	if in == nil {
		return nil
	}
	out := &MCPServerInfo{
		Name:            in.Name,
		Version:         in.Version,
		ProtocolVersion: in.ProtocolVersion,
		Instructions:    in.Instructions,
	}
	if in.Capabilities != nil {
		out.Capabilities = &MCPServerCapabilities{
			Tools:       in.Capabilities.Tools,
			Resources:   in.Capabilities.Resources,
			Prompts:     in.Capabilities.Prompts,
			Logging:     in.Capabilities.Logging, //nolint:staticcheck // deprecated field must be preserved for round-trip conversion
			Completions: in.Capabilities.Completions,
		}
	}
	return out
}

// Condition type constants for v1beta1.
const (
	conditionTypeAvailable = "Available"
	conditionTypeVerified  = "Verified"
	conditionTypeReady     = "Ready"
	conditionTypeAccepted  = "Accepted"
)

// v1alpha1 Ready reasons that map to v1beta1 Verified.
var handshakeReasons = map[string]bool{
	"MCPEndpointUnavailable": true,
}

// convertConditionsTo splits the v1alpha1 Ready condition into v1beta1
// Available + Verified conditions. The Accepted condition passes through.
func convertConditionsTo(conditions []metav1.Condition) []metav1.Condition {
	var out []metav1.Condition
	for _, c := range conditions {
		switch c.Type {
		case conditionTypeAccepted:
			out = append(out, c)
		case conditionTypeReady:
			out = append(out, splitReadyToAvailableAndVerified(c)...)
		default:
			out = append(out, c)
		}
	}
	return out
}

// splitReadyToAvailableAndVerified converts a single v1alpha1 Ready condition
// into the two v1beta1 conditions it encodes.
func splitReadyToAvailableAndVerified(ready metav1.Condition) []metav1.Condition {
	if handshakeReasons[ready.Reason] {
		// Handshake failure: workload was available (otherwise we wouldn't
		// have attempted the handshake), but verification failed.
		return []metav1.Condition{
			{
				Type:               conditionTypeAvailable,
				Status:             metav1.ConditionTrue,
				Reason:             "Available",
				Message:            "Workload is running",
				ObservedGeneration: ready.ObservedGeneration,
				LastTransitionTime: ready.LastTransitionTime,
			},
			{
				Type:               conditionTypeVerified,
				Status:             ready.Status,
				Reason:             "EndpointUnavailable",
				Message:            ready.Message,
				ObservedGeneration: ready.ObservedGeneration,
				LastTransitionTime: ready.LastTransitionTime,
			},
		}
	}

	if ready.Status == metav1.ConditionTrue && ready.Reason == "Available" {
		// Fully ready: both available and verified.
		return []metav1.Condition{
			{
				Type:               conditionTypeAvailable,
				Status:             metav1.ConditionTrue,
				Reason:             "Available",
				Message:            ready.Message,
				ObservedGeneration: ready.ObservedGeneration,
				LastTransitionTime: ready.LastTransitionTime,
			},
			{
				Type:               conditionTypeVerified,
				Status:             metav1.ConditionTrue,
				Reason:             "Verified",
				Message:            "MCP handshake succeeded",
				ObservedGeneration: ready.ObservedGeneration,
				LastTransitionTime: ready.LastTransitionTime,
			},
		}
	}

	// All other Ready states (DeploymentUnavailable, ServiceUnavailable,
	// ConfigurationInvalid, ScaledToZero, Initializing) are workload-level
	// issues. Verified is unknown since handshake hasn't been attempted.
	return []metav1.Condition{
		{
			Type:               conditionTypeAvailable,
			Status:             ready.Status,
			Reason:             ready.Reason,
			Message:            ready.Message,
			ObservedGeneration: ready.ObservedGeneration,
			LastTransitionTime: ready.LastTransitionTime,
		},
		{
			Type:               conditionTypeVerified,
			Status:             metav1.ConditionUnknown,
			Reason:             "NotVerified",
			Message:            "Handshake has not been attempted",
			ObservedGeneration: ready.ObservedGeneration,
			LastTransitionTime: ready.LastTransitionTime,
		},
	}
}

// convertConditionsFrom merges v1beta1 Available + Verified back into a single
// v1alpha1 Ready condition. The Accepted condition passes through.
func convertConditionsFrom(conditions []metav1.Condition) []metav1.Condition {
	var out []metav1.Condition

	available := meta.FindStatusCondition(conditions, conditionTypeAvailable)
	verified := meta.FindStatusCondition(conditions, conditionTypeVerified)

	for _, c := range conditions {
		switch c.Type {
		case conditionTypeAvailable, conditionTypeVerified:
			// handled below
		default:
			out = append(out, c)
		}
	}

	if available == nil && verified == nil {
		return out
	}

	ready := mergeAvailableAndVerifiedToReady(available, verified)
	out = append(out, ready)
	return out
}

// mergeAvailableAndVerifiedToReady collapses two v1beta1 conditions into one
// v1alpha1 Ready condition.
func mergeAvailableAndVerifiedToReady(available, verified *metav1.Condition) metav1.Condition {
	// If workload is not available, that dominates.
	if available != nil && available.Status != metav1.ConditionTrue {
		return metav1.Condition{
			Type:               conditionTypeReady,
			Status:             available.Status,
			Reason:             available.Reason,
			Message:            available.Message,
			ObservedGeneration: available.ObservedGeneration,
			LastTransitionTime: available.LastTransitionTime,
		}
	}

	// Workload is available. If verification failed, report that.
	if verified != nil && verified.Status == metav1.ConditionFalse {
		reason := "MCPEndpointUnavailable"
		return metav1.Condition{
			Type:               conditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            verified.Message,
			ObservedGeneration: verified.ObservedGeneration,
			LastTransitionTime: verified.LastTransitionTime,
		}
	}

	// Workload is available and verification passed (or is unknown/skipped).
	if verified != nil && verified.Status == metav1.ConditionTrue {
		src := verified
		if available != nil {
			src = available
		}
		return metav1.Condition{
			Type:               conditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Available",
			Message:            src.Message,
			ObservedGeneration: src.ObservedGeneration,
			LastTransitionTime: src.LastTransitionTime,
		}
	}

	// Verified is unknown or nil, workload available - still initializing handshake.
	src := available
	if src == nil {
		src = verified
	}
	return metav1.Condition{
		Type:               conditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Available",
		Message:            "Workload is running, handshake pending",
		ObservedGeneration: src.ObservedGeneration,
		LastTransitionTime: src.LastTransitionTime,
	}
}
