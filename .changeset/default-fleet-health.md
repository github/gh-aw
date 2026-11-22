---
"gh-aw": major
---

Make the resumable default-branch fleet report the default for `gh aw health` and the MCP `health` tool.

Remove `--fleet` from CLI calls and `fleet` from MCP arguments. Unnamed calls now return fleet `scope`, `coverage`, historical outcomes, and current-revision status counts. Use `--full` or MCP `full: true` for per-workflow evidence. CLI consumers that require the previous all-branches scope and JSON schema can add `--all-branches`. Named workflow reports are unchanged.
