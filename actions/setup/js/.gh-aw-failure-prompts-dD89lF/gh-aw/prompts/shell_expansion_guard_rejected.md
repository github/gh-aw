> [!WARNING]
> **Shell Expansion Guard Rejected Command**: The sandbox rejected a shell command because expansion patterns looked unsafe.

This signal was detected from engine runtime logs. A quoted payload is not guaranteed to pass the engine's independent shell guard.

<details>
<summary>How to remediate</summary>

- Do **not** retry the identical rejected command.
- Do not bypass the denial with unapproved helper commands, pipelines, temporary files, or shell expansions.
- For permitted CLI calls, prefer one single-quoted JSON object argument. Inside JSON strings, encode newlines as `\n`, double quotes as `\"`, backslashes as `\\`, and apostrophes as `\u0027`; Unicode can remain literal.

```bash
safeoutputs add_comment '{"item_number":42,"body":"### Result\n\nIt\u0027s ready.\nQuoted text: \"hello\".\nLiteral path: C:\\temp\\file.txt"}'
```

Stdin is also supported: `safeoutputs <tool_name> .` reads a complete JSON object from stdin, but does not grant permission for helper commands or redirections. Prefer inline JSON.

If the CLI call is denied, use a native safe-output function only when explicitly exposed. Otherwise call `safeoutputs report_incomplete` with inline JSON containing `reason` and optional `details` when reachable; if that is also denied or unavailable, state the blocker in the final response.

</details>
