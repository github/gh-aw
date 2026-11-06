---
title: Dependabot Manifest Generation
description: Automatic dependency manifest generation for tracking runtime dependencies in agentic workflows, enabling Dependabot to detect and update outdated tools.
sidebar:
  order: 750
---

The `gh aw compile --dependabot` command scans workflows for runtime tools (`npx`, `pip install`, `go install`) and agentic engine packages, generates dependency manifests (`package.json`, `requirements.txt`, `go.mod`), and configures Dependabot to monitor for updates and security alerts.

Engine entries use the effective installed version: `engine.version` when set, otherwise the compiler default. Copilot CLI releases are tracked through `@github/copilot`; Copilot SDK packages use their independently pinned SDK version. Declarative engines can declare npm, pip, or Go installation metadata. Non-package engines and dynamic GitHub Actions version expressions are not represented as engine package dependencies.

## Usage

Run `gh aw compile --dependabot` to compile all workflows and generate manifests in `.github/workflows/`.

**Prerequisites**: Node.js/npm required for `package-lock.json` generation. Pip and Go manifests generate without additional tools.

## Compiler-managed `gh-aw-actions` ignore rule

`gh aw compile` always reconciles the compiler-managed ignore rule for `github/gh-aw-actions/*` when your repository already has a `github-actions` update block in `.github/dependabot.yml` (this is not limited to `--dependabot` runs).

- No-op if `.github/dependabot.yml` does not exist
- No-op if there is no `package-ecosystem: github-actions` update block
- Preserves user-defined `ignore` entries
- The wildcard reflects the repository that provides the reusable actions: `github/gh-aw-actions/*` by default, or `<owner>/<repo>/*` when `gh aw compile --actions-repo <owner>/<repo>` overrides the actions repository

```yaml
updates:
  - package-ecosystem: github-actions
    directory: "/.github/workflows"
    schedule:
      interval: weekly
    ignore:
      - dependency-name: "github/gh-aw-actions/*" # Managed by gh aw compile. Version-locked to the gh-aw compiler; do not bump.
      - dependency-name: "actions/checkout" # user-defined, preserved
```

## Generated Files

| Ecosystem | Manifest | Lock File |
|-----------|----------|-----------|
| **npm** | `package.json` | `package-lock.json` (via `npm install --package-lock-only`) |
| **pip** | `requirements.txt` | - |
| **Go** | `go.mod` | - |

All ecosystems update `.github/dependabot.yml` with weekly update schedules. Existing configurations are preserved; only missing ecosystems are added.

## APM version updates

Workflows using [`apm:` manifest-backed skills](/gh-aw/reference/dependencies/#native-manifest-backed-skills) cause `gh aw compile --dependabot` to add weekly `package-ecosystem: apm` entries for the consumed manifest directories. The compiler preserves existing schedules, registries, groups, cooldowns, ignore rules, and comments; existing entries covering the path are reused, including fields inherited through YAML aliases and merge keys. Invalid configuration is reported without overwriting it.

```yaml
version: 2
updates:
  - package-ecosystem: apm
    directory: /
    schedule:
      interval: weekly
```

Unlike generated npm/pip/Go inventories, `apm.yml` and `apm.lock.yaml` are authored dependency sources: the compiler never overwrites them. Dependabot updates supported Git references in the manifest and regenerates an existing lockfile. Branch pins such as `#main` update only the lockfile. Dependabot does not create missing locks and does not provide APM security updates.

After an APM Dependabot update, recompile the affected workflow and commit its `.lock.yml` with the manifest/lock changes. Do not translate those updates back into Markdown package lists. The compiled native skill pins and stale-check hash both depend on the committed APM documents.

Private Git repository access must be configured for Dependabot independently of the credentials used by workflow skill installation. The shared APM component's inline `packages:` list is not scanned by Dependabot's APM ecosystem.

## Handling Dependabot PRs

> [!WARNING]
> **Never merge Dependabot PRs that only modify compiler-generated npm, pip, or Go manifest files.** These changes are overwritten on next compilation. This warning does not apply to authored APM manifests and lockfiles.

**Correct workflow**: Update source `.md` files, then recompile to regenerate manifests.

For engine alerts, update `engine.version` in affected workflows, or upgrade gh-aw to obtain newer engine defaults, then run `gh aw compile --dependabot`. SDK defaults are updated through gh-aw, not `engine.version`.

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

## Learn More

- [CLI Commands](/gh-aw/setup/cli/#compile) - Complete compile command reference
- [Compilation Process](/gh-aw/reference/compilation-process/) - How compilation works
- [GitHub Dependabot Docs](https://docs.github.com/en/code-security/dependabot) - Official Dependabot guide
