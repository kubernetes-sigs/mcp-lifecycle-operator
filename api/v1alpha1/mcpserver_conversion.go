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

	// Status - direct field copy
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	dst.Status.DeploymentName = src.Status.DeploymentName
	dst.Status.ServiceName = src.Status.ServiceName
	dst.Status.Address = convertAddressTo(src.Status.Address)
	dst.Status.ServerInfo = convertServerInfoTo(src.Status.ServerInfo)
	dst.Status.HandshakeRetryCount = src.Status.HandshakeRetryCount
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.ReadyReplicas = src.Status.ReadyReplicas
	dst.Status.Conditions = src.Status.Conditions

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

	// Status - direct field copy
	dst.Status.ObservedGeneration = src.Status.ObservedGeneration
	dst.Status.DeploymentName = src.Status.DeploymentName
	dst.Status.ServiceName = src.Status.ServiceName
	dst.Status.Address = convertAddressFrom(src.Status.Address)
	dst.Status.ServerInfo = convertServerInfoFrom(src.Status.ServerInfo)
	dst.Status.HandshakeRetryCount = src.Status.HandshakeRetryCount
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.ReadyReplicas = src.Status.ReadyReplicas
	dst.Status.Conditions = src.Status.Conditions

	return nil
}

func convertSourceTo(in Source) v1beta1.Source {
	out := v1beta1.Source{
		Type: v1beta1.SourceType(in.Type),
	}
	if in.ContainerImage != nil {
		out.ContainerImage = &v1beta1.ContainerImageSource{
			Ref: in.ContainerImage.Ref,
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
			Ref: in.ContainerImage.Ref,
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
	}
}

func convertNetworkConfigFrom(in *v1beta1.NetworkConfig) *NetworkConfig {
	if in == nil {
		return nil
	}
	return &NetworkConfig{
		IngressFrom: in.IngressFrom,
	}
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
