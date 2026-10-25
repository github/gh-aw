<mcp-clis>
The following servers are available as CLI executables on `PATH`. Invoke them from bash when you need CLI transport. Unless a server is explicitly listed in another prompt section as an MCP tool (for example `<safe-output-tools>`), do not call these via an MCP tool interface.

__GH_AW_MCP_CLI_SERVERS_LIST__

For `mcpscripts`, prefer its native functions when they are explicitly exposed in your tool inventory. Otherwise use the CLI commands above through Bash; `mcpscripts` is a server/executable name, not an individual MCP function. A CLI example does not grant permission for its shell commands or pipelines.

For `safeoutputs`, every successful call is a real write-intent declaration - do not use it for probing, auth checks, or placeholder payloads. Use `noop` only when no action is needed; reserve `report_incomplete` for blocked, failed, or unconfirmed work.

Check `command -v safeoutputs` in the agent's shell when permitted; runner setup logs do not establish sandbox availability. If the CLI is missing, use a configured native safe-output function only if it is explicitly exposed in your tool inventory. Report unavailable functionality with `report_missing_tool` when configured and reachable, but remember that diagnostics do not satisfy the terminal safe-output requirement. Use `report_incomplete` when the task is blocked, a write fails, or completion cannot be confirmed. If no supported transport is available, stop and report the missing command in the final response; do not claim the action was emitted or use authenticated GitHub commands as a substitute.

Pass the complete JSON object inline as one single-quoted shell argument. Inside JSON strings, encode newlines as `\n`, double quotes as `\"`, backslashes as `\\`, and apostrophes as `\u0027` so they do not end the shell quote. Unicode can remain literal. The binary comes first, so a malformed payload still runs the CLI and reports an error:
```bash
safeoutputs add_comment '{"item_number":42,"body":"### Result\n\nIt\u0027s ready.\nQuoted text: \"hello\".\nLiteral path: C:\\temp\\file.txt"}'
```
This preserves multiline content without auxiliary tools, pipelines, temporary files, or shell expansion. Adapt fields to the enabled tool's schema. Raw Markdown and `@filename` references are not JSON payloads.

Stdin is also supported: `safeoutputs <tool_name> .` reads a complete JSON object from stdin. CLI proxying grants the mounted executable, not auxiliary commands, pipelines, file writes, or shell expansions. Stdin support does not grant permission for redirections. Prefer inline JSON; do not introduce helper tools to construct or submit it.

If the sandbox rejects a command, do not repeat it or switch to an unapproved shell workaround. Use a native safe-output function only if explicitly exposed; otherwise call `safeoutputs report_incomplete` with inline JSON containing `reason` and optional `details` when reachable. If even that call is unavailable or denied, state the blocker in your final response; do not probe with real writes.

The generated command syntax above is schema-derived from each enabled tool's final `inputSchema` and is the source of truth for required/optional parameters.
Use `<server> --help` and `<server> <tool> --help` for the same schema-derived signatures and examples before calling any command.
</mcp-clis>
