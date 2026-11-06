---
env:
  JAVA_BIN: /tmp/gh-aw/agent/tla-tools/jre/bin/java
  TLA2TOOLS_JAR: /tmp/gh-aw/agent/tla-tools/tla2tools.jar
steps:
  - name: Prepare pinned TLA+ runtime
    shell: bash
    run: |
      bash "$GITHUB_WORKSPACE/.github/scripts/install_tla_runtime.sh" /tmp/gh-aw/agent/tla-tools
---

<!--
Import shared/tla-plus.md to install checksum-verified Temurin JRE 21 and
TLA+ Tools v1.7.4 (TLC 2.19) before the agent runs on a Linux x64 runner. The portable runtime
and jar live under /tmp/gh-aw/agent so they are accessible in the sandbox.
This fragment does not grant tools, network access, or safe outputs.
Custom jobs do not inherit these agent setup steps.
The repository-owned installer and reviewed pins come from the workflow repository checkout,
not a previously released setup action that might lack the installer.
-->

{{#import .github/aw/formal-methods.md}}
