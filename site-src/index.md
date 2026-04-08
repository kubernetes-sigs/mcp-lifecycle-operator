# MCP Lifecycle Operator

A Kubernetes operator that provides a declarative API to deploy, manage, and safely roll out MCP Servers, handling their full lifecycle with production-grade automation and ecosystem integrations.

## Overview

The MCP Lifecycle Operator simplifies the deployment and management of [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) servers on Kubernetes. It provides a declarative, Kubernetes-native way to run MCP servers as scalable, production-ready services.

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
      ref: my-registry/mcp-server:latest
  config:
    port: 8081
```

## Get Started

Ready to deploy your first MCP server? Check out our [Getting Started Guide](guides/quickstart.md) or explore the [examples](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/tree/main/examples).

## Community

This project is part of [Kubernetes SIG Apps](https://github.com/kubernetes/community/blob/main/sig-apps/README.md).

- **Slack**: [#sig-apps on Kubernetes Slack](https://kubernetes.slack.com/messages/sig-apps)
- **Mailing List**: [sig-apps@kubernetes.io](https://groups.google.com/a/kubernetes.io/g/sig-apps)
- **GitHub**: [kubernetes-sigs/mcp-lifecycle-operator](https://github.com/kubernetes-sigs/mcp-lifecycle-operator)

## Contributing

We welcome contributions! See our [Contributing Guide](contributing/index.md) to get started.
