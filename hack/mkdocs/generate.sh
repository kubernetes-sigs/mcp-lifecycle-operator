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

# Install crd-ref-docs if not present
if ! command -v crd-ref-docs &> /dev/null; then
    echo "Installing crd-ref-docs..."
    go install github.com/elastic/crd-ref-docs@latest
fi

# Generate the API reference documentation
crd-ref-docs \
    --source-path=./api/v1alpha1 \
    --config=./crd-ref-docs.yaml \
    --renderer=markdown \
    --output-path=./site-src/reference/index.md

echo "API reference documentation generated successfully!"
