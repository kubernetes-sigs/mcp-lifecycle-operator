#!/usr/bin/env bash
# Convenience script to set up a kind cluster, deploy the operator,
# and run the TLS validation e2e tests.
#
# Usage:
#   ./hack/run-e2e-tls.sh            # full setup + test + teardown
#   ./hack/run-e2e-tls.sh --no-setup # skip cluster creation (reuse existing)
#   ./hack/run-e2e-tls.sh --no-teardown # keep cluster after tests
#
# Requires: kind, kubectl, go, and a container engine (docker or podman).
# Set CONTAINER_TOOL=podman to use podman instead of docker.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

SETUP=true
TEARDOWN=true

for arg in "$@"; do
  case "$arg" in
    --no-setup)   SETUP=false ;;
    --no-teardown) TEARDOWN=false ;;
    -h|--help)
      echo "Usage: $0 [--no-setup] [--no-teardown]"
      exit 0
      ;;
    *)
      echo "Unknown argument: $arg"
      exit 1
      ;;
  esac
done

cd "${ROOT_DIR}"

if [[ "${SETUP}" == "true" ]]; then
  echo "==> Setting up kind cluster and deploying operator..."
  make setup-test-e2e
  make deploy-test-e2e
fi

echo "==> Running TLS validation e2e tests..."
set +e
go test -tags=e2e ./test/e2e/ -v -count=1 -timeout 30m -run "TestTLS"
EXIT_CODE=$?
set -e

if [[ "${TEARDOWN}" == "true" ]]; then
  echo "==> Tearing down kind cluster..."
  make cleanup-test-e2e
fi

exit ${EXIT_CODE}
