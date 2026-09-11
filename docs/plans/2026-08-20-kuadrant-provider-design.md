# Kuadrant Provider Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an in-tree Kuadrant gateway provider that creates HTTPRoute + MCPServerRegistration resources for MCP servers.

**Architecture:** Independent reconciler following the same provider pattern as the HTTPRoute provider. Watches MCPGatewayBinding with `spec.provider == "kuadrant"`, creates two owned resources per binding: an HTTPRoute (routing traffic from the Kuadrant MCP Gateway to the MCPServer Service) and an MCPServerRegistration (registering the server with the Kuadrant gateway broker).

**Tech Stack:** controller-runtime, Gateway API v1, Kuadrant MCP Gateway CRDs (mcp.kuadrant.io/v1alpha1)

---

## Design Decisions

### MCPServerRegistration Types

Local minimal Go types (not imported from Kuadrant) because:
- The `mcp-gateway` repo is not published as a Go module on pkg.go.dev
- The `kuadrant-operator` module doesn't contain MCPServerRegistration
- Importing the full operator module would add heavy transitive deps
- The types are simple flat structs (~85 lines total including DeepCopy)
- A comment in the source explains why local types are used

### ConfigMap Keys

| Key                 | Required | Default | Description                                      |
|---------------------|----------|---------|--------------------------------------------------|
| `gateway-name`      | Yes      | —       | Gateway resource name                            |
| `gateway-namespace` | Yes      | —       | Gateway resource namespace                       |
| `hostname`          | Yes      | —       | Hostname matching Gateway's listener wildcard    |
| `prefix`            | Yes      | —       | Tool/prompt name prefix for federation           |
| `section-name`      | No       | `mcps`  | Gateway listener section name                    |

### MCPServerRegistration Fields

- `spec.targetRef` — references the HTTPRoute by name (same namespace)
- `spec.prefix` — set if ConfigMap provides it, omitted otherwise
- `spec.path` — from `mcpServer.Spec.Config.Path` (default `/mcp`)
- `spec.state` — always `Enabled`

### MCPServer Not Found

Both kuadrant and httproute providers return early (`ctrl.Result{}, nil`) when the MCPServer is not found, since the MCPGatewayBinding has an owner reference to the MCPServer and garbage collection will clean it up.

### CRD Detection

At startup, check both HTTPRoute and MCPServerRegistration CRDs via REST mapper. If either is missing, log and skip — no error.

### E2e Tests

Deferred to a follow-up. Kuadrant requires Istio (not Envoy Gateway), so needs a separate e2e target/workflow. Unit tests via envtest provide coverage for now.

---

## File Layout

```
internal/controller/providers/kuadrant/
├── types.go            # MCPServerRegistration types + scheme registration
├── controller.go       # Reconciler, init() registration, SetupWithManager
├── controller_test.go  # envtest-based unit tests
└── suite_test.go       # Test suite setup
```

## Additional Changes

- `internal/controller/providers/httproute/controller.go` — Fix MCPServer-not-found to return early (align with kuadrant)
- `cmd/main.go` — Blank import for kuadrant package init() registration
- `site-src/guides/gateway.md` — Document the kuadrant provider (ConfigMap keys, prerequisites)

## RBAC

The kuadrant controller needs markers for:
- `mcp.kuadrant.io/mcpserverregistrations` — get, list, watch, create, update, delete
- `gateway.networking.k8s.io/httproutes` — get, list, watch, create, update, delete
- `mcp.x-k8s.io/mcpgatewaybindings` — get, list, watch + status/finalizers
- `mcp.x-k8s.io/mcpservers` — get, list, watch
- `core/configmaps` — get, list, watch

---

## Tasks

### Task 1: Create local Kuadrant types

**Files:** Create `internal/controller/providers/kuadrant/types.go`

- MCPServerRegistration, MCPServerRegistrationSpec, TargetReference structs
- MCPServerRegistrationList for scheme registration
- DeepCopyObject/DeepCopyInto implementations
- SchemeBuilder + AddToScheme
- Comment explaining why local types instead of import

### Task 2: Implement kuadrant reconciler

**Files:** Create `internal/controller/providers/kuadrant/controller.go`

- init() with providers.Register("kuadrant", Setup)
- Reconciler struct with Client + Scheme
- Reconcile: get binding → get MCPServer (return early if not found) → get ConfigMap → validate keys → build HTTPRoute (with sectionName) → build MCPServerRegistration → create-or-update both → check accepted → set status
- SetupWithManager: CRD checks for both HTTPRoute and MCPServerRegistration, watch binding + owns HTTPRoute + owns MCPServerRegistration + watches ConfigMap + watches MCPServer
- RBAC markers
- Helper functions: setNotRegistered, updateBindingStatus, findBindingsForConfigMap, findBindingsForMCPServer

### Task 3: Fix HTTPRoute provider MCPServer-not-found

**Files:** Modify `internal/controller/providers/httproute/controller.go`

- Change MCPServer-not-found from setNotRegistered to return early

### Task 4: Add blank import in main.go

**Files:** Modify `cmd/main.go`

- Add `_ "github.com/kubernetes-sigs/mcp-lifecycle-operator/internal/controller/providers/kuadrant"`

### Task 5: Write unit tests

**Files:** Create `internal/controller/providers/kuadrant/suite_test.go`, `controller_test.go`

- Suite setup with envtest (register Kuadrant types + Gateway API CRDs)
- Test: creates HTTPRoute and MCPServerRegistration from valid binding + ConfigMap
- Test: sets Registered=False when ConfigMap missing required keys
- Test: updates resources when ConfigMap changes
- Test: sets optional prefix when present
- Test: defaults sectionName to mcps
- Test: uses custom sectionName when provided
- Test: returns early when MCPServer not found

### Task 6: Update documentation

**Files:** Modify `site-src/guides/gateway.md`

- Add a new section for the `kuadrant` provider (parallel to the existing `httproute` section)
- Document prerequisites (Kuadrant MCP Gateway + Istio)
- Document ConfigMap format with all keys (required/optional)
- Document what resources are created (HTTPRoute + MCPServerRegistration)
- Document CRD auto-detection behavior

### Task 7: Verify

- `make generate manifests` to regenerate RBAC
- `make test` to run all tests