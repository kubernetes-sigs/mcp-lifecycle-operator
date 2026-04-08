# API Reference

## Packages
- [mcp.x-k8s.io/v1alpha1](#mcpx-k8siov1alpha1)


## mcp.x-k8s.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the mcp v1alpha1 API group.

### Resource Types
- [MCPServer](#mcpserver)



#### ContainerImageSource



ContainerImageSource defines a container image source.



_Appears in:_
- [Source](#source)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ref` _string_ | Ref is the container image containing the MCP server implementation.<br />Must be a valid OCI image reference.<br />Examples:<br />  - ghcr.io/modelcontextprotocol/servers/filesystem:latest<br />  - ghcr.io/modelcontextprotocol/servers/github:v1.0.0<br />  - custom-registry.io/my-mcp-server:1.2.3<br />  - custom-registry.io/my-mcp-server@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef |  | MaxLength: 1000 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### HealthConfig



HealthConfig defines health check configuration for the MCP server.
If not specified, no health probes will be configured.

The probes are passed directly to the Deployment's container spec without any
transformation, providing full access to the Kubernetes Probe API. This includes
all probe types (httpGet, tcpSocket, exec, grpc) and all configuration options
(initialDelaySeconds, periodSeconds, timeoutSeconds, successThreshold, failureThreshold).

_Validation:_
- MinProperties: 1

_Appears in:_
- [RuntimeConfig](#runtimeconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `livenessProbe` _[Probe](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#probe-v1-core)_ | LivenessProbe defines the liveness probe for the MCP server container.<br />Kubernetes uses liveness probes to know when to restart a container.<br />If not specified, no liveness probe will be configured.<br />This probe is passed directly to the container spec without transformation,<br />providing full compatibility with the Kubernetes Probe API. |  | Optional: \{\} <br /> |
| `readinessProbe` _[Probe](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#probe-v1-core)_ | ReadinessProbe defines the readiness probe for the MCP server container.<br />Kubernetes uses readiness probes to know when a container is ready to start accepting traffic.<br />If not specified, no readiness probe will be configured.<br />This probe is passed directly to the container spec without transformation,<br />providing full compatibility with the Kubernetes Probe API. |  | Optional: \{\} <br /> |


#### MCPServer



MCPServer is the Schema for the mcpservers API





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `mcp.x-k8s.io/v1alpha1` | | |
| `kind` _string_ | `MCPServer` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[MCPServerSpec](#mcpserverspec)_ | spec defines the desired state of MCPServer |  | Required: \{\} <br /> |
| `status` _[MCPServerStatus](#mcpserverstatus)_ | status defines the observed state of MCPServer |  | Optional: \{\} <br /> |


#### MCPServerAddress



MCPServerAddress contains the address information for the MCPServer.



_Appears in:_
- [MCPServerStatus](#mcpserverstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | URL is the cluster-internal address of the MCP server service.<br />Format: http://<servicename>.<namespace>.svc.cluster.local:<port>/<path> |  | Optional: \{\} <br /> |


#### MCPServerSpec



MCPServerSpec defines the desired state of MCPServer.



_Appears in:_
- [MCPServer](#mcpserver)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `source` _[Source](#source)_ | Source is a required field that defines where the MCP server should be sourced from.<br />Currently supports container images, with potential for additional source types in the future.<br />This configuration determines how the MCP server will be deployed and run. |  | Required: \{\} <br /> |
| `config` _[ServerConfig](#serverconfig)_ | Config is a required field that defines how the MCP server should be configured when it runs.<br />This includes runtime settings such as the server port, command-line arguments,<br />environment variables, and storage mounts. |  | Required: \{\} <br /> |
| `runtime` _[RuntimeConfig](#runtimeconfig)_ | Runtime defines runtime management configuration.<br />If not specified, default runtime settings will be applied. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### MCPServerStatus



MCPServerStatus defines the observed state of MCPServer.



_Appears in:_
- [MCPServer](#mcpserver)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `phase` _string_ | Phase represents the current lifecycle phase of the MCPServer.<br />Possible values: Pending, Running, Failed |  | Optional: \{\} <br /> |
| `deploymentName` _string_ | DeploymentName is the name of the Deployment created for this MCPServer. |  | Optional: \{\} <br /> |
| `serviceName` _string_ | ServiceName is the name of the Service created for this MCPServer. |  | Optional: \{\} <br /> |
| `address` _[MCPServerAddress](#mcpserveraddress)_ | Address contains the address of the MCP server service. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#condition-v1-meta) array_ | Conditions represent the current state of the MCPServer resource.<br />Each condition has a unique type and reflects the status of a specific aspect of the resource.<br />Standard condition types include "Ready", "Progressing", and "Degraded".<br />The "Ready" condition indicates the resource is fully functional and available.<br />The "Progressing" condition indicates the resource is being created or updated.<br />The "Degraded" condition indicates the resource failed to reach or maintain its desired state.<br />The status of each condition is one of True, False, or Unknown. |  | Optional: \{\} <br /> |


#### MountPermissions

_Underlying type:_ _string_

MountPermissions defines the access permissions for a volume mount.

_Validation:_
- Enum: [ReadOnly ReadWrite RecursiveReadOnly]

_Appears in:_
- [StorageMount](#storagemount)

| Field | Description |
| --- | --- |
| `ReadOnly` | MountPermissionsReadOnly indicates the mount is read-only.<br /> |
| `ReadWrite` | MountPermissionsReadWrite indicates the mount is read-write.<br /> |
| `RecursiveReadOnly` | MountPermissionsRecursiveReadOnly indicates the mount and all its submounts are recursively read-only.<br />This provides stronger guarantees than ReadOnly alone.<br /> |


#### RuntimeConfig



RuntimeConfig defines runtime execution configuration for the MCP server.

This section covers how the MCP server executes and behaves at runtime,
including replicas, security, resource allocation, and health probes.

If not specified, default runtime settings will be applied.
See individual field documentation for specific defaults.

_Validation:_
- MinProperties: 1

_Appears in:_
- [MCPServerSpec](#mcpserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `replicas` _integer_ | Replicas is the number of MCP server pod replicas to run.<br />Defaults to 1 if not specified.<br />Set to 0 to scale down the deployment.<br />This field is a pointer (*int32) to distinguish between:<br />  - nil (not specified) -> defaults to 1 replica<br />  - ptr.To(0) (explicit 0) -> scale-to-zero<br />This follows the same pattern as Deployment.Spec.Replicas in k8s.io/api/apps/v1. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `security` _[SecurityConfig](#securityconfig)_ | Security defines security-related configuration.<br />If not specified, default security settings will be applied. |  | Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#resourcerequirements-v1-core)_ | Resources defines the resource requirements for the MCP server container.<br />This includes CPU and memory requests and limits.<br />If not specified, the container will run without explicit resource constraints.<br />Supports partial specification (e.g., only requests or only limits).<br />Example:<br />  resources:<br />    requests:<br />      cpu: "100m"<br />      memory: "256Mi"<br />    limits:<br />      cpu: "500m"<br />      memory: "512Mi" |  | Optional: \{\} <br /> |
| `health` _[HealthConfig](#healthconfig)_ | Health defines health check configuration for the MCP server.<br />If not specified, no health probes will be configured. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### SecurityConfig



SecurityConfig defines security-related configuration.
If not specified, default security settings will be applied.
See individual field documentation for specific defaults.



_Appears in:_
- [RuntimeConfig](#runtimeconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `serviceAccountName` _string_ | ServiceAccountName is the name of the ServiceAccount to use for the MCP server pods.<br />The ServiceAccount should have appropriate RBAC permissions for the MCP server's operations.<br />If not specified, the default ServiceAccount for the namespace will be used.<br />Must be a string that follows the DNS1123 subdomain format.<br />Must be at most 253 characters in length, and must consist only of lower case alphanumeric characters, '-'<br />and '.', and must start and end with an alphanumeric character.<br />Example: For kubernetes-mcp-server with read-only access, create a ServiceAccount<br />and bind it to the 'view' ClusterRole. |  | MaxLength: 253 <br />Optional: \{\} <br /> |
| `podSecurityContext` _[PodSecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podsecuritycontext-v1-core)_ | PodSecurityContext specifies the security context for the MCP server pod. |  | Optional: \{\} <br /> |
| `securityContext` _[SecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#securitycontext-v1-core)_ | SecurityContext specifies the security context for the MCP server container. |  | Optional: \{\} <br /> |


#### ServerConfig



ServerConfig defines how the MCP server should be configured when it runs.



_Appears in:_
- [MCPServerSpec](#mcpserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `port` _integer_ | Port is a required field that specifies the port number on which the MCP server listens for connections.<br />Must be between 1 and 65535.<br />This should match the port that the MCP server container exposes and will be used for<br />configuring the Kubernetes Service. |  | Maximum: 65535 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `arguments` _string array_ | Arguments are command line arguments for the MCP server container.<br />Use this to pass configuration flags to the server.<br />Example: ["--config", "/etc/mcp-config/config.toml", "--verbose"]<br />When not specified, the container image's default arguments (CMD/ENTRYPOINT) are used.<br />An empty array [] is allowed and will override the container image's default arguments with no arguments.<br />Empty strings within the array are not allowed. |  | Optional: \{\} <br /> |
| `env` _[EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envvar-v1-core) array_ | Env is a list of environment variables to set in the MCP server container.<br />Supports the full Kubernetes EnvVar API including valueFrom for secrets and configmaps. |  | Optional: \{\} <br /> |
| `envFrom` _[EnvFromSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envfromsource-v1-core) array_ | EnvFrom is a list of sources to populate environment variables in the MCP server container.<br />Each entry injects all key-value pairs from a Secret or ConfigMap as environment variables.<br />The keys become the variable names. Useful when a Secret's keys already match<br />the expected env var names (e.g., GITHUB_TOKEN). |  | Optional: \{\} <br /> |
| `storage` _[StorageMount](#storagemount) array_ | Storage defines storage mounts for ConfigMaps, Secrets, and EmptyDirs.<br />Each item uses native Kubernetes volume source types for consistency and feature parity.<br />If specified, must contain at least 1 item. Maximum 64 items.<br />Each storage mount must have a unique path. |  | MaxItems: 64 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `path` _string_ | Path is the HTTP path where the MCP server listens for SSE/Streamable HTTP connections.<br />This path is appended to the service address in the status URL.<br />Must be a valid URI path component starting with '/'.<br />Maximum 253 characters. Cannot contain spaces, control characters, or query/fragment separators (? #).<br />Examples: /mcp, /api/v1/mcp, /services/mcp-server<br />Defaults to /mcp if not specified. | /mcp | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### Source



Source defines where the MCP server's container image (or other source types in the future) is located.



_Appears in:_
- [MCPServerSpec](#mcpserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[SourceType](#sourcetype)_ | Type is a required field that configures how the MCP server should be sourced.<br />Allowed values are: ContainerImage.<br />When set to ContainerImage, the MCP server will be sourced directly from an OCI<br />container image following the configuration specified in containerImage. |  | Enum: [ContainerImage] <br />Required: \{\} <br /> |
| `containerImage` _[ContainerImageSource](#containerimagesource)_ | ContainerImage specifies container image details when Type is ContainerImage. |  | Optional: \{\} <br /> |


#### SourceType

_Underlying type:_ _string_

SourceType defines the type of source for the MCP server.

_Validation:_
- Enum: [ContainerImage]

_Appears in:_
- [Source](#source)

| Field | Description |
| --- | --- |
| `ContainerImage` | SourceTypeContainerImage indicates the source is a container image.<br /> |


#### StorageMount



StorageMount defines a storage mount combining volume source and mount configuration.
The Path and Permissions fields apply to all storage types, while Source contains
the type-specific configuration (ConfigMap, Secret, or EmptyDir).



_Appears in:_
- [ServerConfig](#serverconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `path` _string_ | Path is a required field that specifies where the volume should be mounted in the container.<br />Must be an absolute path (starting with /).<br />The ConfigMap or Secret data will be accessible to the MCP server process at this location.<br />Must be between 1 and 4096 characters, start with '/', and must not contain ':'. |  | MaxLength: 4096 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `permissions` _[MountPermissions](#mountpermissions)_ | Permissions specifies the access permissions for the mount.<br />Allowed values are ReadOnly, ReadWrite, and RecursiveReadOnly.<br />When set to ReadOnly, the mount is read-only.<br />When set to ReadWrite, the mount is read-write.<br />When set to RecursiveReadOnly, the mount and all submounts are recursively read-only.<br />Defaults to ReadOnly for ConfigMap and Secret mounts.<br />For EmptyDir mounts, ReadWrite is more common for writable scratch space. | ReadOnly | Enum: [ReadOnly ReadWrite RecursiveReadOnly] <br />Optional: \{\} <br /> |
| `source` _[StorageSource](#storagesource)_ | Source defines where the storage data comes from (ConfigMap, Secret, or EmptyDir). |  | Required: \{\} <br /> |


#### StorageSource



StorageSource defines the source of the storage to mount (ConfigMap, Secret, or EmptyDir).



_Appears in:_
- [StorageMount](#storagemount)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[StorageType](#storagetype)_ | Type is a required field that specifies the type of volume source.<br />Allowed values are: ConfigMap, Secret, EmptyDir.<br />This determines which volume source field (configMap, secret, or emptyDir) should be configured. |  | Enum: [ConfigMap Secret EmptyDir] <br />Required: \{\} <br /> |
| `configMap` _[ConfigMapVolumeSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#configmapvolumesource-v1-core)_ | ConfigMap specifies a ConfigMap volume source (when Type is ConfigMap).<br />Uses native Kubernetes ConfigMapVolumeSource type for full feature parity. |  | Optional: \{\} <br /> |
| `secret` _[SecretVolumeSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#secretvolumesource-v1-core)_ | Secret specifies a Secret volume source (when Type is Secret).<br />Uses native Kubernetes SecretVolumeSource type for full feature parity. |  | Optional: \{\} <br /> |
| `emptyDir` _[EmptyDirVolumeSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#emptydirvolumesource-v1-core)_ | EmptyDir specifies an EmptyDir volume source (when Type is EmptyDir).<br />Uses native Kubernetes EmptyDirVolumeSource type for full feature parity. |  | Optional: \{\} <br /> |


#### StorageType

_Underlying type:_ _string_

StorageType defines the type of storage mount.

_Validation:_
- Enum: [ConfigMap Secret EmptyDir]

_Appears in:_
- [StorageSource](#storagesource)

| Field | Description |
| --- | --- |
| `ConfigMap` | StorageTypeConfigMap indicates a ConfigMap volume source.<br /> |
| `Secret` | StorageTypeSecret indicates a Secret volume source.<br /> |
| `EmptyDir` | StorageTypeEmptyDir indicates an EmptyDir volume source.<br /> |


