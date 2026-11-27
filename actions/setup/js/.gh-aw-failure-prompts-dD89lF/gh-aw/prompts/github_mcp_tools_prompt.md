<github-mcp-tools>
The GitHub MCP server is read-only. Use GitHub MCP tools for all GitHub reads: listing and searching issues, pull requests, discussions, labels, milestones; reading workflow runs, jobs, and artifacts; accessing repository contents, code, and metadata. Do not use shell `gh` commands for GitHub API reads — `gh` is not authenticated.

**Identity**: Do not call `get_me` — it returns 403 under the integration token. Read your identity (actor, repository, run ID) from the `<github-context>` block provided at the start of the prompt.

**Code scanning queries**: When calling `list_code_scanning_alerts`, always include `state: open` and `severity: critical,high` to bound the response size and avoid oversized payloads.

**Field selection (GitHub MCP server ≥ 1.6.0)**: When calling list/search tools that accept a `fields` parameter (`list_pull_requests`, `list_issues`, `search_issues`, `search_pull_requests`, `list_commits`, `list_releases`, `search_code`), pass only the fields you need to reduce response size. Example: `list_pull_requests` with `fields: [number, title, state, html_url]`. For `get_file_contents`, `fields` only reduces directory listings; use `fields: [name, type, size, path]` when checking file metadata.

**Partial file reads**: Before using `get_file_contents` on a file, list its parent directory with `fields: [name, type, size, path]`. If the target is large or you only need a header/section, avoid fetching the whole file; use a bounded excerpt such as a raw file URL with an HTTP range or another available ranged-read tool instead.
</github-mcp-tools>
