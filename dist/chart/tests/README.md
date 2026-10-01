# Helm Chart Unit Tests

This directory contains unit tests for the mcp-lifecycle-operator Helm chart using [helm-unittest](https://github.com/helm-unittest/helm-unittest).

## Overview

The tests verify that the Helm chart templates render correctly across different value combinations and configurations. They ensure that:

- Templates produce valid Kubernetes resources
- Values are correctly propagated to the rendered manifests
- Conditional logic works as expected
- Default values produce a working configuration

## Test Structure

Tests are organized by resource type:

- **`default_values_test.yaml`** - Validates that all resources render correctly with default values
- **`deployment_test.yaml`** - Tests the Deployment resource including image configuration, resources, scheduling, and leader election
- **`serviceaccount_test.yaml`** - Tests ServiceAccount creation and naming
- **`rbac_test.yaml`** - Tests RBAC resources (Roles, RoleBindings, ClusterRoles, ClusterRoleBindings)
- **`crd_test.yaml`** - Tests CRD rendering and the `crd.enable` and `crd.keep` flags
- **`metrics_test.yaml`** - Tests metrics Service configuration
- **`pod_metadata_test.yaml`** - Tests pod annotations and labels

## Running Tests

### Prerequisites

Install the helm-unittest plugin:

```bash
make install-helm-unittest
```

Or manually:

```bash
helm plugin install https://github.com/helm-unittest/helm-unittest.git
```

### Run All Tests

```bash
make helm-test
```

### Run Tests with Verbose/Debug Output

```bash
make helm-test-verbose
```

This enables debug logging showing test suite parsing and execution details.

### Run Tests with Debug Output and Rendered Templates

```bash
make helm-test-debug
```

This shows debug logging plus the actual rendered YAML templates for debugging.

### Run Specific Test File

```bash
helm unittest dist/chart -f 'tests/deployment_test.yaml'
```

## Test Scenarios

### Default Values

Tests that the chart renders all expected resources with default configuration:
- Deployment with 1 replica
- ServiceAccount
- RBAC resources (ClusterRole, ClusterRoleBinding, Role, RoleBinding)
- Metrics Service
- CRD

### Image Configuration

Tests custom image settings:
- Custom image repository
- Custom image tag
- Custom pull policy

### ServiceAccount

Tests ServiceAccount configuration:
- Default naming
- Custom naming with `nameOverride`
- Custom naming with `fullnameOverride`

### RBAC

Tests RBAC resource creation:
- Manager ClusterRole and ClusterRoleBinding
- Leader election Role and RoleBinding
- Proper subject references

### Leader Election

Tests leader election configuration:
- Default `--leader-elect` argument
- Custom arguments override

### Scheduling

Tests pod scheduling configuration:
- `nodeSelector` values
- `tolerations` array
- `affinity` rules

### Resources

Tests container resource configuration:
- Custom CPU and memory limits
- Custom CPU and memory requests

### Pod Metadata

Tests pod template metadata:
- Default annotations
- Custom annotations via `manager.podAnnotations`
- Default labels
- Custom labels via `manager.podLabels`

### Metrics

Tests metrics service configuration:
- Enabled by default
- Disabled with `metrics.enable: false`
- Custom port with `metrics.port`

### CRD

Tests CRD installation:
- Enabled by default with `crd.enable: true`
- Disabled with `crd.enable: false`
- Keep annotation with `crd.keep: true`

## Writing New Tests

### Test File Structure

```yaml
suite: test description
templates:
  - path/to/template.yaml
tests:
  - it: should do something
    set:
      key: value
    asserts:
      - isKind:
          of: ResourceKind
      - equal:
          path: metadata.name
          value: expected-name
```

### Common Assertions

- `isKind` - Verify resource kind
- `equal` - Check exact value match
- `contains` - Check array contains value
- `isNull` - Verify field is null
- `isNotNull` - Verify field exists
- `hasDocuments` - Check number of rendered documents

### Testing Multiple Templates

```yaml
suite: test multiple resources
templates:
  - template1.yaml
  - template2.yaml
tests:
  - it: should render both resources
    asserts:
      - hasDocuments:
          count: 2
```

### Testing Conditional Rendering

```yaml
tests:
  - it: should not render when disabled
    set:
      feature.enable: false
    asserts:
      - hasDocuments:
          count: 0
```

## Reference

- [helm-unittest Documentation](https://github.com/helm-unittest/helm-unittest/blob/main/DOCUMENT.md)
- [Assertion Types](https://github.com/helm-unittest/helm-unittest/blob/main/DOCUMENT.md#assertion-types)
- [Kueue Helm Tests](https://github.com/kubernetes-sigs/kueue/tree/main/charts/kueue/tests) - Reference implementation from kubernetes-sigs

## CI Integration

These tests can be integrated into CI pipelines:

```yaml
# GitHub Actions example
- name: Run Helm tests
  run: |
    helm plugin install https://github.com/helm-unittest/helm-unittest.git
    helm unittest dist/chart --color
```

## Troubleshooting

### Plugin Not Found

If you get "plugin not found" errors:

```bash
helm plugin list
helm plugin install https://github.com/helm-unittest/helm-unittest.git
```

### Test Failures

Run with verbose output to see detailed information:

```bash
make helm-test-verbose
```

Or with debug output to see rendered templates:

```bash
make helm-test-debug
```

### Template Rendering Issues

Test individual templates:

```bash
helm template test-release dist/chart --debug