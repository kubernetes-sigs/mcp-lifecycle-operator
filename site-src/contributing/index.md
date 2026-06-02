# Contributing

We welcome contributions from the community. This page explains how to get started and where to find help.

## Getting started

Before contributing, please read the project's [Contributing Guidelines](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/blob/main/CONTRIBUTING.md) in the repository. In particular:

- **[Contributor License Agreement (CLA)](https://git.k8s.io/community/CLA.md)** — You must sign the Kubernetes CLA before we can accept your pull requests.
- **[Kubernetes Contributor Guide](https://k8s.dev/guide)** — Main contributor documentation; you can jump to the [contributing page](https://k8s.dev/docs/guide/contributing/).
- **[Contributor Cheat Sheet](https://k8s.dev/cheatsheet)** — Common resources for existing developers.

The Kubernetes community abides by the CNCF [Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md).

## Development workflow

From the repository root:

- **Build:** `make build`
- **Format:** `make fmt`
- **Lint / vet:** `make lint`
- **Tests:** `make test` (writes `cover.out` for coverage)
- **Test coverage:** `make test` writes `cover.out`. Run `make test-cover` to refresh tests and emit `out/coverage.html` and `out/coverage.txt` (`go tool cover`). With `cover.out` present, `make cover-func` prints a per-function summary and `make cover-html` opens the interactive HTML report in a browser (local). Remove generated artifacts with `make cover-clean`.
- **Generate manifests:** `make manifests generate`

### E2E tests

- **Set up Kind cluster:** `make setup-test-e2e` (creates a Kind cluster with a local container registry via `hack/create-kind-cluster.sh`)
- **Deploy operator:** `make deploy-test-e2e` (builds, pushes to local registry, deploys to Kind)
- **Run e2e tests:** `make test-e2e`
- **Tear down:** `make cleanup-test-e2e`

### OLM bundle

The operator supports installation via [OLM (Operator Lifecycle Manager)](https://olm.operatorframework.io/). The bundle is auto-generated from existing kustomize manifests — only the CSV skeleton in `config/manifests/bases/` is maintained manually.

- **Generate bundle:** `make bundle`
- **Build bundle image:** `make bundle-build`
- **Push bundle image:** `make bundle-push`

To run OLM bundle e2e tests (tests all four install modes: OwnNamespace, SingleNamespace, MultiNamespace, AllNamespaces):

- **Deploy:** `make deploy-test-e2e-bundle` (creates Kind cluster with registry, builds/pushes images, installs OLM)
- **Run tests:** `make test-e2e-bundle`

After making changes, open a pull request on GitHub. Ensure CI passes and address any review feedback.

## Mentorship

- [Mentoring Initiatives](https://k8s.dev/community/mentoring) — Kubernetes offers mentorship programs and is always looking for volunteers.

## Bug reports

Bug reports should be filed as [GitHub Issues](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/issues/new) on this repo.

## Communications

- [Slack channel (#sig-apps)](https://kubernetes.slack.com/messages/sig-apps)
- [Mailing List](https://groups.google.com/a/kubernetes.io/g/sig-apps)

For meeting schedules and additional community information, see the [SIG Apps README](https://github.com/kubernetes/community/blob/main/sig-apps/README.md).
