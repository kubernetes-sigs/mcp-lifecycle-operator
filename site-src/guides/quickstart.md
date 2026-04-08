# Quickstart Guide

This guide will walk you through deploying your first MCP server using the MCP Lifecycle Operator.

## Prerequisites

- Kubernetes cluster (v1.28+)
- kubectl configured to access your cluster
- Go 1.24+ (for building from source)

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

### Create a Test MCPServer

In a new terminal, create a test `MCPServer` resource:

```bash
kubectl apply -f - <<EOF
apiVersion: mcp.x-k8s.io/v1alpha1
kind: MCPServer
metadata:
  name: test-server
  namespace: default
spec:
  source:
    type: ContainerImage
    containerImage:
      ref: aliok/mcp-server-streamable-http:latest
  config:
    port: 8081
EOF
```

### Verify the Deployment

Check that the operator created the resources:

```bash
# View the MCPServer status
kubectl get mcpservers
kubectl get mcpserver test-server -o yaml

# Verify the Deployment was created
kubectl get deployment test-server

# Verify the Service was created
kubectl get service test-server

# Check the pod is running
kubectl get pods -l mcp-server=test-server
```

Expected output from `kubectl get mcpservers`:

```
NAME          PHASE     IMAGE                                      PORT   ADDRESS                                            AGE
test-server   Running   aliok/mcp-server-streamable-http:latest   8081   http://test-server.default.svc.cluster.local:8081/mcp  1m
```

The `ADDRESS` column shows the cluster-internal URL that can be used by other workloads to connect to the MCP server.

### View Status Details

The status includes the service address for easy discovery:

```yaml
status:
  phase: Running
  deploymentName: test-server
  serviceName: test-server
  address:
    url: http://test-server.default.svc.cluster.local:8081/mcp
  conditions:
    - type: Ready
      status: "True"
```

## Test the Service

Port-forward to test connectivity:

```bash
kubectl port-forward service/test-server 8081:8081
```

Then in another terminal:

```bash
curl http://localhost:8081/mcp
```

You should see a response from the MCP server.

## Example MCPServer Resources

### Streamable HTTP MCP Server

```yaml
apiVersion: mcp.x-k8s.io/v1alpha1
kind: MCPServer
metadata:
  name: streamable-http-server
  namespace: default
spec:
  source:
    type: ContainerImage
    containerImage:
      ref: aliok/mcp-server-streamable-http:latest
  config:
    port: 8081
```

### MCP Server with ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-config
data:
  LOG_LEVEL: "debug"
  FEATURE_FLAG: "enabled"
---
apiVersion: mcp.x-k8s.io/v1alpha1
kind: MCPServer
metadata:
  name: configured-server
spec:
  source:
    type: ContainerImage
    containerImage:
      ref: my-registry.io/mcp-server:latest
  config:
    port: 8081
    envFrom:
      - configMapRef:
          name: mcp-config
```

## Cleanup

To remove the MCP server:

```bash
kubectl delete mcpserver test-server
```

To uninstall the operator:

```bash
# If you deployed to cluster
make undeploy

# Remove the CRDs
make uninstall
```

## Next Steps

- Explore more [examples](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/tree/main/examples)
- Check the [API Reference](../reference/) for all configuration options
