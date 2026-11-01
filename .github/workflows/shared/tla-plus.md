---
env:
  JAVA_BIN: /tmp/gh-aw/agent/tla-tools/jre/bin/java
  TLA2TOOLS_JAR: /tmp/gh-aw/agent/tla-tools/tla2tools.jar
steps:
  - name: Prepare pinned TLA+ runtime
    shell: bash
    run: |
      set -euo pipefail
      tools=/tmp/gh-aw/agent/tla-tools
      mkdir -p "$tools/jre"
      curl --fail --location --silent --show-error \
        --retry 2 --connect-timeout 10 --max-time 120 \
        --output "$tools/jre.tar.gz" \
        'https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jre_x64_linux_hotspot_21.0.12.1_1.tar.gz'
      printf '%s  %s\n' \
        2413149700df0f7d440500a84a8f764c535f21e5a5e87d38328b64eec2c5b500 \
        "$tools/jre.tar.gz" | sha256sum --check --strict
      tar -xzf "$tools/jre.tar.gz" --strip-components=1 -C "$tools/jre"
      curl --fail --location --silent --show-error \
        --retry 2 --connect-timeout 10 --max-time 120 \
        --output "$tools/tla2tools.jar" \
        'https://github.com/tlaplus/tlaplus/releases/download/v1.8.0/tla2tools.jar'
      printf '%s  %s\n' \
        011267fca25d0752cf18894dd585cd28b96960c112ea8a20b823ea770e7e10cd \
        "$tools/tla2tools.jar" | sha256sum --check --strict
      "$tools/jre/bin/java" -version
---

<!--
Import shared/tla-plus.md to install checksum-verified Temurin JRE 21 and
TLC 1.8.0 before the agent runs on a Linux x64 runner. The portable runtime
and jar live under /tmp/gh-aw/agent so they are accessible in the sandbox.
This fragment does not grant tools, network access, or safe outputs.
Custom jobs do not inherit these agent setup steps.
-->

{{#import .github/aw/formal-methods.md}}
