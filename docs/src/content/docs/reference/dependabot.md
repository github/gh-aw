---
title: Dependabot Support
description: Automatic dependency manifest generation for tracking runtime dependencies in agentic workflows, enabling Dependabot to detect and update outdated tools.
sidebar:
  order: 750
---

The `gh aw compile --dependabot` command scans workflows for runtime tools (`npx`, `pip install`, `go install`), generates dependency manifests (`package.json`, `requirements.txt`, `go.mod`), and configures Dependabot to monitor for updates

## Usage

Run `gh aw compile --dependabot` to compile all workflows and generate manifests in `.github/workflows/`.

> [!IMPORTANT]
> Must compile **all workflows** - cannot be used with specific files or `--dir` flag.

**Prerequisites**: Node.js/npm required for `package-lock.json` generation. Pip and Go manifests generate without additional tools.

## Generated Files

| Ecosystem | Manifest | Lock File |
|-----------|----------|-----------|
| **npm** | `package.json` | `package-lock.json` (via `npm install --package-lock-only`) |
| **pip** | `requirements.txt` | - |
| **Go** | `go.mod` | - |

All ecosystems update `.github/dependabot.yml` with weekly update schedules. Existing configurations are preserved; only missing ecosystems are added.

## Handling Dependabot PRs

> [!WARNING]
> **Never merge Dependabot PRs that only modify manifest files.** These changes are overwritten on next compilation.

**Correct workflow**: Update source `.md` files, then recompile to regenerate manifests.

```bash
# Find affected workflows
grep -r "@playwright/test@1.41.0" .github/workflows/*.md

# Edit workflow .md files (change version)
# npx @playwright/test@1.41.0 → npx @playwright/test@1.42.0

# Regenerate manifests
gh aw compile --dependabot

# Commit (Dependabot auto-closes its PR)
git add .github/workflows/
git commit -m "chore: update @playwright/test to 1.42.0"
git push
```

### Handling Transitive Dependencies (MCP Servers)

When Dependabot flags transitive dependencies (e.g., `@modelcontextprotocol/sdk`, `hono` from `@sentry/mcp-server`), update the **shared MCP configuration** instead:

```bash
# Locate the shared MCP config (e.g., .github/workflows/shared/mcp/sentry.md)
# Update the version in the args array:
# args: ["@sentry/mcp-server@0.27.0"] → args: ["@sentry/mcp-server@0.29.0"]

# Regenerate manifests
gh aw compile --dependabot

# Regenerate package-lock.json to pick up transitive dependency updates
cd .github/workflows && npm install --package-lock-only

# Commit changes
git add .github/workflows/
git commit -m "chore: update @sentry/mcp-server to 0.29.0"
git push
```

**Why?** The compiler generates `package.json` from MCP server configurations in workflow files. Directly editing `package.json` will be overwritten on next compilation.

## AI Agent Prompt Template

```markdown
A Dependabot PR updated dependencies in .github/workflows/.

Fix workflow:
1. Identify which .md files reference the outdated dependency
2. Update versions in workflow files
3. Run `gh aw compile --dependabot` to regenerate manifests
4. Verify manifests match the Dependabot PR
5. Commit and push (Dependabot auto-closes)

Affected PR: [link]
Updated dependency: [name@version]
```

## Troubleshooting

| Issue | Solution |
|-------|----------|
| **package-lock.json not created** | Install Node.js/npm from [nodejs.org](https://nodejs.org/) |
| **Dependency not detected** | Avoid shell variables (`${TOOL}`); use literal package names |
| **Dependabot not opening PRs** | Verify `.github/dependabot.yml` is valid YAML and manifest files exist |

## Related Documentation

- [CLI Commands](/gh-aw/setup/cli/#compile) - Complete compile command reference
- [Compilation Process](/gh-aw/reference/compilation-process/) - How compilation works
- [GitHub Dependabot Docs](https://docs.github.com/en/code-security/dependabot) - Official Dependabot guide
