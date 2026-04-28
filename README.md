# mcp-lifecycle-operator

A Kubernetes operator that provides a declarative API to deploy, manage, and safely roll out MCP Servers, handling their full lifecycle with production-grade automation and ecosystem integrations.

> **Note:** This project is currently in **alpha** (`v1alpha1`). APIs and behavior may change in future releases.

## Documentation

- [Introduction](https://mcp-lifecycle-operator.sigs.k8s.io/introduction/) - Architecture and MCPServer API overview
- [Quickstart Guide](https://mcp-lifecycle-operator.sigs.k8s.io/guides/quickstart/) - Get up and running quickly
- [API Reference](https://mcp-lifecycle-operator.sigs.k8s.io/reference/) - Full MCPServer API documentation
- [Complete MCPServer example](./config/samples/mcp_v1alpha1_mcpserver_complete.yaml) - YAML showing all available fields
- [Contributing](https://mcp-lifecycle-operator.sigs.k8s.io/contributing/) - How to contribute to the project

## Prerequisites

- Kubernetes cluster (v1.28+)
- kubectl configured to access your cluster

## Quick Start

### 1. Install the Operator

#### Option A: Install from Release (Recommended)

Install the operator and CRDs directly from the [latest release](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/latest):

```bash
kubectl apply -f https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/latest/download/install.yaml
```

This installs the CRDs, the controller Deployment, and all necessary RBAC resources in the `mcp-lifecycle-operator-system` namespace.

#### Option B: Run Locally (for Development)

Clone the repository and run the controller on your local machine:

```bash
make install  # Install the CRDs
make run      # Run the controller locally
```

Keep this terminal open. The controller logs will appear here.

#### Option C: Build and Deploy from Source

Build and deploy the controller as a Deployment in your cluster:

```bash
# Build and push the container image for multiple platforms
make docker-buildx IMG=<your-registry>/mcp-lifecycle-operator:latest

# Deploy to cluster
make deploy IMG=<your-registry>/mcp-lifecycle-operator:latest
```

Note: `docker-buildx` builds for multiple architectures (amd64, arm64, s390x, ppc64le) and pushes automatically.

### 2. Create a Test MCPServer

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
      ref: quay.io/containers/kubernetes_mcp_server:latest
  config:
    port: 8080
EOF
```

### 3. Verify the Deployment

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
NAME          READY   ACCEPTED   IMAGE                                                  PORT   ADDRESS                                               AGE
test-server   True    True       quay.io/containers/kubernetes_mcp_server:latest         8080   http://test-server.default.svc.cluster.local:8080/mcp   1m
```

The `ADDRESS` column shows the cluster-internal URL that can be used by other workloads to connect to the MCP server.

The status includes conditions and the service address for easy discovery:

```yaml
status:
  deploymentName: test-server
  serviceName: test-server
  address:
    url: http://test-server.default.svc.cluster.local:8080/mcp
  conditions:
    - type: Accepted
      status: "True"
      reason: Valid
    - type: Ready
      status: "True"
      reason: Available
```

### 4. Test the Service

Port-forward to test connectivity:

```bash
kubectl port-forward service/test-server 8080:8080
```

Then in another terminal:
```bash
# Test the health endpoint
curl http://localhost:8080/healthz

# Test the MCP endpoint
curl http://localhost:8080/mcp
```

You should see a response from the MCP server.

### 5. Uninstall (Optional)

To remove the CRDs and operator:

```bash
# If you installed from the release
kubectl delete -f https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/latest/download/install.yaml

# If you deployed from source
make undeploy
make uninstall
```

## Examples

For more examples, see the [examples/](./examples/) directory:

- **[kubernetes-mcp-server](./examples/kubernetes-mcp-server/)** - Deploy the Kubernetes MCP Server with basic and ConfigMap-based configurations
- **[everything-mcp-server](./examples/everything-mcp-server/)** - Deploy the Everything MCP Server

## Development

### Prerequisites

- Go 1.25+

### Building

```bash
# Generate code and manifests
make manifests generate

# Build binary
make build

# Run tests
make test
```

## Community, discussion, contribution, and support

This project is part of Kubernetes [SIG Apps](https://github.com/kubernetes/community/blob/main/sig-apps/README.md).

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- [Slack channel](https://kubernetes.slack.com/messages/sig-apps)
- [Mailing List](https://groups.google.com/a/kubernetes.io/g/sig-apps)

### Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](code-of-conduct.md).
