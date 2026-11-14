---
"gh-aw": minor
---

Make `compile --dry-run` usable on more workflows. `pre-steps`, `pre-agent-steps`,
custom MCP servers, `mcp-scripts`, and enabled Bash graders are now omitted from
dry-run locks (and listed in `disabled_effects`) instead of rejected. Generic compiler
warnings are advisory (`advisory_compiler_warnings`); lint and security warnings still
fail the gate. Dry-run placeholder job outputs no longer trip the expression-obfuscation
lint. The debug guides no longer require a security reviewer for compile-only runs or
unchanged tests.
