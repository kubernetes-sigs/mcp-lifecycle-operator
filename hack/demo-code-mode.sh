#!/usr/bin/env bash
#
# Copyright 2026 The Kubernetes Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#
# End-to-end demo of code mode with the "agentsandbox" sandbox provider.
#
# It builds the two data-plane images (the dispatcher and the mcplo-agentsandbox
# sandbox image, which bundles sandboxd + the runner), optionally loads them into
# a kind cluster, installs the CRDs, runs the operator with the image flags
# wired, applies the example MCPToolset, waits for it to go Ready, and prints how
# to call execute_code with the bundled demo client.
#
# The agentsandbox provider drives executions through agent-sandbox Sandboxes, so
# code mode no longer runs on core Kubernetes alone: this script installs the
# pinned agent-sandbox release (controller + CRDs) into the cluster before
# applying the toolset. Set SKIP_AGENT_SANDBOX_INSTALL if it is already present,
# or AGENT_SANDBOX_VERSION to pin a different release
# (https://github.com/kubernetes-sigs/agent-sandbox).
#
# Usage:
#   REGISTRY=quay.io/matzew ./hack/demo-code-mode.sh            # push to registry
#   LOAD_KIND=1 KIND_CLUSTER=codemode ./hack/demo-code-mode.sh  # load into kind, no push
#
# Env knobs:
#   REGISTRY       image registry prefix          (default: quay.io/matzew)
#   TAG            image tag                       (default: demo)
#   LOAD_KIND      if set, kind-load instead of push
#   KIND_CLUSTER   kind cluster name               (default: codemode)
#   CONTAINER_TOOL podman|docker                   (default: podman)
#   NAMESPACE      demo namespace                  (default: default)
#   RUN_OPERATOR   if set, run the operator locally (make run) at the end
#   AGENT_SANDBOX_VERSION       agent-sandbox release to install (default: v1.0.4)
#   SKIP_AGENT_SANDBOX_INSTALL  if set, assume agent-sandbox is already installed
set -euo pipefail

REGISTRY="${REGISTRY:-quay.io/matzew}"
TAG="${TAG:-demo}"
CONTAINER_TOOL="${CONTAINER_TOOL:-podman}"
KIND_CLUSTER="${KIND_CLUSTER:-codemode}"
NAMESPACE="${NAMESPACE:-default}"
# Keep this default in lockstep with AGENT_SANDBOX_VERSION in the Makefile so the
# installed controller matches the sandboxd baked into AGENTSANDBOX_IMG.
AGENT_SANDBOX_VERSION="${AGENT_SANDBOX_VERSION:-v1.0.4}"

DISPATCHER_IMG="${REGISTRY}/mcplo-execute-code:${TAG}"
# The sandbox image the operator bakes into the SandboxTemplate (passed as
# --runner-image): sandboxd + the portable runner.
AGENTSANDBOX_IMG="${REGISTRY}/mcplo-agentsandbox:${TAG}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

say() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }

say "Building data-plane images with ${CONTAINER_TOOL}"
make docker-build-dispatcher   DISPATCHER_IMG="$DISPATCHER_IMG"     CONTAINER_TOOL="$CONTAINER_TOOL"
make docker-build-agentsandbox AGENTSANDBOX_IMG="$AGENTSANDBOX_IMG" CONTAINER_TOOL="$CONTAINER_TOOL"

if [[ -n "${LOAD_KIND:-}" ]]; then
  say "Loading images into kind cluster '${KIND_CLUSTER}'"
  kind load docker-image "$DISPATCHER_IMG"   --name "$KIND_CLUSTER"
  kind load docker-image "$AGENTSANDBOX_IMG" --name "$KIND_CLUSTER"
else
  say "Pushing images to ${REGISTRY}"
  "$CONTAINER_TOOL" push "$DISPATCHER_IMG"
  "$CONTAINER_TOOL" push "$AGENTSANDBOX_IMG"
fi

if [[ -z "${SKIP_AGENT_SANDBOX_INSTALL:-}" ]]; then
  say "Installing agent-sandbox ${AGENT_SANDBOX_VERSION} (controller + CRDs)"
  kubectl apply --server-side -f \
    "https://github.com/kubernetes-sigs/agent-sandbox/releases/download/${AGENT_SANDBOX_VERSION}/sandbox-with-extensions.yaml"
  kubectl -n agent-sandbox-system rollout status deploy/agent-sandbox-controller --timeout=120s
else
  say "Skipping agent-sandbox install (SKIP_AGENT_SANDBOX_INSTALL set); assuming it is present"
fi

say "Installing CRDs"
make install

say "Applying the example MCPToolset (provider: agentsandbox)"
kubectl apply -f examples/mcptoolset-ops.yaml

cat <<EOF

Images built:
  dispatcher:   ${DISPATCHER_IMG}
  agentsandbox: ${AGENTSANDBOX_IMG}

Next: run the operator with the image flags wired, e.g.

  DISPATCHER_IMAGE=${DISPATCHER_IMG} \\
  RUNNER_IMAGE=${AGENTSANDBOX_IMG} \\
  make run

(or deploy the operator image with the same two env vars set).

Then wait for the toolset and grab its endpoint:

  kubectl -n ${NAMESPACE} wait --for=condition=Ready mcptoolset/ops-toolset --timeout=120s
  kubectl -n ${NAMESPACE} get mcptoolset ops-toolset -o jsonpath='{.status.executeEndpoint}{"\\n"}'

Call execute_code with the bundled demo client:

  kubectl -n ${NAMESPACE} port-forward svc/ops-toolset-execute 8080:8080 &
  go run ./hack/code-mode-demo -endpoint http://localhost:8080/mcp -toolset ops-toolset

Watch a fresh pod per execution (SC-005):

  kubectl -n ${NAMESPACE} get pods -l mcp.x-k8s.io/toolset=ops-toolset --watch

Prove the operator is off the hot path (SC-006) - scale it to zero, call again:

  kubectl scale deploy/<operator-deploy> --replicas=0
  go run ./hack/code-mode-demo -endpoint http://localhost:8080/mcp -toolset ops-toolset   # still works
EOF

if [[ -n "${RUN_OPERATOR:-}" ]]; then
  say "Running the operator (make run) with image flags wired - Ctrl-C to stop"
  DISPATCHER_IMAGE="$DISPATCHER_IMG" RUNNER_IMAGE="$AGENTSANDBOX_IMG" make run
fi
