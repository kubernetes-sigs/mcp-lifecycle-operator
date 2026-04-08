#!/bin/sh

set -e

# Run mkdocs command
if [ "$1" = "build" ]; then
    echo "Building MkDocs site..."
    exec python -m mkdocs build
elif [ "$1" = "serve" ]; then
    echo "Starting MkDocs development server..."
    shift  # Remove 'serve' from arguments
    exec python -m mkdocs serve "$@"
else
    # Run custom command
    exec "$@"
fi
