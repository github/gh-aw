<safe-outputs>
gh CLI is NOT authenticated. Use the configured safe-output tools for GitHub writes and completion signaling. This engine has no MCP tool-call interface, so the `safeoutputs` CLI on `PATH` is the ONLY way to invoke the tools listed in `<safe-output-tools>`: run `safeoutputs <tool_name> <json>` from bash (see the `<mcp-clis>` section). No command is named after an individual tool, so shell discovery such as `type create_pull_request` or `compgen -c` will never find one; always go through the `safeoutputs` binary.

Check `command -v safeoutputs` in the agent's shell when permitted; runner setup logs do not establish sandbox availability. If `safeoutputs` is missing, no supported transport is available for this engine: stop and report the missing command in the final response. Do not claim the action was emitted, retry commands that are not installed, or use authenticated GitHub commands as a substitute.

Pass the complete JSON object inline as one single-quoted shell argument. Inside JSON strings, encode newlines as `\n`, double quotes as `\"`, backslashes as `\\`, and apostrophes as `\u0027` so they do not end the shell quote. Unicode can remain literal:
```bash
safeoutputs add_comment '{"item_number":42,"body":"### Result\n\nIt\u0027s ready.\nQuoted text: \"hello\".\nLiteral path: C:\\temp\\file.txt"}'
```
Adapt fields to the enabled tool's schema. Stdin is also supported: `safeoutputs <tool_name> .` reads a complete JSON object from stdin. Prefer inline JSON without auxiliary tools, pipelines, temporary files, or shell expansion; stdin support does not grant permission for helper commands or redirections.

Report unavailable functionality with `report_missing_tool` when configured and reachable, but remember that diagnostics do not satisfy the terminal safe-output requirement. Reserve `report_incomplete` for blocked, failed, or unconfirmed work. If the diagnostic tool is unavailable or denied, report the limitation in the final response. Never bypass a denial using an unapproved shell workaround.
