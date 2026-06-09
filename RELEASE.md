# Release Process

The MCP Lifecycle Operator is released on an as-needed basis. The process is as follows:

## Issue Templates

Two GitHub issue templates are provided under `.github/ISSUE_TEMPLATE/`:

- **new-release.md** - for major/minor releases (creates a release branch, runs
  the full checklist)
- **new-patch-release.md** - for patch releases (cherry-picks to an existing
  release branch)

See `docs/release-v0.1.0-issue.md` for a concrete example of a completed
release issue.

## Process

1. Open a release issue using the appropriate template, filling in the changelog
   since the previous release.
1. All [OWNERS](OWNERS) must LGTM the release proposal.
1. Create (or verify) the release branch `release-0.MINOR` and push it to
   upstream.
1. Verify the [postsubmit image-pushing job](https://github.com/kubernetes/test-infra/blob/master/config/jobs/image-pushing/k8s-staging-mcp-lifecycle-operator.yaml)
   covers the release branch (the existing `^release-` pattern should match).
1. Verify the Go version in the Prow job image matches `go.mod`.
1. Pin the image tag in `config/manager/kustomization.yaml` on the release
   branch (submit a PR against the release branch).
1. Ensure all CI (lint, unit tests, e2e) passes on the release branch.
1. An OWNER creates a signed tag and pushes it:
   ```bash
   git tag -s -m "mcp-lifecycle-operator release $VERSION" $VERSION
   git push upstream $VERSION
   ```
   Pushing the tag triggers Cloud Build to build and publish the staging image.
1. Submit a PR to
   [kubernetes/k8s.io](https://github.com/kubernetes/k8s.io) updating
   `registry.k8s.io/images/k8s-staging-mcp-lifecycle-operator/images.yaml` to
   promote the container image to production.
   Wait for merge and verify image availability:
   ```bash
   crane manifest registry.k8s.io/mcp-lifecycle-operator/mcp-lifecycle-operator:$VERSION
   ```
1. Generate the install manifest:
   ```bash
   IMG=registry.k8s.io/mcp-lifecycle-operator/mcp-lifecycle-operator:$VERSION make build-installer
   ```
1. Create a [GitHub release](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/new)
   with the changelog; attach `dist/install.yaml` as a release asset.
1. Send an announcement email to `dev@kubernetes.io` with subject:
   `[ANNOUNCE] mcp-lifecycle-operator $VERSION is released`
1. Close the release issue.
