# Quickstart Guide

This guide will walk you through deploying your first MCP server using the MCP Lifecycle Operator.

## Prerequisites

- Kubernetes cluster (v1.28+)
- kubectl configured to access your cluster
- Go 1.25+ (for building from source)

## Installation

### 1. Install the CRDs

First, install the Custom Resource Definitions:

```bash
make install
```

This creates the `MCPServer` CRD in your cluster.

### 2. Run the Controller

You have two options:

#### Option A: Run Locally (Recommended for Testing)

Run the controller on your local machine (it will connect to your cluster):

```bash
make run
```

Keep this terminal open. The controller logs will appear here.

#### Option B: Deploy to Cluster

Build and deploy the controller as a Deployment in your cluster:

```bash
# Build and push the container image for multiple platforms
make docker-buildx IMG=<your-registry>/mcp-lifecycle-operator:latest

# Deploy to cluster
make deploy IMG=<your-registry>/mcp-lifecycle-operator:latest
```

!!! note
    `docker-buildx` builds for multiple architectures (amd64, arm64, s390x, ppc64le) and pushes automatically.

## Deploy Your First MCP Server

### Create a Basic MCPServer

In a new terminal, create a basic `MCPServer` resource using the [kubernetes-mcp-server](https://github.com/containers/kubernetes-mcp-server):

```bash
kubectl apply -f - <<EOF
apiVersion: mcp.x-k8s.io/v1alpha1
kind: MCPServer
metadata:
  name: kubernetes-mcp-server
  namespace: default
spec:
  source:
    type: ContainerImage
    containerImage:
      ref: quay.io/containers/kubernetes_mcp_server:latest
  config:
    port: 8080
EOF
```

!!! note
    The kubernetes-mcp-server provides MCP tools for interacting with Kubernetes resources like pods, namespaces, and events. For full functionality, it needs RBAC permissions (see example below).

### Verify the Deployment

Check that the operator created the resources:

```bash
# View the MCPServer status
kubectl get mcpservers
kubectl get mcpserver kubernetes-mcp-server -o yaml

# Verify the Deployment was created
kubectl get deployment kubernetes-mcp-server

# Verify the Service was created
kubectl get service kubernetes-mcp-server

# Check the pod is running
kubectl get pods -l mcp-server=kubernetes-mcp-server
```

Expected output from `kubectl get mcpservers`:

```
NAME                    PHASE     IMAGE                                         PORT   ADDRESS                                                     AGE
kubernetes-mcp-server   Running   quay.io/containers/kubernetes_mcp_server:latest   8080   http://kubernetes-mcp-server.default.svc.cluster.local:8080/mcp  1m
```

The `ADDRESS` column shows the cluster-internal URL that can be used by other workloads to connect to the MCP server.

### View Status Details

The status includes the service address for easy discovery:

```yaml
status:
  phase: Running
  deploymentName: kubernetes-mcp-server
  serviceName: kubernetes-mcp-server
  address:
    url: http://kubernetes-mcp-server.default.svc.cluster.local:8080/mcp
  conditions:
    - type: Ready
      status: "True"
```

## Test the Service

Port-forward to test connectivity:

```bash
kubectl port-forward service/kubernetes-mcp-server 8080:8080
```

Then in another terminal:

```bash
# Test the health endpoint
curl http://localhost:8080/healthz

# Test the MCP endpoint
curl http://localhost:8080/mcp
```

You should see a response from the MCP server.

## Production Example: MCP Server with RBAC

For production use, the kubernetes-mcp-server needs RBAC permissions to access Kubernetes resources. This example shows the recommended setup with proper ServiceAccount and permissions:

```bash
kubectl apply -f - <<EOF
# ServiceAccount for the kubernetes-mcp-server
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mcp-viewer
  namespace: default
---
# ClusterRoleBinding to grant read-only access across the cluster
# Uses the built-in 'view' ClusterRole which provides read-only access to most resources
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: mcp-viewer-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view  # Built-in ClusterRole with read-only permissions
subjects:
  - kind: ServiceAccount
    name: mcp-viewer
    namespace: default
---
# ConfigMap containing the kubernetes-mcp-server configuration
apiVersion: v1
kind: ConfigMap
metadata:
  name: kubernetes-mcp-server-config
  namespace: default
data:
  config.toml: |
    # Kubernetes MCP Server Configuration
    log_level = 5
    port = "8080"
    read_only = true
    toolsets = ["core", "config"]
---
# MCPServer resource with ServiceAccount for RBAC
apiVersion: mcp.x-k8s.io/v1alpha1
kind: MCPServer
metadata:
  name: kubernetes-mcp-server-rbac
  namespace: default
spec:
  source:
    type: ContainerImage
    containerImage:
      ref: quay.io/containers/kubernetes_mcp_server:latest
  config:
    port: 8080
    arguments:
      - --config
      - /etc/mcp-config/config.toml
    storage:
      - path: /etc/mcp-config
        source:
          type: ConfigMap
          configMap:
            name: kubernetes-mcp-server-config
  runtime:
    security:
      serviceAccountName: mcp-viewer  # Use the ServiceAccount with RBAC permissions
EOF
```

This creates:
1. **ServiceAccount** (`mcp-viewer`) - Identity for the MCP server pods
2. **ClusterRoleBinding** - Binds the ServiceAccount to the built-in `view` ClusterRole
3. **ConfigMap** - Server configuration with read-only mode and specific toolsets
4. **MCPServer** - References the ServiceAccount and mounts the ConfigMap

!!! tip "Why is RBAC Needed?"
    The kubernetes-mcp-server provides tools that interact with the Kubernetes API (list pods, namespaces, events, etc.). Without proper RBAC, these tools will fail with permission errors. The built-in `view` ClusterRole provides read-only access to most resources, perfect for read-only MCP server operations.

## Cleanup

To remove the MCP server:

```bash
# Remove basic deployment
kubectl delete mcpserver kubernetes-mcp-server

# Or remove RBAC deployment (also deletes ServiceAccount, ClusterRoleBinding, and ConfigMap)
kubectl delete mcpserver kubernetes-mcp-server-rbac
kubectl delete clusterrolebinding mcp-viewer-binding
kubectl delete serviceaccount mcp-viewer
kubectl delete configmap kubernetes-mcp-server-config
```

To uninstall the operator:

```bash
# If you deployed to cluster
make undeploy

# Remove the CRDs
make uninstall
```

## Next Steps

- Explore more examples:
  - [kubernetes-mcp-server examples](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/tree/main/examples/kubernetes-mcp-server) - Basic, ConfigMap, and RBAC examples
  - [All examples](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/tree/main/examples)
- Check the [API Reference](../reference/) for all configuration options
