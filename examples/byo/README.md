# Existing workload and Service example

The [Kubernetes MCP Server example](../kubernetes-mcp-server/README.md) shows the default managed mode, where the operator creates the workload and Service. This example uses references for resources managed outside the operator.

This example creates a Deployment and Service to represent those existing resources, then an MCPServer that references them. The operator observes the Deployment and verifies the endpoint on Service port `80`, which forwards to container port `8080`. It does not take ownership of either resource.

## Deploy and verify

With the operator and its current `v1beta1` CRD installed, run from the repository root:

```bash
kubectl apply -f examples/byo/mcpserver.yaml
kubectl wait --for=condition=Available mcpserver/byo-mcp-server -n default --timeout=2m
kubectl wait --for=condition=Verified mcpserver/byo-mcp-server -n default --timeout=2m
kubectl get mcpserver byo-mcp-server -n default
kubectl get mcpserver byo-mcp-server -n default -o jsonpath='{.status.address.url}'
```

The address is `http://existing-mcp.default.svc.cluster.local:80/mcp`. The operator does not create a NetworkPolicy for this referenced workload. Configure any required isolation yourself and permit the operator's verification traffic.

## Cleanup

Deleting the MCPServer leaves the referenced Deployment and Service intact:

```bash
kubectl delete mcpserver byo-mcp-server -n default
kubectl get deployment,service existing-mcp -n default
```

Remove the resources created by this example when finished:

```bash
kubectl delete -f examples/byo/mcpserver.yaml --ignore-not-found
```

See the [existing workload and Service guide](https://mcp-lifecycle-operator.sigs.k8s.io/main/guides/byo/) for other reference combinations, configuration boundaries, and availability behavior.
