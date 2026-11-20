# Storage Version Migration

MCPServer manifests and clients using `mcp.x-k8s.io/v1alpha1` must switch to
`v1beta1` or `v1`. Alpha requests are no longer accepted. This does not affect
the separate `v1alpha1` MCPGatewayBinding API. Storage migration does not update
client manifests. Deploy the v1 admission webhook before disabling alpha serving.

The `MCPServer` CRD serves `v1beta1` and `v1`, with **`v1` as the
storage version**. The conversion webhook makes objects readable through each
served version, but reading an object does not rewrite its bytes in etcd. Objects
written before the promotion may still be stored as `v1alpha1` or `v1beta1`.
The CRD's `status.storedVersions` records versions that may still exist in
storage. Do not remove an older version from the CRD while objects may still be
stored in that version.

Storage version migration force-rewrites all stored `MCPServer` objects at the
current storage version.

Existing `v1alpha1` MCPServers do not need a separate migration to `v1beta1`
first: conversion uses `v1alpha1` → `v1beta1` → `v1` while the migrator rewrites
them. The `v1alpha1` endpoint is no longer served. Clients must use `v1beta1` or
`v1` to access these objects; alpha conversion support remains available for
objects still stored in that version. Deprecated
status data omitted from `v1`, including `status.handshakeRetryCount` and
`status.serverInfo.capabilities.logging`, is lost during the rewrite.

## Prerequisites

A storage-version-migrator controller must be running in the cluster, and its
`migration.k8s.io/v1alpha1` API must be registered to use the bundled manifest
or `make migrate-storage`. OpenShift ships this as the
`openshift-kube-storage-version-migrator` operator. On clusters with the native
`storagemigration.k8s.io/v1` API instead, use the alternative below. See the
[Kubernetes storage migration guide](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/storage-version-migration/)
for native API availability.

Install the operator release that marks `v1` as the CRD storage version before
starting. Check `spec.versions` on the live `mcpservers.mcp.x-k8s.io` CRD;
`v1` must have `storage: true`.

The operator's **conversion webhook must stay available for the entire run**. The
migrator reads each stored object (possibly `v1alpha1` or `v1beta1` bytes), and
the API server routes it through the conversion webhook to re-encode it as `v1`. If the
webhook is unreachable or its serving certificate is invalid while the migration
runs, those writes fail and the migration reports `Failed`. Confirm the operator
is healthy before starting.

The **validating webhook** (`vmcpserver.mcp.x-k8s.io`, `failurePolicy: Fail`) is
also on the write path. The migrator rewrites each object with an `UPDATE`, and
that admission call re-runs `ValidateUpdate`, which validates the *entire* new
object against the current admission policy - not just the diff. An `MCPServer`
that was stored **before** a policy was tightened (for example a newer image
allowlist or a digest-pinning requirement) is therefore re-checked against
today's rules and can be **rejected**, which surfaces as a `Failed` migration.
This is not fixed by delete-and-retry: either bring the stored objects into
compliance first, or relax/grandfather the policy for the duration of the
migration.

## Running the migration

On clusters with the `migration.k8s.io/v1alpha1` API:

```bash
make migrate-storage
# equivalently:
kubectl apply -k config/storage-migration
```

If an earlier `v1beta1` migration left a `mcpservers-v1beta1` request behind, it
is harmless and separate from the new `mcpservers-v1` request. After checking
its result, you can remove the stale request:

```bash
kubectl delete storageversionmigration.migration.k8s.io mcpservers-v1beta1 --ignore-not-found
```

On clusters with the native `storagemigration.k8s.io/v1` API, create a separate
one-shot request. The native API identifies the resource by group and resource;
it does not take a version in `spec.resource`:

```yaml
apiVersion: storagemigration.k8s.io/v1
kind: StorageVersionMigration
metadata:
  name: mcpservers-v1
spec:
  resource:
    group: mcp.x-k8s.io
    resource: mcpservers
```

