---
"gh-aw": major
---

Make the resumable default-branch fleet report the default for `gh aw health` and the MCP `health` tool.

Unnamed calls now cover the default branch and exclude PR events. Update JSON consumers to read `scope`, `coverage`, `historical_outcomes`, and `status_counts`. Use `--full` or MCP `full: true` for `details.workflows` and verified failure representatives. Named workflow reports are unchanged; use a named check for branch/event revision evidence or `gh aw logs` for broader run history.
