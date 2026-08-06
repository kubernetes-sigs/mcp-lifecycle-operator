# Gateway Integration Example

This example demonstrates exposing an MCP server through a gateway using the built-in `httproute` provider, which creates a [Gateway API HTTPRoute](https://gateway-api.sigs.k8s.io/api-types/httproute/).

## Prerequisites

- Kubernetes cluster with the MCP Lifecycle Operator installed
- [Gateway API CRDs](https://gateway-api.sigs.k8s.io/guides/#installing-gateway-api) installed
- A Gateway resource deployed and managed by a gateway controller (e.g., Envoy Gateway, Istio, Cilium)

## Deploy

1. Create the gateway configuration ConfigMap:

```bash
kubectl apply -f gateway-config.yaml
```

2. Deploy the MCPServer with gateway integration:

```bash
kubectl apply -f mcpserver-with-gateway.yaml
```

## Verify

```bash
# Check the MCPGatewayBinding is registered
kubectl get mcpgatewaybindings

# Check the HTTPRoute was created
kubectl get httproutes

# Verify the MCPServer address reflects the gateway URL
kubectl get mcpserver kubernetes-mcp-server -o jsonpath='{.status.address.url}'
```

## Configuration

Edit `gateway-config.yaml` to match your environment:

| Key                 | Description                              |
|---------------------|------------------------------------------|
| `gateway-name`      | Name of your Gateway resource            |
| `gateway-namespace` | Namespace where the Gateway lives        |
| `hostname`          | Hostname for routing (optional)          |

## Cleanup

```bash
kubectl delete -f mcpserver-with-gateway.yaml
kubectl delete -f gateway-config.yaml
```