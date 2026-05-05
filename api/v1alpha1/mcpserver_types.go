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

// Generated from kubebuilder template:
// https://github.com/kubernetes-sigs/kubebuilder/blob/v4.11.1/pkg/plugins/golang/v4/scaffolds/internal/templates/api/types.go

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SourceType defines the type of source for the MCP server.
// +kubebuilder:validation:Enum=ContainerImage
type SourceType string

const (
	// SourceTypeContainerImage indicates the source is a container image.
	SourceTypeContainerImage SourceType = "ContainerImage"
)

// ContainerImageSource defines a container image source.
type ContainerImageSource struct {
	// Ref is the container image containing the MCP server implementation.
	// Must be a valid OCI image reference.
	// Examples:
	//   - ghcr.io/modelcontextprotocol/servers/filesystem:latest
	//   - ghcr.io/modelcontextprotocol/servers/github:v1.0.0
	//   - custom-registry.io/my-mcp-server:1.2.3
	//   - custom-registry.io/my-mcp-server@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=1000
	// +kubebuilder:validation:XValidation:rule="self.matches('^([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])((\\\\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]))+)?(:[0-9]+)?\\\\b')",message="must start with a valid domain. valid domains must be alphanumeric characters (lowercase and uppercase) separated by the \".\" character."
	// +kubebuilder:validation:XValidation:rule="self.find('(\\\\/[a-z0-9]+((([._]|__|[-]*)[a-z0-9]+)+)?((\\\\/[a-z0-9]+((([._]|__|[-]*)[a-z0-9]+)+)?)+)?)') != \"\"",message="a valid name is required. valid names must contain lowercase alphanumeric characters separated only by the \".\", \"_\", \"__\", \"-\" characters."
	// +kubebuilder:validation:XValidation:rule="self.find('(@.*:)') != \"\" || self.find(':.*$') != \"\"",message="must end with a digest or a tag"
	// +kubebuilder:validation:XValidation:rule="self.find('(@.*:)') == \"\" ? (self.find(':.*$') != \"\" ? self.find(':.*$').substring(1).size() <= 127 : true) : true",message="tag is invalid. the tag must not be more than 127 characters"
	// +kubebuilder:validation:XValidation:rule="self.find('(@.*:)') == \"\" ? (self.find(':.*$') != \"\" ? self.find(':.*$').matches(':[\\\\w][\\\\w.-]*$') : true) : true",message="tag is invalid. valid tags must begin with a word character (alphanumeric + \"_\") followed by word characters or \".\", and \"-\" characters"
	// +kubebuilder:validation:XValidation:rule="self.find('(@.*:)') != \"\" ? self.find('(@.*:)').matches('(@[A-Za-z][A-Za-z0-9]*([-_+.][A-Za-z][A-Za-z0-9]*)*[:])') : true",message="digest algorithm is not valid. valid algorithms must start with an uppercase or lowercase alpha character followed by alphanumeric characters and may contain the \"-\", \"_\", \"+\", and \".\" characters."
	// +kubebuilder:validation:XValidation:rule="self.find('(@.*:)') != \"\" ? self.find(':.*$').substring(1).size() >= 32 : true",message="digest is not valid. the encoded string must be at least 32 characters"
	// +kubebuilder:validation:XValidation:rule="self.find('(@.*:)') != \"\" ? self.find(':.*$').matches(':[0-9A-Fa-f]*$') : true",message="digest is not valid. the encoded string must only contain hex characters (A-F, a-f, 0-9)"
	Ref string `json:"ref,omitempty"`
	// NOTE: the validation rules above are taken from
	// https://github.com/operator-framework/operator-controller/blob/475e1341d0aa045c4fcb6a93a1ffeb2d16484ca7/api/v1/clustercatalog_types.go#L275-L321

	// Future fields could include:
	//   - ImagePullSecrets
	//   - PullPolicy
}

// Source defines where the MCP server's container image (or other source types in the future) is located.
// +kubebuilder:validation:XValidation:rule="self.type == 'ContainerImage' ? has(self.containerImage) : !has(self.containerImage)",message="containerImage must be set when type is ContainerImage and must not be set otherwise"
type Source struct {
	// Type is a required field that configures how the MCP server should be sourced.
	// Allowed values are: ContainerImage.
	// When set to ContainerImage, the MCP server will be sourced directly from an OCI
	// container image following the configuration specified in containerImage.
	// +kubebuilder:validation:Required
	Type SourceType `json:"type,omitempty"`

	// ContainerImage specifies container image details when Type is ContainerImage.
	// +optional
	ContainerImage *ContainerImageSource `json:"containerImage,omitempty"`
}

