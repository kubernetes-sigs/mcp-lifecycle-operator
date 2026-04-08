# Release v0.1.0

**Release branch:** `release-0.1`

## Changelog

First release. Key features:

**Core:**
- Initial MCP Lifecycle Operator implementation — declarative API (`MCPServer` CRD, `v1alpha1`)
  to deploy, manage, and roll out MCP Servers on Kubernetes
- Reconciler manages Deployments and Services for MCPServer resources
- Server-side apply (SSA) for all status updates

**API Features:**
- Container image, port, and args configuration
- ConfigMap mounting with configurable path and volume name
- Secret mounting with secretKey field support
- Environment variables and envFrom (with secret/configmap reference validation)
- Resource requirements and limits
- Liveness and readiness probes
- Storage mounts with EmptyDir support and duplicate path validation
- Security context at pod and container level (restricted Pod Security Standard defaults)
- Configurable replicas (including scale-to-zero)
- Path configuration with detailed validation for routing
- Status address URL with configurable path

**Operational:**
- Production logging by default
- Aggregation labels on clusterroles (admin, editor, viewer)
- CloudBuild configuration for k8s-staging image publishing
- CI: lint, unit tests, e2e tests (pinned to commit SHAs)
- Dependabot for automated dependency updates

**Dependencies:**
- Go 1.25.8
- controller-runtime v0.23.3
- Kubernetes API v0.35.2

## Checklist

- [ ] All OWNERS must LGTM the release proposal
- [ ] Verify the changelog above is up-to-date
- [x] Create the release branch
  `release-0.1` exists (branched at `1c39a19`), cherry-pick PR #78 merged.
- [x] Verify the [postsubmit image-pushing job](https://github.com/kubernetes/test-infra/blob/master/config/jobs/image-pushing/k8s-staging-mcp-lifecycle-operator.yaml)
  covers `release-0.1` — the existing `^release-` pattern matches; presubmits run on all branches
- [ ] Verify Go version in Prow job image matches `go.mod`
  **Note:** Prow config currently uses `golang:1.24`, but `go.mod` declares `go 1.25.8`.
  This must be aligned before proceeding.
- [ ] Update `config/manager/kustomization.yaml` on the release branch: pin
  `newTag` to `v0.1.0`
  - [ ] Submit PR against `release-0.1`
- [x] Ensure all CI (lint, unit tests, e2e) passes on the release branch
  Lint: 0 issues. Unit tests: pass (controller 81.6% coverage). Generated manifests up to date.
- [ ] An OWNER creates a signed tag:
  ```bash
  git tag -s -m "mcp-lifecycle-operator release v0.1.0" v0.1.0
  ```
- [ ] An OWNER pushes the tag:
  ```bash
  git push upstream v0.1.0
  ```
  This triggers Cloud Build to build and publish the staging image.
- [ ] Submit PR to
  [kubernetes/k8s.io](https://github.com/kubernetes/k8s.io) updating
  `registry.k8s.io/images/k8s-staging-mcp-lifecycle-operator/images.yaml`
  to promote the container image to production
  **Note:** An image promotion entry already exists but targets `1c39a19`. Update to
  point to the final branch tip after pinning + tagging.
  - [ ] Wait for merge and verify image availability:
    ```bash
    crane manifest registry.k8s.io/mcp-lifecycle-operator/mcp-lifecycle-operator:v0.1.0
    ```
- [ ] Generate the install manifest and include it among the release assets:
  ```bash
  IMG=registry.k8s.io/mcp-lifecycle-operator/mcp-lifecycle-operator:v0.1.0 make build-installer
  ```
- [ ] Create [GitHub release](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/new)
  with the changelog above; attach `dist/install.yaml` as a release asset
- [ ] Send announcement email to `dev@kubernetes.io` with subject:
  `[ANNOUNCE] mcp-lifecycle-operator v0.1.0 is released`
- [ ] Close this issue
