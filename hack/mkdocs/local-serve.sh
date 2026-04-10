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

# Script to run MkDocs locally using a Python virtual environment
# This avoids conflicts with system Python on macOS

set -e
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT_DIR="$( cd "${SCRIPT_DIR}/../.." && pwd )"
VENV_DIR="${ROOT_DIR}/.venv-mkdocs"

cd "${ROOT_DIR}"

# Create virtual environment if it doesn't exist
if [ ! -d "${VENV_DIR}" ]; then
    echo "Creating Python virtual environment..."
    python3 -m venv "${VENV_DIR}"
fi

# Activate virtual environment
echo "Activating virtual environment..."
source "${VENV_DIR}/bin/activate"

# Install/upgrade dependencies
echo "Installing MkDocs dependencies..."
pip install -q --upgrade pip
pip install -q -r hack/mkdocs/image/requirements.txt

# Generate API docs
echo "Generating API documentation..."
make api-ref-docs

# Start MkDocs server
echo ""
echo "Starting MkDocs development server..."
echo "Documentation will be available at: http://127.0.0.1:3000"
echo "Press Ctrl+C to stop"
echo ""

python -m mkdocs serve --dev-addr=127.0.0.1:3000
