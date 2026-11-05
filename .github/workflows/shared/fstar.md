---
env:
  FSTAR_VERSION: 2026.10.25
  FSTAR_SHA256: a716153960545bc26b9d093f55614fedc716a202e4daa1665e5c63e4a2fb04e4
  FSTAR_BIN: /tmp/gh-aw/agent/fstar-tools/fstar/bin/fstar.exe
  FSTAR_Z3: /tmp/gh-aw/agent/fstar-tools/fstar/lib/fstar/z3-5.3.0/bin/z3
steps:
  - name: Prepare pinned F* and compatible Z3
    shell: bash
    run: |
      set -euo pipefail
      if [[ "$(uname -s)" != Linux || "$(uname -m)" != x86_64 ]]; then
        echo "The pinned F* toolchain requires Linux x86_64." >&2
        exit 1
      fi
      tools=/tmp/gh-aw/agent/fstar-tools
      mkdir -p "$tools"
      archive="$tools/fstar.tar.gz"
      curl --fail --location --silent --show-error \
        --retry 2 --connect-timeout 10 --max-time 240 \
        --output "$archive" \
        "https://github.com/FStarLang/FStar/releases/download/v${FSTAR_VERSION}/fstar-v${FSTAR_VERSION}-Linux-x86_64.tar.gz"
      printf '%s  %s\n' "$FSTAR_SHA256" "$archive" | sha256sum --check --strict
      tar -xzf "$archive" --no-same-owner -C "$tools"
      "$FSTAR_BIN" --version | tee "$tools/fstar-version.txt"
      grep -Fx "F* $FSTAR_VERSION" "$tools/fstar-version.txt"
      "$FSTAR_Z3" -version | tee "$tools/z3-version.txt"
      grep -Fx 'Z3 version 5.3.0 - 64 bit' "$tools/z3-version.txt"
      smoke=$(mktemp -d "$tools/smoke.XXXXXX")
      trap 'rm -rf "$smoke"' EXIT
      printf '%s\n' 'module Smoke' 'let increment (n:nat) : Tot (r:nat{r == n + 1}) = n + 1' > "$smoke/Smoke.fst"
      "$FSTAR_BIN" --smt "$FSTAR_Z3" --z3version 5.3.0 --cache_off --z3rlimit 50 \
        --message_format human "$smoke/Smoke.fst"
---

<!--
Import shared/fstar.md for checksum-verified F* and its compatible Z3 on Linux
x64. Tools live under /tmp/gh-aw/agent so the agent sandbox can access them.
Update FSTAR_VERSION and resolve its matching release SHA-256 for FSTAR_SHA256.
This import does not grant tools, network access, or safe outputs. Custom jobs
must install their own tools.
-->

{{#import .github/aw/formal-methods.md}}
