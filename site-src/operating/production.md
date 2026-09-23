# Production readiness

The MCP Lifecycle Operator ships with defaults tuned for a fast getting-started experience: permissive network access, an allow-all egress posture for managed pods, and simple examples that skip external dependencies such as cert-manager. Those defaults are deliberately *not* what you want in production.

This page is aimed at **platform and cluster operators**. It is a hub that gathers the hardening and day-2 topics the operator supports, grouped by category. Each category links out to the detailed page where one already exists, and covers the configuration inline where it does not.

!!! note
    These settings are independent - adopt them incrementally. Nothing here changes the operator's default behaviour; you opt in per concern.

## Readiness checklist

| Area | Default | Production recommendation | Details |
| --- | --- | --- | --- |
| Managed-pod network isolation | Ingress open to any pod; egress allow-all | Restrict `spec.network.ingressFrom` / `egressTo` | [Network isolation](#network-isolation) |
| Gateway exposure | ClusterIP Service per server | Shared gateway via `httproute` or `kuadrant` | [Gateway integration](#gateway-integration) |
| TLS posture | Go defaults | Pin `TLS_MIN_VERSION` / groups to policy | [TLS & transport security](#tls-transport-security) |
| Metrics endpoint | Served over HTTPS on `8443` | Scrape over TLS; avoid `insecureSkipVerify` | [Metrics & monitoring security](#metrics-monitoring-security) |
| Operator hardening | Non-root, read-only rootfs, all caps dropped | Keep the shipped `securityContext`; scope RBAC | [Operator hardening](#operator-hardening) |
| Availability & resources | 1 replica, modest requests/limits | Size for load; plan storage migrations | [Availability & operations](#availability-operations) |

## Network isolation

The operator creates a `NetworkPolicy` for every managed MCP server pod. Out of the box that policy allows **any pod in the cluster** to reach the server port and lets the server pod egress **anywhere**. For production, and to align with the NSA CSI MCP Security guidance on designing for boundaries, restrict both directions through `spec.network`.

```yaml
apiVersion: mcp.x-k8s.io/v1beta1
kind: MCPServer
metadata:
  name: my-mcp-server
  namespace: default
spec:
  # ...
  network:
    # Only pods in the server's namespace carrying this label may reach it.
    # Add a namespaceSelector to admit clients from other namespaces.
    ingressFrom:
      - podSelector:
          matchLabels:
            role: mcp-client
    # The server pod may only egress to this CIDR on 443/TCP.
    egressTo:
      - ipBlock:
          cidr: 10.0.0.0/8
    egressPorts:
      - port: 443
        protocol: TCP
```

| Field | Effect when set | Effect when empty (default) |
| --- | --- | --- |
| `network.ingressFrom` | Only the listed `NetworkPolicyPeer`s can reach the server port | Any pod in the cluster can connect |
| `network.egressTo` | Server pod may only reach the listed destinations | Egress to all destinations allowed |
| `network.egressPorts` | Restricts egress to the listed ports | All ports allowed to whatever `egressTo` permits |
| `network.dnsEgressPeer` | Scopes the automatic DNS rule to a specific destination | DNS (port 53) allowed to any destination |

!!! warning "NetworkPolicies are additive"
    These rows describe only the policy the operator manages. Kubernetes unions every NetworkPolicy that selects a pod, so another policy in the namespace can still admit broader ingress or egress than what you configure here. Audit the other policies that select the server pod when you rely on these restrictions.

!!! warning "DNS is always permitted when egress is restricted"
    As soon as `egressTo` or `egressPorts` is set, the operator prepends an egress rule allowing UDP and TCP port `53` so pods can still resolve names. Use `dnsEgressPeer` to narrow that rule to your cluster's DNS service. `dnsEgressPeer` on its own does **not** activate egress restrictions.

Peers use the standard Kubernetes `NetworkPolicyPeer` shape (`podSelector`, `namespaceSelector`, `ipBlock`); `ipBlock` cannot be combined with the selector fields. These constraints are checked during **reconciliation**, not by an admission webhook: an MCPServer with an invalid CIDR is still admitted by the API server and then reports a `ValidationError` status condition, so validate config before applying rather than relying on `kubectl apply` to reject it.

## Gateway integration

Each MCP server is backed by a `ClusterIP` Service, reachable only from inside the cluster. To let clients outside the cluster reach servers through a shared ingress point - instead of wiring up external exposure per server - route them through a gateway. The operator ships two providers:

- **`httproute`** - the [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/).
- **`kuadrant`** - the [Kuadrant MCP Gateway](https://docs.kuadrant.io/latest/mcp-gateway/). The provider creates the `HTTPRoute` and `MCPServerRegistration` that route MCP traffic through the gateway; it does **not** create `AuthPolicy` or `RateLimitPolicy` resources (configure those separately - see the tip below).

Set `spec.gateway.provider` and the operator manages an `MCPGatewayBinding` for you. Both providers **require** `spec.gateway.configRef` pointing at a ConfigMap with the gateway details; reconciliation fails without it. See the full **[Gateway Integration guide](../guides/gateway.md)** for provider configuration and examples.

!!! tip
    For internet-facing deployments prefer the `kuadrant` provider, but expose an MCP server only after the applicable `AuthPolicy` and `RateLimitPolicy` resources are configured and verified at the gateway. Remember to supply its required `configRef`.

## TLS & transport security

The controller manager serves its webhook and metrics endpoints over TLS, and opens an outbound TLS connection to each managed server to verify the MCP handshake. Both surfaces read the same profile from `TLS_MIN_VERSION`, `TLS_CIPHER_SUITES`, and `TLS_GROUPS`.

In production, pin these to your organization's crypto policy (or let a platform TLS profile such as an OpenShift `TLSSecurityProfile` supply them) instead of relying on Go defaults. The outbound client path enforces a TLS 1.2 floor that a permissive profile cannot lower.

See the **[TLS configuration page](tls.md)** for the full variable reference, TLS 1.3 named groups, and post-quantum (`X25519MLKEM768`) availability.

## Metrics & monitoring security

The operator exposes Prometheus metrics over HTTPS on port `8443`. In production:

- Scrape the endpoint **over TLS** and provision the scraper with the CA so you can drop `insecureSkipVerify: true` from any `ServiceMonitor`/`PodMonitor`. cert-manager is the usual way to issue the serving certificate.
- Restrict who can reach `8443` (RBAC on the metrics reader, and/or a NetworkPolicy on the operator namespace).

See the **[Metrics page](metrics.md)** for the full list of exposed metrics and their labels.

## Operator hardening

The shipped controller Deployment already runs the manager container with a locked-down container-level `securityContext` (under `spec.template.spec.containers[]`) - keep it:

```yaml
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  capabilities:
    drop:
      - ALL
  seccompProfile:
    type: RuntimeDefault
```

Additional hardening for production:

- **RBAC** - the operator installs with the ClusterRole it needs to manage MCP servers cluster-wide. If you run it scoped to specific namespaces, tighten the role bindings accordingly and review the granted verbs against your least-privilege baseline.
- **Namespace isolation** - apply a NetworkPolicy to the operator's own namespace so only your monitoring stack can reach the metrics port.
- **Image provenance** - pin the operator image by digest and verify signatures where your supply-chain policy requires it.

## Availability & operations

- **Replicas & resources** - the operator ships with a single replica and modest requests/limits (`cpu: 10m` / `memory: 128Mi` requests, `cpu: 500m` / `memory: 512Mi` limits). Size these for the number of managed servers in your cluster and load-test before committing to limits; see [Performance testing](#performance-testing).
- **Storage-version migrations** - when upgrading across API storage versions, drive the migration deliberately rather than relying on lazy conversion. See the **[Storage Version Migration page](storage-version-migration.md)**.

### Performance testing

Sizing and scaling recommendations should be backed by load and stress testing (operator under many managed servers, CPU/memory under load). This is tracked upstream and is not yet part of this guide.

## Further reading

- NSA/CISA Cybersecurity Information Sheet on MCP security - the boundary and hardening recommendations this guide draws on.
- [Gateway Integration guide](../guides/gateway.md)
- [TLS configuration](tls.md) · [Metrics](metrics.md) · [Storage Version Migration](storage-version-migration.md)
