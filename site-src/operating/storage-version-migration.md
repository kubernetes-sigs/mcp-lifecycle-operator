# Storage Version Migration

The `MCPServer` CRD serves both `v1alpha1` and `v1beta1`, with **`v1beta1` as the
storage version**. The conversion webhook converts objects on read, so every
`MCPServer` is always readable as `v1beta1` today.

However, objects written to etcd *before* `v1beta1` became the storage version are
still physically stored as `v1alpha1` bytes. They are only rewritten when something
updates them. Until every stored object is at `v1beta1`:

- the CRD's `status.storedVersions` keeps listing `v1alpha1`, and
- `v1alpha1` **cannot be safely removed** as a served version - the API server
  could no longer decode the stale records.

Storage version migration force-rewrites all stored `MCPServer` objects at the
current storage version.

## Prerequisites

A storage-version-migrator controller must be running in the cluster, and its
`migration.k8s.io/v1alpha1` API must be registered. OpenShift ships this as the
`openshift-kube-storage-version-migrator` operator. On a cluster where that API
is not installed, `kubectl apply` fails with `no matches for kind
"StorageVersionMigration"` - install a storage-version-migrator first.

The operator's **conversion webhook must stay available for the entire run**. The
migrator reads each stored object (still `v1alpha1` bytes), and the API server
routes it through the conversion webhook to re-encode it as `v1beta1`. If the
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

```bash
make migrate-storage
# equivalently:
kubectl apply -k config/storage-migration
```

## Verifying completion

```bash
# Wait for the migration to succeed:
kubectl get storageversionmigration mcpservers-v1beta1 -o yaml
# Look for a status condition of type "Succeeded".
```

The migrator rewrites the stored objects but does **not** update the CRD's
`status.storedVersions`. That field keeps listing `v1alpha1` until it is pruned
explicitly - see below.

## Retrying a failed migration

A `StorageVersionMigration` is a one-shot object: the migrator processes it once
and records the result. Re-running `make migrate-storage` (or `kubectl apply`)
against the unchanged object is a no-op and does **not** re-trigger it. To retry
after a `Failed` result, delete the object and apply it again:

```bash
kubectl delete storageversionmigration mcpservers-v1beta1
make migrate-storage
```

## Removing v1alpha1

Dropping `v1alpha1` as a served version is **gated on this migration completing**
on every cluster that ever stored `v1alpha1` objects. The steps below must run
**in this order**: the API server rejects a CRD whose `spec.versions` drops a
version still listed in `status.storedVersions` (`status.storedVersions[0]:
Invalid value: "v1alpha1": missing from spec.versions`), so the stored version has
to be pruned first; and the validating webhook has to be moved to `v1beta1` before
the `v1alpha1` type is deleted, or admission validation is silently lost. Once the
migration reports `Succeeded`:

1. Prune `v1alpha1` from `status.storedVersions` (the migrator does not do this).
   This is safe only after the migration succeeded, because no object is stored as
   `v1alpha1` any more:

   ```bash
   kubectl patch crd mcpservers.mcp.x-k8s.io --subresource=status --type=merge \
     -p '{"status":{"storedVersions":["v1beta1"]}}'
   ```

2. **Re-register the validating webhook at `v1beta1` first.** The only
   `+kubebuilder:webhook` marker lives on the MCPServer `v1alpha1` type
   (`api/v1alpha1/mcpserver_webhook.go`), and `v1beta1`'s
   `SetupWebhookWithManager` currently wires **conversion only** (no
   `WithValidator`). If you remove the `v1alpha1` type before moving the
   validator, `make manifests` regenerates a `ValidatingWebhookConfiguration`
   with **no `mcpservers` rule** - `MCPServerCustomValidator` silently stops
   enforcing the image allowlist, digest, and label policy, and no test turns
   red. Move `MCPServerCustomValidator` (and its `+kubebuilder:webhook` marker)
   to `v1beta1`, wire it via `WithValidator` in the `v1beta1`
   `SetupWebhookWithManager`, and confirm the regenerated manifest still carries
   the `mcpservers` validating rule before continuing.

3. Drop `v1alpha1` as a served version **in code**, not with a live `kubectl edit
   crd`. The CRD is generated by controller-gen from the kubebuilder markers on
   the API types, so a hand edit to `spec.versions` is reverted on the next `make
   manifests` or operator upgrade. Remove the **MCPServer** `v1alpha1` type and its
   `+kubebuilder` markers only - do **not** delete the whole `api/v1alpha1`
   package: it also holds `MCPGatewayBinding`, whose CRD still has `v1alpha1` as its
   only served and storage version, so removing the package would break it.
   Regenerate the manifests and ship the change so the MCPServer CRD serves
   `v1beta1` only.

4. Confirm only `v1beta1` remains served and stored:

   ```bash
   kubectl get crd mcpservers.mcp.x-k8s.io \
     -o jsonpath='{range .spec.versions[*]}{.name}{" "}{end}{"\n"}{.status.storedVersions}'
   # Expected: v1beta1
   #           ["v1beta1"]
   ```

Do not prune `storedVersions` before the migration has succeeded - the API server
must still be able to decode every stored object at a version that remains.
