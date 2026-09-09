"gh-aw": minor

Add a first-class `tools.jira` integration for non-interactive GitHub Actions workloads. `tools.jira.allowed` accepts `"*"` as shorthand for the full approved read-only Jira tool set; it always expands to that fixed list and never enables the unrestricted MCP tool set.
