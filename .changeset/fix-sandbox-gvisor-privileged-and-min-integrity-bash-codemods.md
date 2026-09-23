---
"gh-aw": patch
---

Add a `gh aw fix --write` codemod that inserts `tools.bash: false` when `tools.github.min-integrity` is set to `none` and `tools.bash` is not already specified, satisfying the strict-mode requirement that shell access be explicit. It runs before the `cli-proxy-false-when-bash-disabled` codemod so a single fix pass also emits the required `tools.cli-proxy: false`, and it supports single-line inline `tools: {github: {min-integrity: none}}` mappings.
