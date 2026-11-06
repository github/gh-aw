---
env:
  JAVA_BIN: /tmp/gh-aw/agent/tla-tools/jre/bin/java
  TLA2TOOLS_JAR: /tmp/gh-aw/agent/tla-tools/tla2tools.jar
steps:
  - name: Prepare pinned TLA+ runtime
    shell: bash
    run: |
      set -euo pipefail
      trusted=$(mktemp -d "$RUNNER_TEMP/tla-installer.XXXXXX")
      trap 'rm -rf "$trusted"' EXIT
      mkdir -p "$trusted/.github/scripts" "$trusted/actions/setup/sh"
      cp "$GITHUB_WORKSPACE/.github/scripts/install_tla_runtime.sh" "$trusted/.github/scripts/"
      cp "$GITHUB_WORKSPACE/actions/setup/sh/release_integrity.sh" "$trusted/actions/setup/sh/"
      printf '%s  %s\n' \
        b6e2054d1df3a27795b60c5f35b655a038a6b4140e8b401369e6cb03016b8d32 "$trusted/.github/scripts/install_tla_runtime.sh" \
        306fb2fce7cb05d44b3cb32a227b2e549c5b81c2f7be3fc26deeacbea1d7eb0c "$trusted/actions/setup/sh/release_integrity.sh" | sha256sum --check --strict
      bash "$trusted/.github/scripts/install_tla_runtime.sh" /tmp/gh-aw/agent/tla-tools
---

<!--
Import shared/tla-plus.md to install checksum-verified Temurin JRE 21 and
TLA+ Tools v1.7.4 (TLC 2.19) before the agent runs on a Linux x64 runner. The portable runtime
and jar live under /tmp/gh-aw/agent so they are accessible in the sandbox.
This fragment does not grant tools, network access, or safe outputs.
Custom jobs do not inherit these agent setup steps.
The installer and helper are copied from the checkout and verified against reviewed
workflow-owned hashes before execution, not trusted merely because they are in the checkout.
-->

{{#import .github/aw/formal-methods.md}}
