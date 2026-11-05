# MCP Lifecycle Operator Examples

This directory contains example MCPServer deployments.

## kubernetes-mcp-server

Deploys the [kubernetes-mcp-server](https://github.com/containers/kubernetes-mcp-server) which provides MCP tools for Kubernetes cluster interaction.

See [kubernetes-mcp-server/README.md](./kubernetes-mcp-server/README.md) for:
- Basic deployment
- ConfigMap-based configuration
- Testing and verification

## everything-mcp-server

Deploys the [Everything MCP Server](https://github.com/modelcontextprotocol/servers/tree/main/src/everything), one of the reference servers from the Model Context Protocol project that exercises all MCP features.

See [everything-mcp-server/README.md](./everything-mcp-server/README.md) for details.

## agentic-networking

Demonstrates exposing an MCPServer's generated Service through
[kube-agentic-networking](https://github.com/kubernetes-sigs/kube-agentic-networking)
Gateway API routing (`Gateway` + `HTTPRoute`, via a direct Service `backendRef`).

See [agentic-networking/README.md](./agentic-networking/README.md) for details.

## Quick Start

The examples below use the default managed mode. When another tool or team manages the workload or Service, the [BYO example](./byo/README.md) shows how to reference those resources.

```bash
# Install CRDs
make install

# Run controller locally
make run

# Deploy example
kubectl apply -f examples/kubernetes-mcp-server/mcpserver.yaml

# Check status
kubectl get mcpserver
```

For complete documentation, see the [main README](../README.md).
