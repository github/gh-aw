#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT=$(mktemp)
trap 'rm -f "$OUTPUT"' EXIT

(cd "$REPO_ROOT" && bash "$SCRIPT_DIR/check-safe-outputs-conformance.sh" >"$OUTPUT" 2>&1) || true

mapfile -t findings < <(grep "IMP-004: Safe output config property is missing" "$OUTPUT" || true)

if [[ ${#findings[@]} -ne 0 ]]; then
    echo "FAIL: Expected no safe-output config schema gaps"
    printf '  %s\n' "${findings[@]}"
    exit 1
fi

if ! grep -q "IMP-004: All safe output config properties are declared in the schema" "$OUTPUT"; then
    echo "FAIL: Expected IMP-004 complete schema coverage result"
    exit 1
fi

echo "PASS: IMP-004 resolves referenced safe-output schemas"
