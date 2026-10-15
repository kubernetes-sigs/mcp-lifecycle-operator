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

# Prepare the working tree to build docs for a mike version.
#
# Usage:
#   prepare-version-sources.sh <version> [source_ref]
#
# Keep workflow tooling in place and load docs/API content from source_ref,
# or from <version> when no override is supplied. Missing content refs fail
# rather than publishing workflow content under a historical version name.

set -euo pipefail

SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${SCRIPT_ROOT}"

VERSION="${1:?version required}"
SOURCE_REF="${2:-}"

CONTENT_REF="${SOURCE_REF:-${VERSION}}"

# Validate required sources before changing the working tree.
if ! git rev-parse --verify "${CONTENT_REF}^{commit}" >/dev/null 2>&1; then
  echo "ERROR: Documentation source ref '${CONTENT_REF}' does not exist. Fetch it or provide source_ref explicitly." >&2
  exit 1
fi
for path in site-src api; do
  if ! git cat-file -e "${CONTENT_REF}:${path}" 2>/dev/null; then
    echo "ERROR: Documentation source ref '${CONTENT_REF}' is missing ${path}. Provide a source_ref containing docs and API sources." >&2
    exit 1
  fi
done

# Older tags may lack API-reference build tooling. Keep the workflow copy of
# each missing file, while always using the selected ref's docs and API types.
CONTENT_PATHS=(site-src api)
for path in crd-ref-docs.yaml hack/mkdocs/generate.sh; do
  if git cat-file -e "${CONTENT_REF}:${path}" 2>/dev/null; then
    CONTENT_PATHS+=("${path}")
  else
    echo "WARNING: ${CONTENT_REF} lacks ${path}; using workflow tooling." >&2
  fi
done

echo "Loading documentation sources from ${CONTENT_REF} (mike version ${VERSION})..."

# Replace site-src and api exactly so files added after CONTENT_REF do not leak in.
git rm -rq --ignore-unmatch site-src api
git checkout "${CONTENT_REF}" -- "${CONTENT_PATHS[@]}"

echo "Documentation sources ready."
