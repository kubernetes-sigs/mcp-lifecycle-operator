# Use an existing workload or Service

In the default **managed mode** shown in the [quickstart](quickstart.md), the operator creates and maintains the Deployment, Service, and NetworkPolicy from your MCPServer configuration.

Bring your own (BYO) is an alternative when another tool or team already manages a workload or Service. The operator observes referenced workloads and verifies their MCP endpoint while you retain control of their configuration and lifecycle. Gateway integration remains available.

Use `apiVersion: mcp.x-k8s.io/v1beta1`. The `workloadRef` and `serviceRef` fields are not available in `v1alpha1`. Referenced resources must already exist in the same namespace as the MCPServer.

## Choose what the operator manages

| Configuration | Workload | Service | NetworkPolicy |
| --- | --- | --- | --- |
| Neither reference (default) | Operator-managed Deployment | Operator-managed | Operator-managed |
| `workloadRef` only | Existing workload | Operator-managed | User-managed |
| `serviceRef` only | Operator-managed Deployment | Existing Service | Operator-managed |
| Both references | Existing workload | Existing Service | User-managed |

`workloadRef.kind` supports `Deployment`, `StatefulSet`, and `DaemonSet`. The operator does not modify, adopt, or delete resources selected through these references. Deleting an MCPServer removes its operator-owned resources, including any generated Service and gateway binding, while referenced resources remain.

## Reference an existing workload and Service

Suppose the following Deployment and Service are managed outside the operator. The Service selects the Deployment's pods through their `app: existing-mcp` label:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: existing-mcp
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: existing-mcp
  template:
    metadata:
      labels:
        app: existing-mcp
    spec:
      containers:
        - name: server
          image: quay.io/containers/kubernetes_mcp_server:latest
          ports:
            - name: http
              containerPort: 8080
          readinessProbe:
            tcpSocket:
              port: http
---
apiVersion: v1
kind: Service
metadata:
  name: existing-mcp
  namespace: default
spec:
  selector:
    app: existing-mcp
  ports:
    - name: mcp
      port: 80
      targetPort: http
```

The server listens on container port `8080`, named `http`. The Service exposes port `80` and forwards to that container port through `targetPort: http`.

Reference both resources from an MCPServer in the same namespace:

```yaml
apiVersion: mcp.x-k8s.io/v1beta1
kind: MCPServer
metadata:
  name: byo-mcp-server
  namespace: default
spec:
  workloadRef:
    kind: Deployment
    name: existing-mcp
  serviceRef:
    name: existing-mcp
```

The operator resolves the endpoint port from the Service: it prefers a port named `mcp`, then falls back to the first port. In this example, it connects to `http://existing-mcp.default.svc.cluster.local:80/mcp`.

With both references set, you may instead set `config.port` to select a Service port explicitly. For this Service, that would be `config.port: 80`, not the container port `8080`.

`config.path` defaults to `/mcp`. Set it if the existing server uses a different MCP path. You can also configure `transport` for TLS and `gateway` for [gateway integration](gateway.md).

See the [runnable BYO example](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/tree/main/examples/byo) for a Deployment, Service, and MCPServer you can apply together.

## Reference only the workload

For the existing Deployment above, omit `serviceRef` to let the operator create its own Service:

```yaml
apiVersion: mcp.x-k8s.io/v1beta1
kind: MCPServer
metadata:
  name: byo-mcp-server
  namespace: default
spec:
  workloadRef:
    kind: Deployment
    name: existing-mcp
  config:
    port: 8080
```

Here, `config.port` is required. The generated Service is named `byo-mcp-server`, exposes port `8080`, and forwards to numeric pod port `8080`; the workload does not need a container port named `mcp`.

The Service uses the workload's `spec.selector.matchLabels`. If the selector contains `matchExpressions`, or has no `matchLabels`, provide `serviceRef` instead. You are responsible for supplying a Service whose selector routes to the intended pods.

## Reference only the Service

Keep the default managed workload but use an existing Service. For this example, the Service selects the operator-managed pods using `mcp-server: managed-mcp-server`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: existing-mcp
  namespace: default
spec:
  selector:
    mcp-server: managed-mcp-server
  ports:
    - name: mcp
      port: 80
      targetPort: 8080
```

The MCPServer configures the container to listen on `8080` and references that Service:

```yaml
apiVersion: mcp.x-k8s.io/v1beta1
kind: MCPServer
metadata:
  name: managed-mcp-server
  namespace: default
spec:
  source:
    type: ContainerImage
    containerImage:
      ref: quay.io/containers/kubernetes_mcp_server:latest
  config:
    port: 8080
  serviceRef:
    name: existing-mcp
```

In this mode, `config.port` remains the required **container port**, `8080`. The endpoint port is inferred from the Service, preferring `mcp`, then the first port. The operator connects to Service port `80`, which forwards to container port `8080`.

The operator does not change the Service's selector, ports, or session affinity.

## Configuration boundaries

When `workloadRef` is set, configure containers, scaling, and network isolation on the existing workload and its own resources. The following MCPServer settings are rejected:

- `source`
- `config.arguments`, `config.env`, `config.envFrom`, and `config.storage`
- `runtime.replicas`, `runtime.resources`, `runtime.security`, and `runtime.health`
- `network`

The operator does not create a NetworkPolicy for a referenced workload. Ensure its network policies allow the operator to reach the MCP endpoint, along with any clients or gateway that need access.

When `serviceRef` is set, `mcp.stateless` is rejected because session affinity belongs to the existing Service. When both references are set, `extraLabels` and `extraAnnotations` are also rejected; these fields apply only to managed workload resources.

You cannot add or remove `workloadRef` after creating an MCPServer. Create a new MCPServer to switch between managed and existing workloads. You can update the referenced workload name or kind, and add, change, or remove `serviceRef` as needed, provided the resulting configuration is valid.

## Availability and discovery

The operator reads availability from the referenced workload's status:

- Deployments use the same Deployment conditions and ready-replica checks as managed mode.
- StatefulSets use `status.availableReplicas`; DaemonSets use `status.numberAvailable`. At least one available instance is sufficient once the workload controller has observed the current generation. These counters account for `minReadySeconds`.
- Partial availability can permit verification while a rollout continues. Inspect condition messages and replica counts for progress.
- A Deployment or StatefulSet intentionally scaled to zero reports `Available=True` with reason `ScaledToZero`, but its endpoint is not verified or published. A DaemonSet with no eligible nodes reports unavailable.

Inspect the MCPServer alongside the workload:

```bash
kubectl get mcpserver byo-mcp-server -n default
kubectl describe mcpserver byo-mcp-server -n default
kubectl get deployment existing-mcp -n default
```

The `WORKLOAD` column identifies a referenced workload as, for example, `BYO:Deployment/existing-mcp`. Status records that identity in `workloadName` and `workloadSummary`; `deploymentName` is reserved for managed Deployments. `serviceName` identifies the referenced or generated Service.

`Verified` reports endpoint verification independently of workload availability. The operator publishes `status.address` only when `Verified=True`; an authentication-protected endpoint can report reason `AuthSkipped` when it is reachable but requires credentials. The published address is the internal Service URL, or the gateway URL when gateway integration supplies one.
