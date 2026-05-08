# Metrics

The MCP Lifecycle Operator exposes **Prometheus** metrics from the **controller manager** (the operator Deployment that reconciles `MCPServer` resources). Metrics include those registered by [controller-runtime](https://book.kubebuilder.io/reference/metrics.html) (for example workqueues and the Kubernetes API client) and **custom `mcpserver_*` series** documented below.

This page is aimed at **platform and cluster operators** who scrape Prometheus and tune alerting—not at authors of `MCPServer` manifests alone.

## Metrics endpoint

Metrics are exposed over **HTTPS** at path **`/metrics`** on **port `8443`** on the controller manager metrics **Service**.

After a typical install from the [release `install.yaml`](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/latest), scrape:

[https://mcp-lifecycle-operator-controller-manager-metrics-service.mcp-lifecycle-operator-system.svc:8443/metrics](https://mcp-lifecycle-operator-controller-manager-metrics-service.mcp-lifecycle-operator-system.svc:8443/metrics)

Adjust the Service name and namespace if you change the Kustomize `namePrefix` / `namespace` when deploying.

### Tuning the metrics listen address (advanced)

If you installed from the [release `install.yaml`](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/latest), metrics are already available on **`:8443`** with HTTPS—you can use the scrape URL above and skip this section.

If you **customize the operator Deployment**, check how the manager container sets its `args`. The repository’s sample patch shows what a typical install uses:

[`config/default/manager_metrics_patch.yaml`](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/blob/main/config/default/manager_metrics_patch.yaml)

Those `args` correspond to the following flags:

| Flag | Default | Description |
| --- | --- | --- |
| `--metrics-bind-address` | `0` (disabled) | Address to serve metrics on. Set to **`:8443`** for HTTPS or **`:8080`** for HTTP. The sample patch above [sets this to `:8443`](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/blob/main/config/default/manager_metrics_patch.yaml). |
| `--metrics-secure` | `true` | Serve metrics over HTTPS. Set to **`false`** for plain HTTP. |

## Custom metrics

Custom metrics use the Prometheus namespace **`mcpserver`** (exported names start with **`mcpserver_`**).

| Metric | Type | Description |
| --- | --- | --- |
| `mcpserver_condition_info` | gauge | Current **Accepted** / **Ready** condition snapshot per `MCPServer`. Value is always `1`; filter by labels. |
| `mcpserver_validation_failures_total` | counter | Total permanent configuration validation failures (`ValidationError`). |
| `mcpserver_deployment_failures_total` | counter | Total failures when reconciling the workload Deployment (`reason` is currently `ReconcileError`). |
| `mcpserver_service_failures_total` | counter | Total failures when reconciling the Service (`reason` is currently `ReconcileError`). |
| `mcpserver_reconcile_phase_duration_seconds` | histogram | Duration of reconciliation phases **validation**, **deployment**, and **service** (seconds; default Prometheus histogram buckets). |

### Labels for `mcpserver_condition_info`

| Label | Description |
| --- | --- |
| `name` | `MCPServer` name |
| `namespace` | `MCPServer` namespace |
| `type` | Condition type: `Accepted` or `Ready` |
| `status` | `True`, `False`, or `Unknown` |
| `reason` | Condition reason (intended to mirror `.status.conditions[]`; see [Gauge versus API status](#gauge-versus-api-status)) |

Only one active series exists per `(name, namespace, type)`. On delete, both gauge series and **`*_failures_total` counter series** for that object are removed from the exporter.

**Typical reasons**

| `type` | Typical `reason` values | `status` notes |
| --- | --- | --- |
| `Accepted` | `Valid`, `Invalid` | Usually `True` or `False` |
| `Ready` | `Available`, `ConfigurationInvalid`, `DeploymentUnavailable`, `ServiceUnavailable`, `ScaledToZero`, `Initializing`, `MCPEndpointUnavailable` | May be `Unknown` (for example `Initializing` while the Deployment has not reported conditions yet) |

### Gauge versus API status

In rare cases the **`mcpserver_condition_info` gauge** can **diverge** from what you see in **`MCPServer.status.conditions`**. When investigating correctness, treat **`MCPServer.status` as the source of truth**.

- **Permanent validation error** — `Ready` / `ConfigurationInvalid` may appear in the API only after a successful status write, while the gauge updated earlier or on a different path.
- **MCP handshake** — after `Available`, a failed handshake can set status to `MCPEndpointUnavailable` without a second gauge update in the same reconcile.

**Example queries**

```promql
sum by (namespace, type, status, reason) (mcpserver_condition_info)
```

```promql
sum by (reason) (mcpserver_condition_info{type="Ready", status="False"})
```

```promql
sum(rate(mcpserver_validation_failures_total[5m])) by (namespace)
```

```promql
histogram_quantile(
  0.99,
  sum(rate(mcpserver_reconcile_phase_duration_seconds_bucket[5m])) by (le, phase)
)
```

### Labels for failure counters (`mcpserver_*_failures_total`)

`mcpserver_validation_failures_total`, `mcpserver_deployment_failures_total`, and `mcpserver_service_failures_total` share the **same label set**:

| Label | Description |
| --- | --- |
| `name` | `MCPServer` name |
| `namespace` | `MCPServer` namespace |
| `reason` | Depends on which counter (see below) |

**`reason` values**

- **`mcpserver_validation_failures_total`** — permanent validation errors currently use `Invalid`.
- **`mcpserver_deployment_failures_total`** and **`mcpserver_service_failures_total`** — currently `ReconcileError` when the corresponding reconcile step returns an error.

### Labels for `mcpserver_reconcile_phase_duration_seconds`

| Label | Description |
| --- | --- |
| `phase` | Reconciliation phase: `validation`, `deployment`, or `service` |

Histogram time series use the usual `_bucket`, `_sum`, and `_count` suffixes for quantiles and averages.

## Prometheus Operator

If you use the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator), apply a `ServiceMonitor` that selects the controller-manager metrics Service. Example:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: controller-manager-metrics-monitor
  namespace: mcp-lifecycle-operator-system   # namespace where the operator runs
  labels:
    control-plane: controller-manager
    app.kubernetes.io/name: mcp-lifecycle-operator
spec:
  endpoints:
    - path: /metrics
      port: https
      scheme: https
      bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
      tlsConfig:
        insecureSkipVerify: true   # tighten for production (e.g. cert-manager); see repo sample
  selector:
    matchLabels:
      control-plane: controller-manager
      app.kubernetes.io/name: mcp-lifecycle-operator
```

The repository maintains the full sample at [`config/prometheus/monitor.yaml`](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/blob/main/config/prometheus/monitor.yaml). Wire it into your install by uncommenting the **`[PROMETHEUS]`** resource (`../prometheus`) in [`config/default/kustomization.yaml`](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/blob/main/config/default/kustomization.yaml), or apply an equivalent manifest alongside kube-prometheus-stack. Add labels your Prometheus `ServiceMonitor` selector expects (for example `release: prometheus`).

## Next steps

- **[Introduction](../introduction.md)** — Architecture and `MCPServer` overview (including status conditions)
- **[Quickstart](../guides/quickstart.md)** — Deploy an MCP server and inspect status
- **[Contributing](../contributing/index.md)** — How to contribute
