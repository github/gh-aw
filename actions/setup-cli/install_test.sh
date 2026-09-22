#!/bin/bash
set +o histexpand
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [ ! -x "$SCRIPT_DIR/install.sh" ]; then
    echo "FAIL: install.sh must be executable" >&2
    exit 1
fi

exec bash "$PROJECT_ROOT/scripts/test-install-script.sh" "$SCRIPT_DIR/install.sh"
