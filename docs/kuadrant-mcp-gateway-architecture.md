# Kuadrant mcp-gateway Architecture

This document explains how the Kuadrant mcp-gateway works end-to-end and how the
mcp-lifecycle-operator integrates with it. It covers the components, setup flow,
and request path in detail.

## Components

### mcp-gateway-controller (operator)

An operator that watches for MCPGatewayExtension and MCPServerRegistration
resources. When it sees an MCPGatewayExtension, it creates the data plane
components (Deployment, Service, EnvoyFilter, HTTPRoute) described below. It
typically runs in a shared operator namespace (e.g., `openshift-operators`).

### mcp-gateway (data plane)

A pod created by the mcp-gateway-controller **in the namespace where the
MCPGatewayExtension lives**. It runs a single binary with two servers:

- **HTTP broker** on port 8080 - the public MCP endpoint. Aggregates tools from
  all registered upstream MCP servers into a single MCP session. Clients talk to
  this.
- **gRPC ext_proc** on port 50051 - an envoy external processor. Envoy calls
  this for every HTTP request before making a routing decision.

### Gateway (envoy)

A standard Gateway API resource backed by a GatewayClass implementation (e.g.,
Istio). When you create a Gateway, the implementation (Istio) spins up an envoy
proxy pod and a cluster-internal Service. Envoy handles all HTTP routing based on
HTTPRoutes.

### mcp-lifecycle-operator

Watches MCPServer resources. For each MCPServer with a `kuadrant` gateway
provider, it creates an MCPGatewayBinding, a per-server HTTPRoute, and an
MCPServerRegistration.

## Setup Flow

### Prerequisites

1. A Gateway implementation (e.g., Istio) is installed and a GatewayClass is
   available.
2. The mcp-gateway-controller is running.
3. The mcp-lifecycle-operator is running.

### Step 1: Create the Gateway

Create a Gateway with two listeners (dual-listener pattern). This example uses
HTTP for local/Kind development; production deployments should use HTTPS with
TLS termination at the gateway:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: mcp-gateway
  namespace: gateway-system
spec:
  gatewayClassName: istio
  listeners:
  - name: mcp
    port: 80
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: All
  - name: mcps
    hostname: "*.mcp.local"
    port: 80
    protocol: HTTP
    allowedRoutes:
      namespaces:
        from: All
```

- **`mcp`** - catch-all listener, no hostname restriction. Accepts any Host
  header. Used for public traffic.
- **`mcps`** - wildcard listener. Only accepts requests with Host headers
  matching `*.mcp.local`. Used for internal per-server routing.

The Gateway implementation (Istio) creates an envoy proxy pod and a Service. On
cloud platforms this Service gets an external load balancer (ELB).

### Step 2: Create the MCPGatewayExtension

```yaml
apiVersion: mcp.kuadrant.io/v1alpha1
kind: MCPGatewayExtension
metadata:
  name: mcp-gateway-extension
  namespace: mcp-system
spec:
  publicHost: <ELB hostname>
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: mcp-gateway
    namespace: gateway-system
    sectionName: mcp
```

- **`targetRef.sectionName: mcp`** - points to the catch-all listener. The
  mcp-gateway-controller creates its broker HTTPRoute on this listener.
- **`publicHost`** - the public address clients will use (typically the ELB
  hostname). Required here because the selected `mcp` listener has no hostname;
  `mcps` is used for per-server routes.

The mcp-gateway-controller sees this and creates four resources:

#### a) Deployment + Service (in the extension's namespace)

A pod running the mcp-gateway binary. The Service exposes ports 8080 (HTTP
broker) and 50051 (gRPC ext_proc). The pod is started with:
- `--mcp-gateway-public-host=<ELB>` - the public address
- `--mcp-gateway-private-host=<gateway-svc>:80` - the gateway's cluster-internal
  Service address, used for hair-pinning (see [Request Flow](#request-flow))

#### b) EnvoyFilter (in the gateway's namespace)

An Istio-specific resource that modifies envoy's configuration:

> "Add an `ext_proc` filter to envoy's HTTP processing chain on port 80. The
> filter calls `mcp-gateway.<extension-ns>.svc.cluster.local:50051` via gRPC.
> Insert it as the FIRST filter - before any routing."

Once envoy picks this up, **every HTTP request** on port 80 passes through the
ext_proc. There is no condition - it is hardwired for all traffic.

#### c) Broker HTTPRoute (in the extension's namespace)

```yaml
spec:
  hostnames: ["<ELB hostname>"]
  parentRefs:
    - kind: Gateway
      name: mcp-gateway
      namespace: gateway-system
      sectionName: mcp               # catch-all listener
  rules:
    - matches:
        - path: { value: /mcp }
      backendRefs:
        - name: mcp-gateway
          port: 8080                  # HTTP broker
```

This tells envoy: when a request arrives with the ELB hostname and path `/mcp`,
route it to the broker. It attaches to the `mcp` catch-all listener, so the ELB
hostname (which does not match `*.mcp.local`) is accepted.

#### d) Config Secret (in the extension's namespace)

Contains a `config.yaml` listing all registered MCP servers - hostnames,
prefixes, and internal URLs. Updated whenever MCPServerRegistrations are created
or deleted.

### Step 3: Create an MCPServer

```yaml
apiVersion: mcp.x-k8s.io/v1beta1
kind: MCPServer
metadata:
  name: example-mcp-server
  namespace: kuadrant-example
spec:
  source:
    type: ContainerImage
    containerImage:
      ref: quay.io/containers/kubernetes_mcp_server:latest
  config:
    port: 8080
    path: /mcp
  gateway:
    provider: kuadrant
    configRef: mcp-gateway-config
