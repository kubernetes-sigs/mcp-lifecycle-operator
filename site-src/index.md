# MCP Lifecycle Operator

A Kubernetes operator that provides a declarative API to deploy, manage, and safely roll out [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) servers on Kubernetes, handling their full lifecycle with production-grade automation and ecosystem integrations.

!!! warning "Alpha"
    This project is currently in **alpha** (`v1alpha1`). APIs and behavior may change in future releases.

## Core Capabilities

**Declarative Deployment** - Define MCP servers using Kubernetes Custom Resources with automatic deployment, service creation, and lifecycle management.

**Production Ready** - Built-in health checks, security configurations, and robust status reporting for production environments.

**Kubernetes Native** - Seamless integration with Kubernetes ecosystem including ConfigMaps, Secrets, and standard networking.

**Lifecycle Management** - Automated rollouts, updates, and deletions with proper cleanup and resource management.

## Quick Example

Deploy an MCP server with a simple YAML manifest:

```yaml
apiVersion: mcp.x-k8s.io/v1alpha1
kind: MCPServer
metadata:
  name: my-mcp-server
  namespace: default
spec:
  source:
    type: ContainerImage
    containerImage:
      ref: quay.io/containers/kubernetes_mcp_server:latest
  config:
    port: 8080
```

## Install

Install the operator from the [latest release](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/latest):

```bash
kubectl apply -f https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/latest/download/install.yaml
```

## Get Started

Learn more about the operator in the [Introduction](introduction.md), or jump straight to the [Getting Started Guide](guides/quickstart.md) to deploy your first MCP server. You can also explore the [examples](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/tree/main/examples).

## Community

This project is part of [Kubernetes SIG Apps](https://github.com/kubernetes/community/blob/main/sig-apps/README.md).

- **Slack**: [#sig-apps on Kubernetes Slack](https://kubernetes.slack.com/messages/sig-apps)
- **Mailing List**: [sig-apps@kubernetes.io](https://groups.google.com/a/kubernetes.io/g/sig-apps)
- **GitHub**: [kubernetes-sigs/mcp-lifecycle-operator](https://github.com/kubernetes-sigs/mcp-lifecycle-operator)

## Contributing

We welcome contributions! See our [Contributing Guide](contributing/index.md) to get started.
