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
- **CI coverage:** The test workflow uploads `cover.out` to [Codecov](https://codecov.io/gh/kubernetes-sigs/mcp-lifecycle-operator) on pull requests and pushes to `main`, tagged with the `unittests` flag. Codecov enforces a minimum project coverage of 70% (with 1% slack) and 80% coverage on changed lines in each PR.
- **E2E code coverage:** In addition to unit-test coverage, the E2E suite can measure how much production code the end-to-end tests exercise. This runs against a live Kind cluster with a coverage-instrumented operator image, so it is scheduled nightly (and can be triggered via `workflow_dispatch`) rather than on every PR. To reproduce locally: `make deploy-test-e2e-cover` then `make test-e2e-cover`. This builds the `coverage` image target (`go build -cover` on a busybox base), deploys it via the `config/coverage` overlay (which adds a writable `GOCOVERDIR` volume), runs the E2E tests, copies the coverage data out of the running pod with `kubectl cp`, and writes a profile to `out/e2e-coverage.out`. The manager flushes coverage counters once at startup and then every 10s while running, because a `-cover` binary otherwise only emits data on clean exit; the upfront flush also means collection (which copies data out of the still-running pod) works even for a short suite. This flushing logic lives in `cmd/coverage.go` behind the `e2ecoverage` build tag and is compiled only into the coverage image; the production build compiles the no-op stub in `cmd/coverage_noop.go`, so it carries no coverage code or `runtime/coverage` import. `make test` runs the untagged production build (which compiles `cmd/coverage_noop.go`) and then a second, tag-scoped `go test -tags e2ecoverage ./cmd/...` so `cmd/coverage.go` is unit-tested too; both profiles (`cover.out` and `cover-e2ecoverage.out`) are uploaded under the `unittests` flag. In CI the E2E profile is uploaded to Codecov under the `e2e` flag, so E2E and unit coverage can be compared side by side. The coverage data lives on an `emptyDir` volume, so if the operator pod restarts mid-run (e.g. OOM) the pre-restart coverage is lost; the collected numbers reflect only the current pod's lifetime. See [#177](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/issues/177).
- **Generate manifests:** `make manifests generate`

After making changes, open a pull request on GitHub. Ensure CI passes and address any review feedback.

## Mentorship

- [Mentoring Initiatives](https://k8s.dev/community/mentoring) — Kubernetes offers mentorship programs and is always looking for volunteers.

## Bug reports

Bug reports should be filed as [GitHub Issues](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/issues/new) on this repo.

## Communications

- [Slack channel (#sig-apps)](https://kubernetes.slack.com/messages/sig-apps)
- [Mailing List](https://groups.google.com/a/kubernetes.io/g/sig-apps)

For meeting schedules and additional community information, see the [SIG Apps README](https://github.com/kubernetes/community/blob/main/sig-apps/README.md).