// StorageType defines the type of storage mount.
// +kubebuilder:validation:Enum=ConfigMap;Secret;EmptyDir
type StorageType string

const (
	// StorageTypeConfigMap indicates a ConfigMap volume source.
	StorageTypeConfigMap StorageType = "ConfigMap"
	// StorageTypeSecret indicates a Secret volume source.
	StorageTypeSecret StorageType = "Secret"
	// StorageTypeEmptyDir indicates an EmptyDir volume source.
	StorageTypeEmptyDir StorageType = "EmptyDir"
)

// MountPermissions defines the access permissions for a volume mount.
// +kubebuilder:validation:Enum=ReadOnly;ReadWrite;RecursiveReadOnly
type MountPermissions string

const (
	// MountPermissionsReadOnly indicates the mount is read-only.
	MountPermissionsReadOnly MountPermissions = "ReadOnly"
	// MountPermissionsReadWrite indicates the mount is read-write.
	MountPermissionsReadWrite MountPermissions = "ReadWrite"
	// MountPermissionsRecursiveReadOnly indicates the mount and all its submounts are recursively read-only.
	// This provides stronger guarantees than ReadOnly alone.
	MountPermissionsRecursiveReadOnly MountPermissions = "RecursiveReadOnly"
)

// StorageSource defines the source of the storage to mount (ConfigMap, Secret, or EmptyDir).
// +kubebuilder:validation:XValidation:rule="self.type == 'ConfigMap' ? has(self.configMap) : !has(self.configMap)",message="configMap must be set when type is ConfigMap and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'Secret' ? has(self.secret) : !has(self.secret)",message="secret must be set when type is Secret and must not be set otherwise"
// +kubebuilder:validation:XValidation:rule="self.type == 'EmptyDir' ? has(self.emptyDir) : !has(self.emptyDir)",message="emptyDir must be set when type is EmptyDir and must not be set otherwise"
type StorageSource struct {
	// Type is a required field that specifies the type of volume source.
	// Allowed values are: ConfigMap, Secret, EmptyDir.
	// This determines which volume source field (configMap, secret, or emptyDir) should be configured.
	// +kubebuilder:validation:Required
	Type StorageType `json:"type,omitempty"`

	// ConfigMap specifies a ConfigMap volume source (when Type is ConfigMap).
	// Uses native Kubernetes ConfigMapVolumeSource type for full feature parity.
	// +optional
	ConfigMap *corev1.ConfigMapVolumeSource `json:"configMap,omitempty"`

	// Secret specifies a Secret volume source (when Type is Secret).
	// Uses native Kubernetes SecretVolumeSource type for full feature parity.
	// +optional
	Secret *corev1.SecretVolumeSource `json:"secret,omitempty"`

	// EmptyDir specifies an EmptyDir volume source (when Type is EmptyDir).
	// Uses native Kubernetes EmptyDirVolumeSource type for full feature parity.
	// +optional
	EmptyDir *corev1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
}

// StorageMount defines a storage mount combining volume source and mount configuration.
// The Path and Permissions fields apply to all storage types, while Source contains
// the type-specific configuration (ConfigMap, Secret, or EmptyDir).
type StorageMount struct {
	// Path is a required field that specifies where the volume should be mounted in the container.
	// Must be an absolute path (starting with /).
	// The ConfigMap or Secret data will be accessible to the MCP server process at this location.
	// Must be between 1 and 4096 characters, start with '/', and must not contain ':'.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	// +kubebuilder:validation:XValidation:rule="self.startsWith('/')",message="path must be an absolute path (must start with '/')"
	// +kubebuilder:validation:XValidation:rule="!self.contains(':')",message="path must not contain ':' character"
	Path string `json:"path,omitempty"`

	// Permissions specifies the access permissions for the mount.
	// Allowed values are ReadOnly, ReadWrite, and RecursiveReadOnly.
	// When set to ReadOnly, the mount is read-only.
	// When set to ReadWrite, the mount is read-write.
	// When set to RecursiveReadOnly, the mount and all submounts are recursively read-only.
	// Defaults to ReadOnly for ConfigMap and Secret mounts.
	// For EmptyDir mounts, ReadWrite is more common for writable scratch space.
	// +optional
	// +kubebuilder:default=ReadOnly
	Permissions MountPermissions `json:"permissions,omitempty"`

	// Source defines where the storage data comes from (ConfigMap, Secret, or EmptyDir).
	// +kubebuilder:validation:Required
	Source StorageSource `json:"source,omitzero"`
}

