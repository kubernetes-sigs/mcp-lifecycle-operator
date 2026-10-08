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
# The workflow commit (mike tooling, mkdocs.yml with the mike provider, deploy
# scripts) stays in place. Docs content is taken from:
#   1. source_ref, if provided
#   2. else <version> when that ref includes hack/mkdocs/deploy.sh
#   3. else the workflow commit (bootstrap for tags that predate mike)
#
# Always keeps the workflow-branch copies of:
#   mkdocs.yml, hack/mkdocs/deploy.sh, hack/mkdocs/gh-pages-netlify.toml,
#   hack/mkdocs/image/requirements.txt

set -euo pipefail

SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${SCRIPT_ROOT}"

VERSION="${1:?version required}"
SOURCE_REF="${2:-}"

ref_has_deploy_tooling() {
  local ref="$1"
  git cat-file -e "${ref}:hack/mkdocs/deploy.sh" 2>/dev/null
}

resolve_content_ref() {
  if [[ -n "${SOURCE_REF}" ]]; then
    echo "${SOURCE_REF}"
    return
  fi
  if git rev-parse --verify "${VERSION}^{commit}" >/dev/null 2>&1 && ref_has_deploy_tooling "${VERSION}"; then
    echo "${VERSION}"
    return
  fi
  echo ""
}

CONTENT_REF="$(resolve_content_ref)"

if [[ -z "${CONTENT_REF}" ]]; then
  echo "Building docs from the workflow revision (labeling as mike version ${VERSION})."
  echo "Tag/ref '${VERSION}' is missing mike deploy tooling or was not provided as source_ref."
  exit 0
fi

echo "Loading documentation sources from ${CONTENT_REF} (mike version ${VERSION})..."

# Preserve workflow-branch tooling that must not be overwritten by older tags.
TOOLING_BACKUP="$(mktemp -d)"
trap 'rm -rf "${TOOLING_BACKUP}"' EXIT
cp mkdocs.yml "${TOOLING_BACKUP}/mkdocs.yml"
cp hack/mkdocs/deploy.sh "${TOOLING_BACKUP}/deploy.sh"
cp hack/mkdocs/gh-pages-netlify.toml "${TOOLING_BACKUP}/gh-pages-netlify.toml"
cp hack/mkdocs/image/requirements.txt "${TOOLING_BACKUP}/requirements.txt"
cp hack/mkdocs/prepare-version-sources.sh "${TOOLING_BACKUP}/prepare-version-sources.sh"

# Replace site-src and api exactly so files added after CONTENT_REF do not leak in.
git rm -rq --ignore-unmatch site-src api
git checkout "${CONTENT_REF}" -- site-src api crd-ref-docs.yaml hack/mkdocs/generate.sh

cp "${TOOLING_BACKUP}/mkdocs.yml" mkdocs.yml
cp "${TOOLING_BACKUP}/deploy.sh" hack/mkdocs/deploy.sh
cp "${TOOLING_BACKUP}/gh-pages-netlify.toml" hack/mkdocs/gh-pages-netlify.toml
cp "${TOOLING_BACKUP}/requirements.txt" hack/mkdocs/image/requirements.txt
cp "${TOOLING_BACKUP}/prepare-version-sources.sh" hack/mkdocs/prepare-version-sources.sh

echo "Documentation sources ready."