```

The ConfigMap `mcp-gateway-config` tells the lifecycle operator which extension
to use:

```yaml
data:
  extension-name: mcp-gateway-extension
  extension-namespace: mcp-system
  # section-name: mcps    # optional — auto-discovered from Gateway wildcard listener when omitted
  # prefix: example_      # optional — auto-generated from MCPServer name + namespace when omitted
```

The **mcp-lifecycle-operator** creates three resources:

#### a) MCPGatewayBinding

Tracks the registration state of this MCPServer with the gateway.

#### b) Per-server HTTPRoute (in the MCPServer's namespace)

```yaml
spec:
  hostnames: ["example-mcp-server.kuadrant-example.mcp.local"]
  parentRefs:
    - kind: Gateway
      name: mcp-gateway
      namespace: gateway-system
      sectionName: mcps             # wildcard listener
  rules:
    - backendRefs:
        - name: example-mcp-server
          port: 8080
```

The hostname is auto-constructed from the wildcard listener:
`<server-name>.<namespace>.mcp.local`. The route attaches to the `mcps` listener
whose `*.mcp.local` pattern matches. The `sectionName` is resolved as follows:
if the ConfigMap sets `section-name`, that value is used; otherwise the operator
auto-discovers the Gateway's wildcard listener (exactly one must exist). The
extension's `targetRef.sectionName` is never used for per-server routes — it
controls the broker route only.

#### c) MCPServerRegistration (in the MCPServer's namespace)

Tells the mcp-gateway-controller about the new server. The controller updates the
config Secret, and the broker picks it up - tools prefixed with `example_` now
map to `example-mcp-server.kuadrant-example.mcp.local`.

## Request Flow

### Simple request (tools/list)

```
1. Client sends POST http://<ELB>/mcp
   Host: <ELB hostname>
   Body: {"jsonrpc":"2.0", "method":"tools/list", "id":1}

2. ELB forwards to envoy (gateway pod in gateway-system).

3. Envoy runs its filter chain. The first filter is ext_proc.
   Envoy opens a gRPC stream to mcp-gateway:50051 and sends the
   request headers.

4. The ext_proc inspects the headers, possibly modifies them,
   and tells envoy to continue.

5. Envoy does normal HTTP routing:
   Host <ELB hostname> + path /mcp -> broker HTTPRoute -> mcp-gateway:8080

6. The broker parses the MCP JSON-RPC message. For tools/list, it
   aggregates tool lists from all registered servers and returns them.

7. Response: broker -> envoy -> ELB -> client.
```

### Tool call (hair-pinning)

```
1. Client sends POST http://<ELB>/mcp
   Body: {"method":"tools/call", "params":{"name":"example_get_pods"}}

2-5. Same as above. Request reaches the broker.

6. Broker looks up the tool name. The prefix "example_" maps to the
   server registered at example-mcp-server.kuadrant-example.mcp.local.
   The broker strips the prefix - upstream knows it as "get_pods".

7. Broker sends a NEW request back through the gateway:
   POST http://mcp-gateway-istio.gateway-system.svc.cluster.local:80/mcp
   Host: example-mcp-server.kuadrant-example.mcp.local
   Body: {"method":"tools/call", "params":{"name":"get_pods"}}

   The destination is the gateway's cluster-internal Service (not the ELB).
   This is the "hair-pin" - the request re-enters the same gateway from
   inside the cluster.

8. Envoy receives the internal request. The ext_proc sees it again
   (it sees all traffic). Envoy routes based on Host:
   Host *.mcp.local -> mcps listener -> per-server HTTPRoute -> MCP server pod:8080

9. The MCP server pod executes get_pods and returns the result.

10. Response: MCP server -> envoy -> broker -> envoy -> ELB -> client.
```

### Why hair-pinning?

The broker does not connect to MCP server pods directly. Instead it routes
through the gateway using the cluster-internal Service. This means:

- Routing is handled by envoy and HTTPRoutes - the broker does not need to track
  pod IPs or service endpoints.
- Gateway-level policies (rate limiting, auth, TLS) apply to internal traffic
  too.
- The per-server HTTPRoutes provide hostname-based isolation between servers.

## Dual-Listener Pattern

| Listener | Hostname | Accepts | Used by |
|----------|----------|---------|---------|
| `mcp` | *(none)* | Any Host header | Broker HTTPRoute (ELB hostname) |
| `mcps` | `*.mcp.local` | Only `*.mcp.local` | Per-server HTTPRoutes (hair-pin traffic) |

Two listeners are needed because:

1. The broker HTTPRoute uses the ELB hostname (e.g.,
   `a7b5eb...elb.amazonaws.com`), which does not match `*.mcp.local`. It needs a
   listener with no hostname restriction.
2. Per-server HTTPRoutes use auto-constructed hostnames
   (`server.namespace.mcp.local`). A wildcard listener gives each server its own
   hostname for clean routing on the internal hair-pin hop.

### How the lifecycle operator's ConfigMap relates

```
MCPGatewayExtension
  targetRef.sectionName: mcp     <- which listener the broker route uses
                                    (mcp-gateway-controller creates this)

ConfigMap
  section-name: mcps             <- which listener per-server routes use
                                    (lifecycle operator creates these)
                                    auto-discovered from Gateway wildcard
                                    listener when omitted
```

The extension's `sectionName` and the ConfigMap's `section-name` serve different
purposes. The extension's `sectionName` controls the broker route (public
traffic). The ConfigMap's `section-name` controls per-server routes (internal
routing). When `section-name` is omitted, the operator auto-discovers the
Gateway's wildcard listener — if exactly one exists, it is selected; otherwise
an error guides the user to set `section-name` explicitly.
