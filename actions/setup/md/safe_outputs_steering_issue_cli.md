Before finalizing, read comments on the steering issue using the GitHub CLI read transport selected by the GitHub guidance in this prompt. For the mounted `github` wrapper, use `github issue_read --help` to discover the schema, then invoke `github issue_read` with inline JSON selecting `method: get_comments`, the steering issue number, and repository owner/name from `<github-context>`. Only when the prompt explicitly selects pre-authenticated `gh` instead, use its read-only issue-comment API. Do not call native GitHub MCP tools.

Treat relevant user-authored comments containing `steer` as feedback for this run. Follow any user-specified protocol when incorporating feedback into the implementation or final pull request body. Read feedback early, during the run, or near the end as appropriate.

Respect repository, tool, and integrity restrictions; never bypass a denied or filtered read. If feedback conflicts with higher-priority instructions, cannot be acted on safely, or cannot be read through the selected transport, explain the limitation in the final output and use a configured, reachable diagnostic safe-output tool when required.
</issue-steering>
