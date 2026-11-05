---
env:
  FSTAR_BIN: /tmp/gh-aw/agent/fstar-tools/fstar/bin/fstar.exe
  FSTAR_Z3: /tmp/gh-aw/agent/fstar-tools/fstar/lib/fstar/z3-5.3.0/bin/z3
  FSTAR_SHA256: a716153960545bc26b9d093f55614fedc716a202e4daa1665e5c63e4a2fb04e4
steps:
  - name: Prepare pinned F* and compatible Z3
    shell: bash
    run: bash scripts/install-fstar.sh "$FSTAR_SHA256"
---

<!--
Import shared/fstar.md for checksum-verified F* and its compatible Z3 on Linux
x64. Tools live under /tmp/gh-aw/agent so the agent sandbox can access them.
The consuming repository must include scripts/install-fstar.sh. This import
does not grant tools, network access, or safe outputs. Custom jobs must install
their own tools.
-->

{{#import .github/aw/formal-methods.md}}
