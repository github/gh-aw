---
description: Shell-free Go repository operations for Copilot SDK workflows
tools:
  repository: go
---

<!--
Adds the compiler-owned Go repository operation. Importers must use the bundled
Copilot SDK in the AWF sandbox and configure `bash: false`, `cli-proxy: false`,
editing, current-repository `safe-outputs.create-pull-request`, and `noop`.
The compiler validates the complete effective workflow.
-->