// ServerConfig defines how the MCP server should be configured when it runs.
type ServerConfig struct {
	// Port is a required field that specifies the port number on which the MCP server listens for connections.
	// Must be between 1 and 65535.
	// This should match the port that the MCP server container exposes and will be used for
	// configuring the Kubernetes Service.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// Arguments are command line arguments for the MCP server container.
	// Use this to pass configuration flags to the server.
	// Example: ["--config", "/etc/mcp-config/config.toml", "--verbose"]
	// When not specified, the container image's default arguments (CMD/ENTRYPOINT) are used.
	// An empty array [] is allowed and will override the container image's default arguments with no arguments.
	// Empty strings within the array are not allowed.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self.all(arg, arg.size() > 0)",message="arguments must not contain empty strings"
	Arguments []string `json:"arguments,omitempty"`

	// Env is a list of environment variables to set in the MCP server container.
	// Supports the full Kubernetes EnvVar API including valueFrom for secrets and configmaps.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom is a list of sources to populate environment variables in the MCP server container.
	// Each entry injects all key-value pairs from a Secret or ConfigMap as environment variables.
	// The keys become the variable names. Useful when a Secret's keys already match
	// the expected env var names (e.g., GITHUB_TOKEN).
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// Storage defines storage mounts for ConfigMaps, Secrets, and EmptyDirs.
	// Each item uses native Kubernetes volume source types for consistency and feature parity.
	// If specified, must contain at least 1 item. Maximum 64 items.
	// Each storage mount must have a unique path.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=map
	// +listMapKey=path
	Storage []StorageMount `json:"storage,omitempty"`

	// Path is the HTTP path where the MCP server listens for SSE/Streamable HTTP connections.
	// This path is appended to the service address in the status URL.
	// Must be a valid URI path component starting with '/'.
	// Maximum 253 characters. Cannot contain spaces, control characters, or query/fragment separators (? #).
	// Examples: /mcp, /api/v1/mcp, /services/mcp-server
	// Defaults to /mcp if not specified.
	// +optional
	// +kubebuilder:default="/mcp"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="self.startsWith('/')",message="path must start with '/'"
	// +kubebuilder:validation:XValidation:rule="!self.contains(' ')",message="path must not contain spaces"
	// +kubebuilder:validation:XValidation:rule="!self.contains('?')",message="path must not contain query string separator '?'"
	// +kubebuilder:validation:XValidation:rule="!self.contains('#')",message="path must not contain fragment separator '#'"
	// +kubebuilder:validation:XValidation:rule="!self.contains('\\n') && !self.contains('\\r') && !self.contains('\\t')",message="path must not contain control characters (newlines, tabs)"
	Path string `json:"path,omitempty"`
}

// SecurityConfig defines security-related configuration.
// If not specified, default security settings will be applied.
// See individual field documentation for specific defaults.
type SecurityConfig struct {
	// ServiceAccountName is the name of the ServiceAccount to use for the MCP server pods.
	// The ServiceAccount should have appropriate RBAC permissions for the MCP server's operations.
	// If not specified, the default ServiceAccount for the namespace will be used.
	// Must be a string that follows the DNS1123 subdomain format.
	// Must be at most 253 characters in length, and must consist only of lower case alphanumeric characters, '-'
	// and '.', and must start and end with an alphanumeric character.
	// Example: For kubernetes-mcp-server with read-only access, create a ServiceAccount
	// and bind it to the 'view' ClusterRole.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="self == '' || !format.dns1123Subdomain().validate(self).hasValue()",message="serviceAccountName must be a valid DNS subdomain name: a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character."
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// PodSecurityContext specifies the security context for the MCP server pod.
	// +optional
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// SecurityContext specifies the security context for the MCP server container.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
}

