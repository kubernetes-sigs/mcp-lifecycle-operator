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

# Deploy a documentation version to gh-pages with mike.
#
# Usage:
#   deploy.sh <version> [--title <title>] [--set-default] [alias...]
#
# Examples:
#   deploy.sh v0.1.0 latest --set-default          # release (updates latest alias)
#   deploy.sh main --title "main (preview)"        # main-branch preview

set -euo pipefail

SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${SCRIPT_ROOT}"

VERSION="${1:?version required}"
shift

SET_DEFAULT=false
TITLE=""
UPDATE_ALIASES=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --set-default)
      SET_DEFAULT=true
      shift
      ;;
    --title)
      TITLE="${2:?title required with --title}"
      shift 2
      ;;
    *)
      UPDATE_ALIASES+=("$1")
      shift
      ;;
  esac
done

echo "Generating API reference documentation..."
make api-ref-docs

MIKE_ARGS=(deploy --push "${VERSION}" --alias-type=redirect)

if [[ -n "${TITLE}" ]]; then
  MIKE_ARGS+=(-t "${TITLE}")
fi

if [[ ${#UPDATE_ALIASES[@]} -gt 0 ]]; then
  MIKE_ARGS+=(--update-aliases "${UPDATE_ALIASES[@]}")
fi

echo "Deploying documentation version ${VERSION}..."
mike "${MIKE_ARGS[@]}"

if [[ "${SET_DEFAULT}" == "true" ]]; then
  echo "Setting default version to latest..."
  mike set-default --push latest
fi

NETLIFY_CONFIG="${SCRIPT_ROOT}/hack/mkdocs/gh-pages-netlify.toml"
WORKTREE_DIR="$(mktemp -d)"

echo "Ensuring Netlify configuration on gh-pages..."
git fetch origin gh-pages
git worktree add -B gh-pages "${WORKTREE_DIR}" origin/gh-pages
cp "${NETLIFY_CONFIG}" "${WORKTREE_DIR}/netlify.toml"

pushd "${WORKTREE_DIR}" > /dev/null
if ! git diff --quiet netlify.toml; then
  git add netlify.toml
  git commit -m "Ensure Netlify configuration for gh-pages publishing"
  git push origin gh-pages
fi
popd > /dev/null

git worktree remove "${WORKTREE_DIR}" --force

echo "Documentation deployment complete."