Apply this manifest after the `v1` CRD storage version is installed. Use the
fully qualified API name when inspecting either migration if both APIs are
available on the cluster.

## Verifying completion

```bash
# For the bundled migration.k8s.io request:
kubectl get storageversionmigration.migration.k8s.io mcpservers-v1 -o yaml
# For the native request:
kubectl get storageversionmigration.storagemigration.k8s.io mcpservers-v1 -o yaml
# Wait for status.conditions to report Succeeded=True.
```

After success, inspect the CRD's `status.storedVersions`. The legacy migrator
does **not** prune old entries; follow the procedure below. The native migrator
may update this field automatically, but still verify it before retiring an old
served version.

## Retrying a failed migration

A `StorageVersionMigration` is a one-shot object: the migrator processes it once
and records the result. Re-running `make migrate-storage` (or `kubectl apply`)
against the unchanged object is a no-op and does **not** re-trigger it. To retry
after a `Failed` result, delete the object and apply it again:

```bash
kubectl delete storageversionmigration.migration.k8s.io mcpservers-v1
make migrate-storage
```

For the native API, delete its `storageversionmigration.storagemigration.k8s.io`
object and reapply the native manifest instead.

## Pruning old stored versions

Once the `v1` migration reports `Succeeded=True`, check the CRD:

```bash
kubectl get crd mcpservers.mcp.x-k8s.io -o jsonpath='{.status.storedVersions}'
```

If `v1alpha1` or `v1beta1` is still listed, prune both entries only after the
migration succeeded on that cluster:

```bash
kubectl patch crd mcpservers.mcp.x-k8s.io --subresource=status --type=merge \
  -p '{"status":{"storedVersions":["v1"]}}'
kubectl get crd mcpservers.mcp.x-k8s.io -o jsonpath='{.status.storedVersions}'
# Expected: ["v1"]
```

Do not prune `storedVersions` before completion: the API server must still be
able to decode every stored object. Pruning this status field does **not** stop
serving `v1alpha1` or `v1beta1` to clients; `spec.versions[*].served` controls
that separately.

## Retiring older served versions

Only remove an old version from `spec.versions` after migration has succeeded
and that version is absent from `status.storedVersions` on every affected
cluster. The CRD is generated from the API types, so make the served-version
change in code and regenerate the manifest; a live edit will be overwritten.

Before disabling `v1alpha1` MCPServer, move its validating webhook marker and
handler to a remaining served version and verify CREATE and UPDATE admission
still works for `v1` and `v1beta1`. This is tracked in
[#556](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/issues/556).

The manager temporarily serves both the old alpha validation endpoint and the
new v1 endpoint. To switch admission without sending requests to an endpoint
that an old manager pod cannot serve:

1. Roll out the new manager image while retaining the existing validating
   webhook configuration and keeping `v1alpha1` served.
2. Wait until all manager pods run the new image and are ready. The manager's
   readiness probe checks that its webhook listener is reachable.
3. Update the validating webhook configuration to target `v1` and its validation
   path. Verify CREATE and UPDATE admission through the remaining served versions.
4. Disable serving `v1alpha1` only after the v1 webhook rule is active.

Applying the whole manifest bundle at once does not enforce this order. If the
installation mechanism cannot stage these steps, plan a maintenance window for
MCPServer writes: calls can be rejected while old pods receive the new path.
This sequence addresses the admission endpoint switch; upgrades that also
introduce v1 CRDs or change storage versions must coordinate those changes with
conversion support separately. Storage migration alone does not switch the
admission endpoint.

The alpha validation handler can be removed in a later release once supported
upgrade paths no longer depend on the old endpoint. This does not imply removal
of alpha conversion support for objects still stored in that version.

Keep the `api/v1alpha1` package for `MCPGatewayBinding`, which still uses it.
Keep `v1beta1` served for its deprecation window; this migration alone is not a
reason to disable it.