// HealthConfig defines health check configuration for the MCP server.
// If not specified, no health probes will be configured.
//
// The probes are passed directly to the Deployment's container spec without any
// transformation, providing full access to the Kubernetes Probe API. This includes
// all probe types (httpGet, tcpSocket, exec, grpc) and all configuration options
// (initialDelaySeconds, periodSeconds, timeoutSeconds, successThreshold, failureThreshold).
//
// +kubebuilder:validation:MinProperties=1
type HealthConfig struct {
	// LivenessProbe defines the liveness probe for the MCP server container.
	// Kubernetes uses liveness probes to know when to restart a container.
	// If not specified, no liveness probe will be configured.
	//
	// This probe is passed directly to the container spec without transformation,
	// providing full compatibility with the Kubernetes Probe API.
	//
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe defines the readiness probe for the MCP server container.
	// Kubernetes uses readiness probes to know when a container is ready to start accepting traffic.
	// If not specified, no readiness probe will be configured.
	//
	// This probe is passed directly to the container spec without transformation,
	// providing full compatibility with the Kubernetes Probe API.
	//
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
}

// RuntimeConfig defines runtime execution configuration for the MCP server.
//
// This section covers how the MCP server executes and behaves at runtime,
// including replicas, security, resource allocation, and health probes.
//
// If not specified, default runtime settings will be applied.
// See individual field documentation for specific defaults.
// +kubebuilder:validation:MinProperties=1
type RuntimeConfig struct {
	// Replicas is the number of MCP server pod replicas to run.
	// Defaults to 1 if not specified.
	// Set to 0 to scale down the deployment.
	// This field is a pointer (*int32) to distinguish between:
	//   - nil (not specified) -> defaults to 1 replica
	//   - ptr.To(0) (explicit 0) -> scale-to-zero
	// This follows the same pattern as Deployment.Spec.Replicas in k8s.io/api/apps/v1.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// Security defines security-related configuration.
	// If not specified, default security settings will be applied.
	// +optional
	Security SecurityConfig `json:"security,omitzero"`

	// Resources defines the resource requirements for the MCP server container.
	// This includes CPU and memory requests and limits.
	// If not specified, the container will run without explicit resource constraints.
	// Supports partial specification (e.g., only requests or only limits).
	// Example:
	//   resources:
	//     requests:
	//       cpu: "100m"
	//       memory: "256Mi"
	//     limits:
	//       cpu: "500m"
	//       memory: "512Mi"
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Health defines health check configuration for the MCP server.
	// If not specified, no health probes will be configured.
	// +optional
	Health HealthConfig `json:"health,omitzero"`
}

// MCPServerSpec defines the desired state of MCPServer.
type MCPServerSpec struct {
	// ExtraLabels are applied to the Deployment metadata, PodTemplate metadata, and Service metadata.
	// The operator-managed keys "app" and "mcp-server" cannot be overridden.
	// +optional
	ExtraLabels map[string]string `json:"extraLabels,omitempty"`
	// ExtraAnnotations are applied to the Deployment metadata, PodTemplate metadata, and Service metadata.
	// +optional
	ExtraAnnotations map[string]string `json:"extraAnnotations,omitempty"`
	// Source is a required field that defines where the MCP server should be sourced from.
	// Currently supports container images, with potential for additional source types in the future.
	// This configuration determines how the MCP server will be deployed and run.
	// +kubebuilder:validation:Required
	Source Source `json:"source,omitzero"`

	// Config is a required field that defines how the MCP server should be configured when it runs.
	// This includes runtime settings such as the server port, command-line arguments,
	// environment variables, and storage mounts.
	// +kubebuilder:validation:Required
	Config ServerConfig `json:"config,omitzero"`

	// Runtime defines runtime management configuration.
	// If not specified, default runtime settings will be applied.
	// +optional
	Runtime RuntimeConfig `json:"runtime,omitzero"`
}

// MCPServerAddress contains the address information for the MCPServer.
type MCPServerAddress struct {
	// URL is the cluster-internal address of the MCP server service.
	// Format: http://<servicename>.<namespace>.svc.cluster.local:<port>/<path>
	// +optional
	URL string `json:"url,omitempty"`
}

