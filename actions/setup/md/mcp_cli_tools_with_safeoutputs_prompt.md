<mcp-clis>
The following servers are available as CLI executables on `PATH`. Invoke them from bash when you need CLI transport. Unless a server is explicitly listed in another prompt section as an MCP tool (for example `<safe-output-tools>`), do not call these via an MCP tool interface.

__GH_AW_MCP_CLI_SERVERS_LIST__

For `mcpscripts`, always use the CLI commands above.
For `safeoutputs`, call the tool names listed in `<safe-output-tools>` directly; the `safeoutputs` CLI commands above are an optional equivalent transport.

For `safeoutputs`, every successful call is a real write-intent declaration - do not use it for probing, auth checks, or placeholder payloads. Use `noop` or `report_incomplete` if not ready to emit the final action.

For multiple or complex arguments, pass the JSON object inline as a single quoted argument — the binary comes first, so a malformed command still runs the CLI and reports an error:
```bash
safeoutputs add_comment '{"item_number":42,"body":"### Title\n\nBody."}'
```
Alternatively, pipe a JSON object on stdin using `.` as the sentinel (prefer this only for large bodies built from files):
```bash
printf '{"item_number":42,"body":"### Title\n\nBody."}' | safeoutputs add_comment .
# or read from a file: safeoutputs create_pull_request . < /tmp/payload.json
```
When piping, make sure the `|` is actually present: `printf '{...}' safeoutputs add_comment .` (no pipe) prints the JSON and exits 0 without ever running the CLI, so nothing is emitted.

**Multi-line or long `body` content:** do NOT build the JSON payload with `printf`/`echo` embedding raw newlines or many escaped characters directly in the command line — the sandbox's shell command-injection guard may reject long or complex quoted arguments (reporting "expansion patterns"/"command substitution" even though none are present) and retrying the identical command will fail again. Instead, write the content to a temp file with a heredoc, then use `jq -Rs` to inject it as the `body` field:
```bash
cat <<'EOF' > /tmp/gh-aw/body.md
Title

Multi-line body content goes here.
EOF
jq -Rs '{title: "My title", body: .}' /tmp/gh-aw/body.md | safeoutputs create_discussion .
```
If a shell command is rejected for containing expansion patterns, do not retry the same command — switch to the heredoc + `jq -Rs` pattern above.

To inject an entire local file as the `body` field without re-embedding its content in the model context, use `jq -Rs`:
```bash
jq -Rs --arg discussion_number "$DISCUSSION_NUMBER" \
  '{discussion_number: ($discussion_number|tonumber), body: .}' \
  discussion-body.md \
  | safeoutputs update_discussion .
```
`jq -Rs` reads the file as a raw string (`-R`) and slurps it into a single JSON string value (`-s`), so `body` is always a valid JSON field. Piping `cat file | safeoutputs ...` does not populate `body` and will be rejected.

The generated command syntax above is schema-derived from each enabled tool's final `inputSchema` and is the source of truth for required/optional parameters.
Use `<server> --help` and `<server> <tool> --help` for the same schema-derived signatures and examples before calling any command.
</mcp-clis>
