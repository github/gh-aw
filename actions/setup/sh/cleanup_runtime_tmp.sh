#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=actions/setup/sh/release_integrity.sh
source "${SCRIPT_DIR}/release_integrity.sh"

cleanup_awf_runtime_tmp
