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

# Generate the API reference documentation
"${CRD_REF_DOCS}" \
    --source-path=./api/v1alpha1 \
    --config=./crd-ref-docs.yaml \
    --renderer=markdown \
    --output-path=./site-src/reference/index.md

echo "API reference documentation generated successfully!"
