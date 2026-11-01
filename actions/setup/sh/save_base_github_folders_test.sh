#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec node --test --test-name-pattern='save:' "${SCRIPT_DIR}/base_github_folders_test.cjs"
