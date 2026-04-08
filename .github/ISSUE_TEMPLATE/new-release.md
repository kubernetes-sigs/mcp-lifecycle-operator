---
name: New release
about: Propose a new major or minor release
title: "Release v0.MINOR.0"
labels: release
assignees: aliok, ArangoGutierrez, matzew, mikebrow, mrunalp, soltysh
---

# Release v0.MINOR.0

<!-- Update MINOR throughout before submitting -->

**Release branch:** `release-0.MINOR`

## Changelog

<!-- Summarize user-facing changes since the last release -->

## Checklist

- [ ] All OWNERS must LGTM the release proposal
- [ ] Verify the changelog above is up-to-date
- [ ] Create the release branch
  ```bash
  git branch release-0.MINOR main
  git push upstream release-0.MINOR
  ```
- [ ] Verify the [postsubmit image-pushing job](https://github.com/kubernetes/test-infra/blob/master/config/jobs/image-pushing/k8s-staging-mcp-lifecycle-operator.yaml)
  covers `release-0.MINOR` (the existing `^release-` pattern should match)
- [ ] Verify Go version in Prow job image matches `go.mod`
- [ ] Update `config/manager/kustomization.yaml` on the release branch: pin
  `newTag` to `v0.MINOR.0`
  - [ ] Submit PR against `release-0.MINOR`
- [ ] Ensure all CI (lint, unit tests, e2e) passes on the release branch
- [ ] An OWNER creates a signed tag:
  ```bash
  git tag -s -m "mcp-lifecycle-operator release v0.MINOR.0" v0.MINOR.0
  ```
- [ ] An OWNER pushes the tag:
  ```bash
  git push upstream v0.MINOR.0
  ```
  This triggers Cloud Build to build and publish the staging image.
- [ ] Submit PR to
  [kubernetes/k8s.io](https://github.com/kubernetes/k8s.io) updating
  `registry.k8s.io/images/k8s-staging-mcp-lifecycle-operator/images.yaml`
  to promote the container image to production
  - [ ] Wait for merge and verify image availability:
    ```bash
    crane manifest registry.k8s.io/mcp-lifecycle-operator/mcp-lifecycle-operator:v0.MINOR.0
    ```
- [ ] Generate the install manifest and include it among the release assets:
  ```bash
  IMG=registry.k8s.io/mcp-lifecycle-operator/mcp-lifecycle-operator:v0.MINOR.0 make build-installer
  ```
- [ ] Create [GitHub release](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/new)
  with the changelog above; attach `dist/install.yaml` as a release asset
- [ ] Send announcement email to `dev@kubernetes.io` with subject:
  `[ANNOUNCE] mcp-lifecycle-operator v0.MINOR.0 is released`
- [ ] Close this issue
