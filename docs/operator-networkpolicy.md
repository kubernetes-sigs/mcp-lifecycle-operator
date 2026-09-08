# Operator NetworkPolicy

The controller-manager pod ships with a restrictive `NetworkPolicy`
(`allow-metrics-traffic`, in `config/network-policy/`) that hardens the operator
itself, not the MCP server workloads it manages.

Operand (MCP server) network policy is a separate, per-`MCPServer` concern
configured through `spec.network` on the CR - this document covers only the
operator's own pod.

## What the policy allows

Ingress:

| Port | Source | Purpose |
|------|--------|---------|
| 8081/TCP | any | kubelet liveness/readiness probes |
| 8443/TCP | namespaces labeled `metrics: enabled` | metrics scraping |
| 9443/TCP | any | admission/conversion webhook from the apiserver |

Egress (deny-by-default allow-list):

| ID | Destination | Ports | Purpose |
|----|-------------|-------|---------|
| E1 | cluster DNS | 53 UDP+TCP | name resolution |
| E2 | kube-apiserver | 443, 6443 TCP | leader election + all reconcile reads/writes |
| E3 | managed MCP server pods (label `mcp-server`) | any | MCP TLS handshake + discovery |

Everything else is denied.

## Adjusting the peer selectors for your cluster

The manifest ships with defaults that target a common Kubernetes layout. Two
egress peers may need adjustment depending on your distribution.

### DNS (E1)

The shipped rule targets the usual `kube-system` / `k8s-app: kube-dns` location:

```yaml
- to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: kube-system
      podSelector:
        matchLabels:
          k8s-app: kube-dns
```

If your cluster runs DNS in a different namespace or with different pod labels,
update the `namespaceSelector`/`podSelector` accordingly. This mirrors the
operand DNS modeling from PR #363.

### kube-apiserver (E2)

The apiserver is typically host-networked and cannot be selected by
pod/namespace selectors portably, so the shipped rule allows the standard
apiserver ports (443, 6443) to `0.0.0.0/0` as a documented interim peer. Where
your environment lets you express it, tighten this to the concrete apiserver
endpoint/CIDR (for example, the `kubernetes` endpoints in the `default`
namespace).

Leaving E2 at `0.0.0.0/0` still blocks all non-443/6443 egress and is a valid,
functional hardening; tightening the peer is an environment-specific refinement.

### Operand pods (E3)

Fully portable - the operator stamps every managed workload with the
`mcp-server` label (`LabelKeyMCPServer`), so the `Exists` podSelector needs no
per-environment change. No port restriction is applied because the MCP port is
per-`MCPServer` (`spec.config.port`) and varies across servers.

## Required namespace labels

- Any namespace that scrapes metrics must be labeled `metrics: enabled`, or the
  8443 ingress rule will block it.

## CNI enforcement

A `NetworkPolicy` is only enforced if the cluster CNI implements it. On a
non-enforcing CNI (for example, some Kind setups without a policy plugin such as
Calico) the policy is accepted but inert - the operator keeps working, but the
egress restriction provides no protection and deny-based tests pass vacuously.
