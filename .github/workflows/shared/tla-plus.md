---
env:
  JAVA_BIN: /tmp/gh-aw/agent/tla-tools/jre/bin/java
  TLA2TOOLS_JAR: /tmp/gh-aw/agent/tla-tools/tla2tools.jar
steps:
  - name: Prepare pinned TLA+ runtime
    if: github.ref == format('refs/heads/{0}', github.event.repository.default_branch)
    shell: bash
    run: |
      set -euo pipefail
      trusted=$(mktemp -d "$RUNNER_TEMP/tla-installer.XXXXXX")
      trap 'rm -rf "$trusted"' EXIT
      mkdir -p "$trusted/.github/scripts" "$trusted/actions/setup/sh"
      cp "$GITHUB_WORKSPACE/.github/scripts/install_tla_runtime.sh" "$trusted/.github/scripts/"
      cp "$GITHUB_WORKSPACE/actions/setup/sh/release_integrity.sh" "$trusted/actions/setup/sh/"
      printf '%s  %s\n' \
        15317bc27032f3c8160cb47fb77084683e21e3c37680db90b9bb2e57adea16e2 "$trusted/.github/scripts/install_tla_runtime.sh" \
        b9371403efbe21a02ce8fb6be09a132ff094eced00d48bace2e3ebad4d9fd7d5 "$trusted/actions/setup/sh/release_integrity.sh" | sha256sum --check --strict
      bash "$trusted/.github/scripts/install_tla_runtime.sh" /tmp/gh-aw/agent/tla-tools
      # runner-guard:ignore RGS-018 -- Default-branch installer and integrity helper are SHA-256 verified before execution.
---

<!--
Import shared/tla-plus.md to install checksum-verified Temurin JRE 21 and
TLA+ Tools v1.7.4 (TLC 2.19) before the agent runs on a Linux x64 runner. The portable runtime
and jar live under /tmp/gh-aw/agent so they are accessible in the sandbox.
This fragment does not grant tools, network access, or safe outputs.
Custom jobs do not inherit these agent setup steps.
The setup runs only on the configured default branch, where the scripts and hashes are
reviewed together; non-default refs skip setup instead of executing checkout-controlled copies.
-->

{{#import .github/aw/formal-methods.md}}