// MCPServerCapabilities describes which MCP protocol capabilities the server advertises
// during the initialize handshake.
type MCPServerCapabilities struct {
	// Tools indicates the server supports tool listing and invocation.
	// +optional
	Tools bool `json:"tools,omitempty"`
	// Resources indicates the server supports resource listing and reading.
	// +optional
	Resources bool `json:"resources,omitempty"`
	// Prompts indicates the server supports prompt templates.
	// +optional
	Prompts bool `json:"prompts,omitempty"`
	// Logging indicates the server supports sending log messages.
	// +optional
	Logging bool `json:"logging,omitempty"`
	// Completions indicates the server supports argument autocompletion.
	// +optional
	Completions bool `json:"completions,omitempty"`
}

// MCPServerInfo contains identity and capability information reported by the
// MCP server during the protocol initialize handshake.
type MCPServerInfo struct {
	// Name is the server's self-reported name.
	// +optional
	Name string `json:"name,omitempty"`
	// Version is the server's self-reported version.
	// +optional
	Version string `json:"version,omitempty"`
	// ProtocolVersion is the MCP protocol version negotiated during the handshake.
	// +optional
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	// Instructions describes how to use the server and its features.
	// This can be used by clients to improve the LLM's understanding of
	// available tools, resources, etc.
	// +optional
	Instructions string `json:"instructions,omitempty"`
	// Capabilities lists which MCP protocol features the server supports.
	// +optional
	Capabilities *MCPServerCapabilities `json:"capabilities,omitempty"`
}

// MCPServerStatus defines the observed state of MCPServer.
type MCPServerStatus struct {
	// ObservedGeneration reflects the generation most recently observed by the controller.
	// This allows users to determine if the status reflects the latest spec changes.
	// When observedGeneration matches metadata.generation, the status is up-to-date.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// DeploymentName is the name of the Deployment created for this MCPServer.
	// +optional
	DeploymentName string `json:"deploymentName,omitempty"`

	// ServiceName is the name of the Service created for this MCPServer.
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// Address contains the address of the MCP server service.
	// +optional
	Address *MCPServerAddress `json:"address,omitempty"`

	// ServerInfo contains identity and capability information reported by the
	// MCP server during the protocol initialize handshake.
	// This field is populated only after a successful handshake.
	// +optional
	ServerInfo *MCPServerInfo `json:"serverInfo,omitempty"`

	// Conditions represent the latest available observations of the MCPServer's state.
	//
	// Standard condition types:
	// - "Accepted": Configuration is valid and all referenced resources exist
	// - "Ready": MCP server is operational and ready to serve requests
	//
	// The "Accepted" condition validates configuration before creating resources.
	// Reasons: Valid (True), Invalid (False with details in message)
	//
	// The "Ready" condition indicates overall server readiness.
	// Status=True means at least one instance is healthy and serving requests.
	// Reasons:
	//   - Available: Server is ready (Status=True)
	//   - ConfigurationInvalid: Accepted=False, cannot proceed
	//   - DeploymentUnavailable: No healthy instances (all deployment/pod issues)
	//   - ScaledToZero: Deployment scaled to 0 replicas
	//   - Initializing: Waiting for initial status
	//
	// Note: Specific failure details (ImagePullBackOff, OOMKilled, CrashLoop, etc.)
	// are included in the condition message, not the reason.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:ac:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.source.containerImage.ref`
// +kubebuilder:printcolumn:name="Port",type=integer,JSONPath=`.spec.config.port`
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.status.address.url`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// MCPServer runs a Model Context Protocol (MCP) server in Kubernetes.
//
// MCPServer creates and manages a Deployment and Service to run an MCP server from a
// container image. The MCP server exposes tools, resources, and prompts that AI applications
// can use via the Model Context Protocol.
//
// Example:
//
//	apiVersion: mcp.x-k8s.io/v1alpha1
//	kind: MCPServer
//	metadata:
//	  name: example
//	spec:
//	  source:
//	    type: ContainerImage
//	    containerImage:
//	      ref: example-mcp-image
//	  config:
//	    port: 8080
//
// The controller manages Deployment and Service resources with the same name as the MCPServer,
// using ownerReferences to establish ownership. The controller will reject updates to resources
// owned by other controllers or resources with no controller owner (to prevent silent overwrites
// of manually-created resources), but will adopt orphaned resources from a deleted MCPServer
// with the same name to enable seamless recreation.
type MCPServer struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of MCPServer
	// +required
	Spec MCPServerSpec `json:"spec"`

	// status defines the observed state of MCPServer
	// +optional
	Status MCPServerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MCPServerList contains a list of MCPServer
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MCPServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MCPServer{}, &MCPServerList{})
}
