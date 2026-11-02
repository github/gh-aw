---
"gh-aw": patch
---

Prevent template injection through expanding shell heredocs and transport MCP configuration expressions as data.

Security compatibility changes: expressions in unquoted heredocs are moved into step environment bindings, including lines beginning with `#`. Proven quoted bodies remain unchanged; unsupported or mismatched heredoc boundaries require manual migration. `gh aw fix` uses the same Bash boundaries and refuses to write a workflow when a boundary cannot be proven.

MCP configuration values are data, not shell scripts. Simple runner `$NAME` and `${NAME}` references remain supported, but shell computations such as `$(...)`, backticks and `${NAME:-default}` are not evaluated. Compilation warns at the affected configuration path; compute values in a setup step and reference its output, or use an equivalent Actions expression when its inputs are available in that context.

Compilation rejects statically visible combinations of transported data and gateway `${NAME}` references. Startup still rejects transported runtime values containing `${NAME}`, even when intended as literal template text, because the pinned gateway would expand them again. A warning or codemod cannot safely remove this runtime protection.

Recompile existing locks with the updated CLI and deploy them with the matching setup runtime. Regeneration does not repair previously executed workflows. Review source migrations with `gh aw fix <workflow>` before applying them with `--write`; do not hand-edit compiler-owned MCP placeholders.
