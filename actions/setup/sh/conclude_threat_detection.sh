#!/usr/bin/env bash
set +o histexpand

set -euo pipefail

# The setup action stages shell and JavaScript entry points side by side.
exec node "${BASH_SOURCE[0]%/*}/conclude_threat_detection.cjs" "$@"
