#!/bin/bash

set -euo pipefail

CLUSTER_NAME=""
CONTAINER_TOOL="docker"
REGISTRY_NAME="kind-registry"
REGISTRY_PORT="5001"

while [[ $# -gt 0 ]]; do
  case $1 in
    --name)
      CLUSTER_NAME="$2"
      shift 2
      ;;
    --container-tool)
      CONTAINER_TOOL="$2"
      shift 2
      ;;
    --registry-port)
      REGISTRY_PORT="$2"
      shift 2
      ;;
    *)
      echo "Unknown argument: $1"
      echo "Usage: $0 [--name <cluster-name>] [--container-tool <docker|podman>] [--registry-port <port>]"
      exit 1
      ;;
  esac
done

CLUSTER_NAME_ARG=""
if [ -n "${CLUSTER_NAME}" ]; then
  CLUSTER_NAME_ARG="--name ${CLUSTER_NAME}"
fi

# Start a local registry if not already running.
if [ "$(${CONTAINER_TOOL} inspect -f '{{.State.Running}}' "${REGISTRY_NAME}" 2>/dev/null || true)" != 'true' ]; then
  echo "Starting local registry on port ${REGISTRY_PORT}..."
  ${CONTAINER_TOOL} run -d --restart=always -p "127.0.0.1:${REGISTRY_PORT}:5000" --name "${REGISTRY_NAME}" registry:2
fi

# Delete existing cluster if it exists.
kind delete cluster ${CLUSTER_NAME_ARG} 2>/dev/null || true

# Create Kind cluster with containerd registry config path enabled.
echo "Creating Kind cluster..."
cat <<EOF | kind create cluster ${CLUSTER_NAME_ARG} --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
EOF

# Tell containerd on each node to reach localhost:<port> via HTTP on the registry container.
NODES=$(kind get nodes ${CLUSTER_NAME_ARG} 2>/dev/null)
for node in ${NODES}; do
  ${CONTAINER_TOOL} exec "${node}" mkdir -p "/etc/containerd/certs.d/localhost:${REGISTRY_PORT}"
  cat <<EOF | ${CONTAINER_TOOL} exec -i "${node}" cp /dev/stdin "/etc/containerd/certs.d/localhost:${REGISTRY_PORT}/hosts.toml"
[host."http://${REGISTRY_NAME}:5000"]
EOF
done

# Connect the registry to the Kind network if not already connected.
# Kind always uses a single Docker network called "kind" for all clusters.
if [ "$(${CONTAINER_TOOL} inspect -f='{{json .NetworkSettings.Networks.kind}}' "${REGISTRY_NAME}")" = 'null' ]; then
  echo "Connecting registry to Kind network..."
  ${CONTAINER_TOOL} network connect "kind" "${REGISTRY_NAME}"
fi

# Document the local registry for tooling that understands KEP-1755.
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REGISTRY_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF

echo "Kind cluster ready with local registry at localhost:${REGISTRY_PORT}"