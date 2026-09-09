#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT=$(mktemp)
trap 'rm -f "$OUTPUT"' EXIT

(cd "$REPO_ROOT" && bash "$SCRIPT_DIR/check-safe-outputs-conformance.sh" >"$OUTPUT" 2>&1) || true

mapfile -t findings < <(grep "IMP-004: Safe output config property is missing" "$OUTPUT" || true)
expected="IMP-004: Safe output config property is missing from schema: safe-outputs.ado-update-work-item.status"

if [[ ${#findings[@]} -ne 1 || "${findings[0]}" != *"$expected"* ]]; then
    echo "FAIL: Expected only the genuine ado-update-work-item.status schema gap"
    printf '  %s\n' "${findings[@]}"
    exit 1
fi

echo "PASS: IMP-004 resolves referenced safe-output schemas"
