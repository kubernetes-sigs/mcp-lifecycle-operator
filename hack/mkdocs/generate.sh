#!/usr/bin/env bash

# Copyright 2026 The Kubernetes Authors.
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
# See the License for the specific language governing permissions and
# limitations under the License.

set -e
set -u
set -o pipefail

SCRIPT_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

cd "${SCRIPT_ROOT}"

echo "Generating API reference documentation..."

# Set up the binary directory
GOPATH_DIR="$(go env GOPATH)"
BIN_DIR="${GOPATH_DIR}/bin"

# Ensure the bin directory exists
mkdir -p "${BIN_DIR}"

# Set GOBIN explicitly for the install command
export GOBIN="${BIN_DIR}"

CRD_REF_DOCS="${BIN_DIR}/crd-ref-docs"

# Install crd-ref-docs if not present
if [ ! -f "${CRD_REF_DOCS}" ]; then
    echo "Installing crd-ref-docs to ${BIN_DIR}..."
    go install github.com/elastic/crd-ref-docs@latest

    # Verify installation succeeded
    if [ ! -f "${CRD_REF_DOCS}" ]; then
        echo "ERROR: crd-ref-docs installation failed. Binary not found at ${CRD_REF_DOCS}"
        echo "GOPATH: ${GOPATH_DIR}"
        echo "GOBIN: ${GOBIN}"
        echo "Contents of ${BIN_DIR}:"
        ls -la "${BIN_DIR}" || echo "Directory ${BIN_DIR} does not exist"
        exit 1
    fi
    echo "Successfully installed crd-ref-docs"
fi

# Keep versions on separate pages: Markdown type anchors collide when both
# versions are rendered in one file. Prefer v1 for the index, while still
# supporting historical source refs that contain only older API versions.
PRIMARY_PAGE=true
ALPHA_REFERENCE_PAGE=
for VERSION in v1 v1beta1 v1alpha1; do
    if [ ! -d "./api/${VERSION}" ]; then
        continue
    fi
    OUTPUT_PATH="./site-src/reference/${VERSION}.md"
    CONFIG_PATH=./crd-ref-docs.yaml
    if [ "${VERSION}" = v1alpha1 ]; then
        CONFIG_PATH=./hack/mkdocs/crd-ref-docs-v1alpha1-mcpserver.yaml
    fi
    if [ "${PRIMARY_PAGE}" = true ]; then
        OUTPUT_PATH=./site-src/reference/index.md
    fi
    if [ "${VERSION}" = v1alpha1 ]; then
        ALPHA_REFERENCE_PAGE="${OUTPUT_PATH##*/}"
    fi
    "${CRD_REF_DOCS}" \
        --source-path="./api/${VERSION}" \
        --config="${CONFIG_PATH}" \
        --renderer=markdown \
        --output-path="${OUTPUT_PATH}"
    PRIMARY_PAGE=false
done

if [ -f ./api/v1alpha1/mcpgatewaybinding_types.go ]; then
    "${CRD_REF_DOCS}" \
        --source-path=./api/v1alpha1 \
        --config=./hack/mkdocs/crd-ref-docs-v1alpha1-mcpgatewaybinding.yaml \
        --renderer=markdown \
        --output-path=./site-src/reference/mcpgatewaybinding.md

    # The binding reference includes shared alpha types that refer to
    # MCPServerSpec and MCPServerStatus. Those definitions live on the alpha
    # MCPServer page after filtering the two resource kinds apart.
    sed -i \
        -e "s|](#mcpserverspec)|](${ALPHA_REFERENCE_PAGE}#mcpserverspec)|g" \
        -e "s|](#mcpserverstatus)|](${ALPHA_REFERENCE_PAGE}#mcpserverstatus)|g" \
        ./site-src/reference/mcpgatewaybinding.md
fi

echo "API reference documentation generated successfully!"
