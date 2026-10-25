---
description: Complete reference for gh aw CLI commands and their MCP tool equivalents for restricted environments
---

# gh aw CLI Commands Reference

## CLI vs MCP Tool — When to Use Each

| Environment | Use |
|---|---|
| **Terminal with usable `gh` access** | `gh aw <command>` CLI |
| **Client exposing authorized gh-aw MCP tools** | Discover their schemas and use the supported operation |
| **GitHub Actions workflow step** | `gh aw <command>` after installing `github/gh-aw/actions/setup-cli` |
| **No CLI or authorized MCP access** | Cached evidence or static source; report unavailable collection |

> [!NOTE]
> **agentic-workflows MCP tool availability**
>
> Workflow configuration can expose gh-aw MCP tools through `agentic-workflows:`.
> Copilot client configuration and grants vary: do not assume MCP availability
> from the client name or treat it as a substitute for missing authentication.
> During debugging, use already-authorized tools only; adding configuration or
> starting a server requires the shared review gates.
>
> In a GitHub Actions workflow step, install the CLI first:
> ```yaml
> - uses: github/gh-aw/actions/setup-cli@<version>
> - run: gh aw compile
> ```

---

## Command Reference

Full per-command flags, examples, and MCP equivalents live in
[cli-commands-reference.md](cli-commands-reference.md): `skill`, `init`, `compile`,
`run`, `logs`, `audit`, `status`, `checks`, `health`, `experiments`, `fix`, `format`,
`upgrade`, `add`, `update`, `deploy`, `env`, `mcp inspect`, `json-schema`.

---
## MCP Tool ↔ CLI Quick Reference

`gh aw audit <run-id>` inspects a run's execution evidence, including missing tools,
safe outputs, and metrics. With multiple run IDs, it compares runs for differences.
The corresponding MCP tools are `audit` (one run) and `audit-diff` (comparison).
The tool names below are available through an authorized, configured
`agentic-workflows` MCP server; “local only” means there is no MCP equivalent.

| CLI command | MCP tool |
|---|---|
| `gh aw status` | `status` |
| `gh aw compile` | `compile` |
| `gh aw run` | Not provided; no API/MCP/manual-dispatch substitution |
| `gh aw logs` | `logs` |
| `gh aw audit` | `audit` |
| `gh aw audit <id1> <id2>` | `audit-diff` |
| `gh aw checks` | `checks` |
| `gh aw health` | *(local only)* |
| `gh aw experiments` | *(local only)* |
| `gh aw mcp inspect` | `mcp-inspect` |
| `gh aw add` | `add` |
| `gh aw update` | `update` |
| `gh aw fix` | `fix` |
| `gh aw format` | *(local only)* |
| `gh aw upgrade` | `upgrade` |
| `gh aw deploy` | *(local only)* |
| `gh aw env` | *(local only)* |
| `gh aw init` | *(local only)* |
| `gh aw json-schema` | *(local only)* |
