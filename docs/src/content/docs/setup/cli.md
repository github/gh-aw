---
title: CLI Commands
description: Complete guide to all available CLI commands for managing agentic workflows with the GitHub CLI extension, including installation, compilation, and execution.
sidebar:
  order: 200
---

The `gh aw` CLI extension enables developers to create, manage, and execute AI-powered workflows directly from the command line. It transforms natural language Markdown files into GitHub Actions.

## Day-one commands

| Command | Description | When to use |
|---------|-------------|-------------|
| [`gh aw init`](#init) | Set up your repository for agentic workflows | First time configuring a repo — creates skills, agents, and `.gitattributes` |
| [`gh aw doctor`](#doctor) | Run repository and authentication diagnostics | Verifying `gh` auth, repo ownership, or local checkout state before setup work |
| [`gh aw add-wizard`](#add-wizard) | Add workflows with interactive guided setup | Adding a community workflow and want guided prompts for secrets and auth |
| [`gh aw add`](#add) | Add workflows from other repositories (non-interactive) | Scripted or CI-based workflow installation without interactive prompts |
| [`gh aw new`](#new) | Create a new workflow from scratch | Building a custom workflow when no existing template fits |
| [`gh aw compile`](#compile) | Convert markdown to GitHub Actions YAML | After editing a workflow `.md` file to regenerate the `.lock.yml` |
| [`gh aw list`](#list) | Quick listing of all workflows | Checking which workflows are installed in the current repository |
| [`gh aw run`](#run) | Execute workflows immediately in GitHub Actions | Triggering a workflow run from the command line without opening GitHub |
| [`gh aw status`](#status) | Check current state of all workflows | Verifying workflows are enabled and seeing their last run result |
| [`gh aw logs`](#logs) | Download and analyze agentic workflow logs and artifacts | Debugging a past run by inspecting output, tokens used, and artifacts |
| [`gh aw audit`](#audit) | Audit and compare workflow runs | Investigating cost, tool usage, or comparing two runs side-by-side |

> [!TIP]
> New to `gh aw`? Start with the [day-one commands](#day-one-commands). The advanced and enterprise setup is further down the page and can be skipped for most users.

## Installation

Install the GitHub CLI extension:

```bash wrap
gh extension install github/gh-aw
```

### Pinning to a Specific Version

Pin a version for production environments, team consistency, or to avoid breaking changes:

```bash wrap
gh extension install github/gh-aw@v0.1.0          # Pin to release tag
gh extension install github/gh-aw@abc123def456    # Pin to commit SHA
gh aw version                                         # Check current version

# Upgrade pinned version
gh extension remove gh-aw
gh extension install github/gh-aw@v0.2.0
```

### Alternative: Standalone Installer

If extension installation fails, use the standalone installer instead:

```bash wrap
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash                # Latest
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash -s v0.1.0      # Pinned
```

```powershell wrap
Invoke-WebRequest https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.ps1 -OutFile install-gh-aw.ps1; pwsh -File ./install-gh-aw.ps1          # Latest
Invoke-WebRequest https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.ps1 -OutFile install-gh-aw.ps1; pwsh -File ./install-gh-aw.ps1 v0.1.0  # Pinned
```

This installs to `~/.local/share/gh/extensions/gh-aw/gh-aw` and supports Linux, macOS, FreeBSD, Windows, and Android (Termux), including environments behind corporate firewalls.

### GitHub Actions Setup Action

In GitHub Actions, use the `setup-cli` action for platform detection and checksum verification:

```yaml wrap
- name: Install gh-aw CLI
  uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v0.37.18
```

See the [setup-cli action README](https://github.com/github/gh-aw/blob/main/actions/setup-cli/README.md) for full details.

## Global Options

| Flag | Description |
|------|-------------|
| `-h`, `--help` | Show help (`gh aw help [command]` for command-specific help) |
| `-v`, `--verbose` | Enable verbose output showing detailed information |
| `--banner` | Display ASCII logo banner with purple GitHub color theme |
| `--version` | Print the current version |

For invalid nested command paths, `gh aw` now fails explicitly instead of falling back to parent help output. For example, `gh aw secrets gh --help` returns an unknown-command error rather than reprinting `gh aw secrets` help.

Use `gh aw version` to print the current version.

### The `--push` Flag

`gh aw run --push` stages workflow files (including transitive imports), commits them, and pushes before dispatching the workflow. It refuses to proceed when unrelated files are already staged.

For `init`, `update`, and `upgrade`, use `--create-pull-request` instead.

## Commands

Commands are organized by workflow lifecycle: creating, building, testing, monitoring, and managing workflows.

Use this table to choose between the similarly named setup commands:

| Command | Best fit |
|---------|----------|
| [`gh aw add-wizard`](#add-wizard) | Guided, interactive setup for an existing workflow, including prompts for engine auth and secrets |
| [`gh aw add`](#add) | Direct, non-interactive installation of an existing local, remote, or packaged workflow |
| [`gh aw new`](#new) | Scaffold a new workflow template in this repository before writing custom instructions |

### Getting Workflows

#### `init`

Initialize repository for agentic workflows. Configures `.gitattributes`, creates the dispatcher skill file (`.github/skills/agentic-workflows/SKILL.md`), and performs non-interactive setup. With the Copilot engine (`--engine copilot`), it also creates the Agentic Workflows custom agent (`.github/agents/agentic-workflows.md`) and enables MCP server integration by default (use `--no-mcp`/`--no-agent` to skip these Copilot-specific artifacts). Use `--no-skill` to skip dispatcher skill creation. Non-Copilot engines skip Copilot-specific artifacts; see [Initializing for non-Copilot engines](#initializing-for-non-copilot-engines).

```bash wrap
gh aw init                              # Initialize repository with defaults (non-interactive)
gh aw init --engine claude              # Skip Copilot-specific artifacts
gh aw init --no-mcp                     # Skip MCP server integration (Copilot engine)
gh aw init --no-skill                   # Skip dispatcher skill creation
gh aw init --no-agent                   # Skip custom agent creation (Copilot engine)
gh aw init --codespaces                 # Configure Codespaces for current repo only
gh aw init --codespaces=repo1,repo2     # Configure Codespaces with additional repos
gh aw init --completions                # Install shell completions
gh aw init --create-pull-request        # Initialize and open a pull request
```

**Options:** `--engine/-e`, `--no-mcp`, `--no-skill`, `--no-agent`, `--codespaces`, `--completions`, `--create-pull-request`

##### Initializing for non-Copilot engines

With `--engine claude`, `--engine codex`, `--engine gemini`, or `--engine pi`, `init` still performs the engine-independent setup and only skips the Copilot-specific artifacts:

| Artifact | Copilot engine | Other engines | Replacement for other engines |
|---|:---:|:---:|---|
| `.gitattributes` entries for compiled `.lock.yml` files | ✅ | ✅ | Not needed — created for every engine |
| Dispatcher skill `.github/skills/agentic-workflows/SKILL.md` | ✅ | ✅ | Not needed — created for every engine; the instructions are plain Markdown that any agent can be pointed at |
| Custom agent `.github/agents/agentic-workflows.md` | ✅ | ❌ | Use the dispatcher skill, or author an agent file in your own agent's format (Claude Code subagents, Codex prompts) from the same instructions |
| MCP wiring: `.github/mcp.json` and `.github/workflows/copilot-setup-steps.yml` | ✅ | ❌ | Register `gh aw mcp-server` in your own MCP host configuration — see [GH-AW as an MCP Server](/gh-aw/reference/gh-aw-as-mcp-server/) |

After `init`, the remaining steps are the same for every engine: pick the engine in workflow frontmatter (`engine: claude`, `engine: codex`, `engine: gemini`, `engine: pi`) and configure that engine's authentication secret. See [AI Engines](/gh-aw/reference/engines/) and [Authentication](/gh-aw/reference/auth/).

The engine chosen at `init` time does not restrict workflows: every workflow selects its own engine in frontmatter, and example workflows written for one engine can be adapted to another by changing `engine:` and its authentication secret.

#### `add-wizard`

Add a workflow with interactive guided setup. Checks requirements, adds the markdown file, and generates the compiled YAML. Prompts for missing API keys and secrets. For remote workflows, this command follows frontmatter [`redirect`](/gh-aw/reference/frontmatter/#redirect-redirect) declarations before installation.

Before the final pull request confirmation, the wizard optionally offers to add repository support files for using coding agents to author, debug, update, and audit agentic workflows. The prompt is skipped when those support files are already configured. Declining adds only the selected workflow files.

```bash wrap
gh aw add-wizard githubnext/agentics/ci-doctor           # Interactive setup
gh aw add-wizard https://github.com/org/repo/blob/main/workflows/my-workflow.md
gh aw add-wizard https://example.com/workflows/my-workflow.json   # Arbitrary URL (JSON workflow)
gh aw add-wizard githubnext/agentics/ci-doctor --no-secret  # Skip secret prompt
```

**Options:** `--no-secret`, `--dir/-d`, `--engine/-e`, `--gh-aw-ref`, `--no-gitattributes`, `--no-stop-after`, `--stop-after`, `--append`, `--no-security-scanner`, `--no-config`

When the Copilot engine is selected, the wizard prompts the user to choose an authentication method: organization billing via [`permissions.copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission) (no PAT required), or a [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token) personal access token (a separate token from the default `GITHUB_TOKEN`, because the agent needs elevated Copilot API access that the ephemeral workflow token does not carry). When `COPILOT_GITHUB_TOKEN` already exists, using it is the default; because GitHub does not expose stored secret values, the user asserts that it is a suitable fine-grained PAT with Copilot Requests permission. The alternative replacement path opens a preconfigured fine-grained PAT creation page, validates the pasted token, and updates the repository secret.

#### `add`

Add workflows from the Agentics collection or other repositories to `.github/workflows`. For remote workflows, this command follows frontmatter [`redirect`](/gh-aw/reference/frontmatter/#redirect-redirect) declarations before installation.

```bash wrap
gh aw add githubnext/agentics/ci-doctor           # Add single workflow
gh aw add githubnext/agentics/ci-doctor@v1.0.0   # Add specific version
gh aw add ./my-workflow.md                       # Add a local workflow file
gh aw add ./*.md                                 # Add multiple local workflow files
gh aw add githubnext/agentics/ci-doctor --dir .github/workflows/shared  # Organize in subdirectory
gh aw add githubnext/agentics/ci-doctor --create-pull-request        # Create PR instead of commit
gh aw add https://example.com/workflows/my-workflow.md               # Arbitrary HTTPS URL (markdown)
gh aw add https://example.com/workflows/my-workflow.json             # Arbitrary HTTPS URL (JSON workflow definition)
```

**Options:** `--dir/-d`, `--create-pull-request`, `--no-gitattributes`, `--append`, `--no-security-scanner`, `--engine/-e`, `--force/-f`, `--gh-aw-ref`, `--name/-n`, `--no-stop-after`, `--stop-after`

Repository-level packages can declare an [`aw.yml` manifest](/gh-aw/reference/aw-yml-package-manifest/) at the repository root or in a nested package folder to define installable files, package `README.md`, schema compatibility, and minimum supported CLI versions.

`add` and `add-wizard` also accept arbitrary `http(s)://` URLs. The fetched response is dispatched by `Content-Type`: `text/markdown` (and `text/x-markdown`) is installed as a raw gh-aw workflow, and `application/json` (or any `*+json` suffix) is converted to a workflow markdown file before installation. Unknown content types produce an actionable error listing the detected type. For non-GitHub hosts, no include/dispatch-workflow dependency resolution is performed, and no GitHub authentication token is sent to the remote server.

##### JSON Workflow Field Mapping

When importing a JSON workflow definition (for example, a payload from the Copilot automation API), the importer translates JSON fields into gh-aw frontmatter and workflow body:

| JSON field | Mapped to | Notes |
|------------|-----------|-------|
| `triggers.interval` | `on:` (fuzzy schedule) | `hourly` → `every 1h`, `daily` → `daily`, `weekly` → `weekly`. A single interval trigger emits the inline shorthand (`on: daily`); the compiler randomizes cron at compile time. |
| `triggers.issues` | `on.issues.types` | A `query` filter has no gh-aw equivalent and emits a per-field warning. |
| `triggers.workflow_run` | `on.workflow_run` (`workflows`, `types`) | A `conclusions` filter emits a per-field warning. |
| `tools` | `tools:` | A 40-entry lookup maps GitHub tool IDs to gh-aw toolsets (`issues`, `pull-requests`, `repos`, etc.). `execute` maps to `bash: "*"` (with a review warning); `web_search` maps to `web-search:`. Built-in read/edit/search tools are silently skipped. Unrecognized tools emit a per-tool warning. |
| `permissions` | `permissions:` | Passed through unchanged. |
| `prompt` | Workflow body | Used when an `instructions` field is absent. |

Unrecognized fields are preserved as commented hints in the generated workflow.

#### `new`

Create a workflow template in `.github/workflows/`, or launch the interactive wizard when no workflow name is provided.

```bash wrap
gh aw new                              # Interactive mode
gh aw new my-custom-workflow           # Create template (.md extension optional)
gh aw new my-workflow --force          # Overwrite if exists
gh aw new my-workflow --engine claude  # Inject engine into frontmatter
```

**Options:** `--force/-f`, `--engine/-e`, `--interactive/-i`

When `--engine` is specified, the engine is injected into the generated frontmatter template:

```yaml wrap
---
permissions:
  contents: read
engine: claude
network: defaults
...
```

#### `secrets`

Manage GitHub Actions secrets and tokens.

##### `secrets set`

Create or update a repository secret (from stdin, flag, or environment variable).

```bash wrap
gh aw secrets set MY_SECRET                                    # From stdin (current repo)
gh aw secrets set MY_SECRET --repo myorg/myrepo                # Specify target repo
gh aw secrets set MY_SECRET --value "secret123"                # From flag
gh aw secrets set MY_SECRET --value-from-env MY_TOKEN          # From env var
```

**Options:** `--repo/-r`, `--value`, `--value-from-env`, `--api-url`

For Claude workflows, set `ANTHROPIC_API_KEY` or configure [Anthropic WIF](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif). `CLAUDE_CODE_OAUTH_TOKEN`, including a token from `claude login`, is not supported; it is silently ignored, so the run instead fails with an authentication error from the Claude CLI that never mentions the token, which is the signal to switch credentials.

##### `secrets bootstrap`

Analyze workflows to determine required secrets and interactively prompt for missing ones. Auto-detects engines in use and checks which required secrets are already configured.

```bash wrap
gh aw secrets bootstrap                                  # Analyze all workflows and prompt for missing secrets
gh aw secrets bootstrap --engine copilot                 # Check only Copilot secrets
gh aw secrets bootstrap --non-interactive                # Display missing secrets without prompting
```

**Options:** `--engine/-e` (copilot, claude, codex, gemini, pi), `--non-interactive`, `--repo/-r`

See [Authentication](/gh-aw/reference/auth/) for details.

#### `doctor`

Run diagnostics to verify CLI authentication and repository setup.

When running inside a GitHub Enterprise checkout and `GH_HOST` is unset, `doctor` auto-detects the host from the git remote. Outside a checkout, run `gh auth login --hostname <host>` to authenticate and set `GH_HOST=<host>` so repository diagnostics target the correct host.

```bash wrap
gh aw doctor
gh aw doctor --json
gh aw doctor --repo github/gh-aw
gh aw doctor --repo github/gh-aw --dir ./gh-aw --require-owner-type org
```

**Options:** `--repo/-r`, `--dir/-d`, `--require-owner-type`, `--json/-j`

Use `--repo` to verify a specific repository exists and inspect the local checkout that should correspond to it. `--require-owner-type` accepts `any`, `user`, or `org` and defaults to `any`; `--dir` and `--require-owner-type` require `--repo`.

`doctor --repo` currently accepts `owner/repo` only. To target GitHub Enterprise Server, select the host via `GH_HOST` rather than prefixing the repository with `[HOST/]`.

### Building

#### `fix`

Auto-fix deprecated workflow fields using codemods. Runs in dry-run mode by default; use `--write` to apply changes.

```bash wrap
gh aw fix                              # Check all workflows (dry-run)
gh aw fix --write                      # Fix all workflows
gh aw fix my-workflow --write          # Fix specific workflow
gh aw fix --list-codemods              # List available codemods
```

**Options:** `--dir/-d`, `--disable-codemod`, `--list-codemods`, `--write`

Use `--disable-codemod` (repeatable) to skip specific codemod IDs by name. Unlike the `--no-X` flags used elsewhere in the CLI (which toggle boolean options), `--disable-codemod` takes a codemod ID as its value and can be specified multiple times, so the `--no-codemod` pattern does not apply here.

Available codemods include:

- `expires-integer-to-string` — converts bare integer `expires` values (e.g., `expires: 7`) to the preferred day-string format (e.g., `expires: 7d`) in all `safe-outputs` blocks.
- `steps-run-secrets-to-env` — rewrites **all** `${{ ... }}` expressions in step `run:` commands to `$VARNAME` references (or `$env:VARNAME` for PowerShell steps) and adds step-level `env` bindings. Secrets, `env.*`, and `github.token` use stable legacy names; all other expressions receive `EXPR_*` names. Required for strict-mode compliance.
- `engine-env-secrets-to-engine-config` — removes secret-bearing entries from `engine.env` that are unsafe under strict mode, preserving required engine credential keys.

Run `gh aw fix --list-codemods` to see all available codemods.

#### `compile`

Compile Markdown workflows to GitHub Actions YAML. Remote imports cached in `.github/aw/imports/`.

```bash wrap
gh aw compile                              # Compile all workflows
gh aw compile my-workflow                  # Compile specific workflow
gh aw compile --watch                      # Auto-recompile on changes
gh aw compile --validate --strict          # Schema + strict mode validation
gh aw compile --fix                        # Run fix before compilation
gh aw compile --zizmor                     # Security scan (warnings)
gh aw compile --strict --zizmor            # Security scan (fails on findings)
gh aw compile --grant                      # License scan container images
gh aw compile --yamllint                   # Lint generated YAML output
gh aw compile --dependabot                 # Generate dependency manifests
gh aw compile --purge                      # Remove orphaned .lock.yml files
```

If the repository root contains an [`aw.yml` manifest](/gh-aw/reference/aw-yml-package-manifest/), `gh aw compile` validates it before compiling workflows.

Unlike `gh aw upgrade`, `gh aw compile` does not run codemods unless you pass `--fix`.

**Options:** `--action-mode`, `--action-tag`, `--actionlint`, `--actions-repo`, `--allow-action-refs`, `--approve`, `--dependabot`, `--dir/-d`, `--engine/-e`, `--fail-fast`, `--fix`, `--force/-f`, `--force-refresh-action-pins`, `--force-refresh-container-pins`, `--gh-aw-ref`, `--ghes`, `--grant`, `--grype`, `--json/-j`, `--logical-repo/-l`, `--models`, `--no-check-update`, `--no-emit`, `--poutine`, `--purge`, `--refresh-stop-time`, `--runner-guard`, `--schedule-seed`, `--shellcheck`, `--show-all`, `--staged`, `--stats`, `--strict`, `--syft`, `--trial`, `--validate`, `--validate-images`, `--watch/-w`, `--yamllint`, `--zizmor`

**`--gh-aw-ref` flag:** Convenience alias for `--action-mode release --action-tag <ref>`. Accepts a branch name, tag, or commit SHA targeting the `github/gh-aw` repository. Branch and tag names are resolved to their full commit SHA at compile time, so the baked-in reference is immutable and reproducible. Useful for E2E-testing workflows compiled against a specific gh-aw revision.

**`--models` flag:** Refreshes the observed model inventory using the same data sources as `gh aw models`, then warns when `models.allowed`, `models.blocked`, or `engine.models` references an unknown model. Built-in and workflow model aliases are accepted. If no observed model data is available, the check is skipped.

**`--approve` flag:** When compiling a workflow that already has a lock file, the compiler enforces *safe update mode* — any newly added secrets or custom actions not present in the previous manifest require explicit approval. Pass `--approve` to accept these changes and regenerate the manifest baseline. On first compile (no existing lock file), enforcement is skipped automatically and `--approve` is not needed.

**Error Reporting:** Displays detailed error messages with file paths, line numbers, column positions, and contextual code snippets.

**JSON Output (`--json`):** Emits an array of `ValidationResult` objects. Each result includes a `labels` field listing all repository labels referenced in safe-outputs (`create-issue.labels`, `create-discussion.labels`, `create-pull-request.labels`, `add-labels.allowed`). Use `--json --no-emit` to collect label references without writing compiled files.

**Dependabot Integration (`--dependabot`):** Generates dependency manifests and `.github/dependabot.yml` by analyzing runtime tools across all workflows. See [Dependabot Support reference](/gh-aw/reference/dependabot/).

**Strict Mode (`--strict`):** Enforces security best practices: no write permissions (use [safe-outputs](/gh-aw/reference/safe-outputs/)), explicit `network` config, no wildcard domains, pinned actions, no deprecated fields. See [Strict Mode reference](/gh-aw/reference/frontmatter/#strict-mode-strict).

**Security and Compliance Scanners:**
- **`--syft`:** Generates a Software Bill of Materials (SBOM) for container images referenced in compiled workflows using the Syft scanner.
- **`--grype`:** Scans container images referenced in compiled workflows for known vulnerabilities using the Grype vulnerability scanner. When a `.grype.yaml` file exists at the repository root it is mounted into the scanner and passed to grype via `--config`, so repository-level ignore rules (documented risk acceptances for findings with no upstream fix) are applied.
- **`--runner-guard`:** Runs taint analysis on compiled workflows to detect unsafe data flows from untrusted inputs to sensitive runner operations.

**Shared Workflows:** Workflows without an `on` field are detected as shared components. Validated with relaxed schema and skip compilation. See [Imports reference](/gh-aw/reference/imports/).

#### `validate`

Validate agentic workflows by running the compiler with all linters enabled, without generating lock files. Equivalent to `gh aw compile --validate --no-emit --zizmor --actionlint --poutine`.

```bash wrap
gh aw validate                              # Validate all workflows
gh aw validate my-workflow                  # Validate specific workflow
gh aw validate my-workflow daily            # Validate multiple workflows
gh aw validate --json                       # Output results in JSON format
gh aw validate --strict                     # Enforce strict mode validation
gh aw validate --fail-fast                  # Stop at the first error
gh aw validate --dir custom/workflows       # Validate from custom directory
gh aw validate --engine copilot             # Override AI engine
```

**Options:** `--allow-action-refs`, `--dir/-d`, `--engine/-e`, `--fail-fast`, `--json/-j`, `--no-check-update`, `--stats`, `--strict`, `--validate-images`

All linters (`zizmor`, `actionlint`, `poutine`), `--validate`, and `--no-emit` are always-on defaults and cannot be disabled. Accepts the same workflow ID format as `compile`.

#### `lint`

Lint existing `.lock.yml` workflow files from disk with actionlint only. This command does not recompile Markdown workflows, and skips `zizmor`/`poutine`.

```bash wrap
gh aw lint                                          # Lint all .github/workflows/*.lock.yml
gh aw lint .github/workflows/foo.lock.yml           # Lint a specific lock file
gh aw lint --dir .github/workflows                  # Lint all lock files in a directory
gh aw lint --shellcheck --pyflakes                  # Enable actionlint script integrations
```

**Options:** `--dir/-d`, `--shellcheck`, `--pyflakes`

By default, shellcheck and pyflakes integrations are disabled to reduce noise for generated `run:` scripts. Built-in actionlint ignore patterns cover gh-aw-specific extensions such as `job.workflow_*` context properties and the `copilot-requests` permission scope.

### Testing

#### `trial`

Test workflows in temporary private repositories (default) or run directly in specified repository (`--host-repo`). Results saved to `trials/`.

```bash wrap
gh aw trial githubnext/agentics/ci-doctor          # Test remote workflow
gh aw trial ./workflow.md --logical-repo owner/repo # Act as different repo
gh aw trial ./workflow.md --host-repo owner/repo   # Run directly in repository
gh aw trial ./workflow.md --dry-run                # Preview without executing
```

**Options:** `--engine/-e`, `--repeat`, `--delete-host-repo-after`, `--logical-repo/-l`, `--clone-repo`, `--trigger-context`, `--host-repo`, `--dry-run`, `--append`, `--auto-merge-prs`, `--no-security-scanner`, `--delete-host-repo-before`, `--json/-j`, `--timeout`, `--yes/-y`

**Secret Handling:** API keys required for the selected engine are automatically checked. If missing from the target repository, they are prompted for interactively and uploaded.

#### `run`

Execute workflows immediately in GitHub Actions. Displays workflow URL for tracking.

```bash wrap
gh aw run workflow                          # Run workflow
gh aw run workflow1 workflow2               # Run multiple workflows
gh aw run workflow --repeat 3               # Run 4 times total (1 initial + 3 repeats)
gh aw run workflow --push                   # Commit, push, and dispatch the workflow
gh aw run workflow --push --ref main        # Push to specific branch
gh aw run workflow --dry-run                # Preview without triggering workflow runs
gh aw run workflow --json                   # Output triggered workflow results as JSON
```

**Options:** `--repeat`, `--push` (see [--push flag](#the---push-flag)), `--ref`, `--enable-if-needed`, `--json/-j`, `--auto-merge-prs`, `--dry-run`, `--engine/-e`, `--raw-field`, `--repo/-r`, `--approve`

When `--json` is set, a JSON array of triggered workflow results is written to stdout.

When `--push` is used, automatically recompiles outdated `.lock.yml` files, stages all transitive imports, and triggers workflow run after successful push. Without `--push`, warnings are displayed for missing or outdated lock files.

> [!NOTE]
> Codespaces Permissions
> Requires `workflows:write` permission. In Codespaces, either configure custom permissions in `devcontainer.json` ([docs](https://docs.github.com/en/codespaces/managing-your-codespaces/managing-repository-access-for-your-codespaces)) or authenticate manually: `unset GH_TOKEN && gh auth login`

### Monitoring

#### `list`

List workflows with basic information (name, engine, compilation status) without checking GitHub Actions state.

```bash wrap
gh aw list                                  # List all workflows
gh aw list ci-                              # Filter by pattern (case-insensitive)
gh aw list --json                           # Output in JSON format
gh aw list --label automation               # Filter by label
gh aw list --dir custom/workflows           # List from a local custom directory
gh aw list --repo owner/repo --path .github/workflows  # List from a remote repository
```

**Options:** `--json/-j`, `--label`, `--dir/-d`, `--path`, `--repo/-r`

Two flags control the workflow directory location, with different purposes:
- `--dir` (`-d`): overrides the **local** workflow directory. Applies only when `--repo` is not set.
- `--path`: specifies the workflow directory path in a **remote** repository. Use together with `--repo`.

Fast enumeration without GitHub API queries. For detailed status including enabled/disabled state and run information, use `status` instead.

#### `status`

List workflows with state, enabled/disabled status, and labels. With `--ref`, includes latest run status. Use `--json` to inspect the raw `on` data, including schedules.

```bash wrap
gh aw status                                # All workflows
gh aw status --ref main                     # With run info for main branch
gh aw status --label automation             # Filter by label
gh aw status --repo owner/other-repo        # Check different repository
```

**Options:** `--ref`, `--label`, `--json/-j`, `--repo/-r`

#### `logs`

Download and analyze logs with tool usage, network patterns, errors, warnings. Results cached for 10-100x speedup on subsequent runs.

```bash wrap
gh aw logs workflow                        # Download logs for workflow
gh aw logs -c 10 --start-date -1w         # Filter by count and date
gh aw logs --ref main --parse --json      # With markdown/JSON output for branch
```

With `--json`, the output also includes deterministic lineage data under `.episodes[]` and `.edges[]`. Use these fields to group orchestrated runs into execution episodes instead of reconstructing relationships from `.runs[]` alone.

**Workflow name matching**: The logs command accepts both workflow IDs (kebab-case filename without `.md`, e.g., `ci-failure-doctor`) and display names (from frontmatter, e.g., `CI Failure Doctor`). Matching is case-insensitive for convenience:

```bash wrap
gh aw logs ci-failure-doctor               # Workflow ID
gh aw logs CI-FAILURE-DOCTOR               # Case-insensitive ID
gh aw logs "CI Failure Doctor"             # Display name
gh aw logs "ci failure doctor"             # Case-insensitive display name
```

**`--cache-before` flag (cache cleanup):** Deletes cached run folders in the output directory whose run creation date is older than the specified cutoff. Accepts the same date/time delta formats as `--start-date` and `--end-date` (e.g. `-1d`, `-1w`, `-1mo`) as well as absolute dates (`YYYY-MM-DD`). Cleanup runs before the download step to free disk space first; failures are non-fatal and logged as warnings. The previous `--after` spelling is kept as a hidden, deprecated alias.

```bash wrap
gh aw logs --cache-before -1w                        # Evict local cache older than 1 week, then proceed with normal run download
gh aw logs --cache-before -30d                       # Evict local cache entries older than 30 days
gh aw logs --cache-before 2024-01-01                 # Evict local cache entries from before a specific date
gh aw logs my-workflow --cache-before -1mo -c 20     # Evict local cache older than 1 month, then download 20 runs of a specific workflow
```

Only directories matching the `run-{ID}` naming pattern inside the output directory are considered. The run's creation timestamp is read from `run_summary.json` inside each folder; if that file is absent (e.g., incomplete download), the directory's modification time is used as a fallback.

**`--train` flag:** Trains log template weights from the downloaded runs and writes `drain3_weights.json` to the logs output directory. The trained weights improve anomaly detection accuracy in subsequent `gh aw audit` and `gh aw logs` runs. To embed weights into the binary as defaults, copy the file to `pkg/agentdrain/data/default_weights.json` and rebuild.

```bash wrap
gh aw logs --train                    # Train on last 10 runs
gh aw logs my-workflow --train -c 50  # Train on up to 50 runs of a specific workflow
```

**`--stdin` flag:** Reads run IDs or URLs from stdin (one per line) instead of discovering runs from the GitHub API. Mutually exclusive with the workflow-name positional argument. Date, count, and workflow-name filters are ignored when `--stdin` is set; content filters (`--engine`, `--firewall`, `--safe-output`, etc.) still apply. Blank lines and `#`-prefixed comment lines are ignored. Bare numeric IDs require `--repo owner/repo` because they carry no embedded repo context. Full run URLs are self-contained and do not require `--repo`.

```bash wrap
cat run-ids.txt | gh aw logs --stdin
echo "1234567890" | gh aw logs --stdin --engine claude
cat run-ids.txt | gh aw logs --stdin --repo owner/repo   # required for bare numeric IDs
gh aw logs --runtime gvisor                              # Filter to runs using a specific sandbox agent runtime
```

**Options:** `--after-run-id`, `--artifacts`, `--before-run-id`, `--cache-before`, `--count/-c`, `--end-date`, `--engine/-e`, `--evals`, `--exclude-staged`, `--filtered-integrity`, `--firewall`, `--format`, `--json/-j`, `--last`, `--no-firewall`, `--output/-o`, `--parse`, `--ref`, `--report-file`, `--repo/-r`, `--runtime`, `--safe-output`, `--start-date`, `--stdin`, `--summary-file`, `--timeout`, `--tool-graph`, `--train`

`logs` defaults `--artifacts` to `usage` for faster, compact downloads. The `--last` flag is an alias for `--count/-c`.

#### `audit`

Analyze workflow runs with detailed reports. The `audit` command has two modes: a single-run audit (default) and a multi-run analysis.

##### `audit <run-id>`

Analyze a single run with a rich multi-section report. Accepts run IDs, workflow run URLs, job URLs, and step-level URLs. Auto-detects Copilot coding agent runs for specialized parsing. Job URLs automatically extract specific job logs; step URLs extract specific steps; without step, extracts first failing step.

```bash wrap
gh aw audit 12345678                                      # By run ID
gh aw audit https://github.com/owner/repo/actions/runs/123 # By workflow run URL
gh aw audit https://github.com/owner/repo/actions/runs/123/job/456 # By job URL (extracts first failing step)
gh aw audit https://github.com/owner/repo/actions/runs/123/job/456#step:7:1 # By step URL (extracts specific step)
gh aw audit 12345678 --parse                              # Parse logs to markdown
gh aw audit 12345678 --repo owner/repo                    # Specify repository for bare run ID
```

**`--stdin` flag:** Reads run IDs or URLs from stdin (one per line), bypassing the need to pass positional arguments. Mutually exclusive with positional run-ID arguments. Blank lines and `#`-prefixed lines are ignored. Bare numeric IDs require `--repo owner/repo`; full URLs carry their own repo context.

```bash wrap
echo "1234567890" | gh aw audit --stdin
echo -e "1234567890\n9876543210" | gh aw audit --stdin   # diff mode: first is base
cat run-ids.txt | gh aw audit --stdin --repo owner/repo
gh aw audit 1234567890 --runtime gvisor                  # Skip run unless sandbox agent runtime matches
```

**Options:** `--artifacts`, `--evals`, `--experiment`, `--format`, `--json/-j`, `--output/-o`, `--parse`, `--repo/-r`, `--runtime`, `--stdin`, `--variant`

The `--repo` flag accepts `owner/repo` format and is required when passing a bare numeric run ID without a full URL, allowing the command to locate the correct repository.

The `--artifacts` flag selects which artifact sets to download (default: `all`). Valid sets include `activation`, `agent`, `all`, `detection`, `evals`, `experiment`, `firewall`, `github-api`, `graders`, `mcp`, and `usage`. Use `all` to download the full artifact set. Unlike `gh aw logs`, which defaults to `usage`, `audit` defaults to `all` for comprehensive analysis. The `--experiment` flag filters to runs that include the named experiment; `--variant` further restricts to a specific variant value and requires `--experiment` to be set. The `--output/-o` flag overrides the output directory.

Logs are saved to `.github/aw/logs/run-{id}/` with filenames indicating the extraction level. Pre-agent failures (integrity filtering, missing secrets, binary install) surface the actual error in `failure_analysis.error_summary`. Invalid run IDs return a human-readable error.

**Report sections:**

| Section | Description |
|---------|-------------|
| **Overview** | Run status, duration, trigger event, repository |
| **Engine Configuration** | Engine ID, model, CLI version, firewall version, MCP servers configured |
| **Prompt Analysis** | Prompt size and source file |
| **Session & Agent Performance** | Wall time, turn count, average turn duration, tokens per minute, timeout detection, agent active ratio |
| **MCP Server Health** | Per-server request counts, error rates, average latency, health status, and slowest tool calls |
| **Safe Output Summary** | Total safe output items broken down by type (comments, PRs, issues, etc.) |
| **Metrics** | Tool usage, token consumption, cost |
| **MCP Failures** | Failed MCP tool calls with error details |
| **Firewall Analysis** | Network requests blocked or allowed by the firewall |
| **Jobs** | Status of each GitHub Actions job in the run |
| **Artifacts** | Downloaded artifacts and their contents |

##### Multi-run diff mode

Compare behavior between two or more workflow runs to detect policy regressions, new unauthorized domains, behavioral drift, and changes in MCP tool usage or run metrics. Pass multiple run IDs directly to `audit` — the first is the base, the rest are comparisons:

```bash wrap
gh aw audit 12345 12346                     # Compare two runs
gh aw audit 12345 12346 12347 12348         # Compare base against 3 runs
gh aw audit 12345 12346 --format markdown   # Markdown output for PR comments
gh aw audit 12345 12346 --json              # JSON for CI integration
gh aw audit 12345 12346 --repo owner/repo   # Specify repository
```

The diff output shows: new or removed network domains, status changes (allowed ↔ denied), volume changes (>100% threshold), MCP tool invocation changes, run metric comparisons (token usage, duration, turns), tokens-per-turn changes, and per-tool and per-bash-command call breakdowns.

**Options:** `--artifacts`, `--format` (pretty, markdown; default: pretty), `--json/-j`, `--output/-o`, `--repo/-r`

#### `graders`

Inspect and replay workflow graders. `graders run` runs one grader declared by a
local workflow against a saved run payload or JSON from standard input.
`graders operational-value` regrades the operational-value observation from a
completed workflow run at an explicit evidence cutoff. It verifies and executes
the evaluator archived by the run without modifying the original artifact.

```bash wrap
gh aw graders run weekly-research loops 123456789
cat payload.json | gh aw graders run weekly-research loops
gh aw graders operational-value 123456789 \
  --evidence-at 2026-08-30T12:00:00.000Z --json
```

**Options:** `--evidence-at` (required), `--json/-j`, `--repo/-r`

#### `outcomes`

Check what happened to a workflow run's safe outputs (accepted, rejected, ignored, or pending).

```bash wrap
gh aw outcomes 1234567890                      # Check outcomes for a specific run
gh aw outcomes 1234567890 --json               # JSON output
gh aw outcomes 1234567890 --repo owner/repo    # Specify repository
gh aw outcomes 1234567890 --outcomes-dir ./otlp # Write outcome JSONL for OTLP export
```

**Options:** `--json/-j`, `--repo/-r`, `--output/-o`, `--outcomes-dir`

##### `outcomes history`

Score recent issues and merged pull requests against the objective mapping. Gives a quick local historical view of what kinds of work the repository has been closing or merging under the current objective mapping.

```bash wrap
gh aw outcomes history                               # Score recent issues and PRs
gh aw outcomes history --source issues --limit 100  # Only issues, limited to 100 items
gh aw outcomes history --repo owner/repo --json     # JSON output for another repo
```

**Options:** `--limit`, `--source`, `--json/-j`, `--repo/-r`

#### `models`

List model catalog pricing, built-in aliases and their resolution order, and models observed in local automation artifacts.

```bash wrap
gh aw models                              # Catalog, aliases, and observed models
gh aw models --json                       # JSON output
gh aw models --logs-dir .github/aw/logs   # Read observed models from another logs directory
gh aw models --refresh-count 50           # Inspect more recent runs when refreshing
gh aw models --refresh-observed=false     # Skip the artifact refresh (local data only)
```

Observed models are aggregated from `summary.json` token usage, per-run token usage artifacts, and `awf-reflect.json` endpoint model lists. By default the command first refreshes those artifacts from recent runs; the refresh writes no report of its own, so `--json` output stays a single JSON document.

**Options:** `--json/-j`, `--logs-dir`, `--refresh-observed`, `--refresh-count`, `--repo/-r`

#### `health`

Display workflow health metrics and success rates.

```bash wrap
gh aw health                       # Summary of all workflows (last 7 days)
gh aw health issue-monster         # Detailed metrics for specific workflow
gh aw health --days 30             # Summary for last 30 days
gh aw health --threshold 90        # Warn if below 90% success rate
gh aw health --json                # Output in JSON format
gh aw health issue-monster --days 90  # 90-day metrics for workflow
```

**Options:** `--days`, `--threshold`, `--repo/-r`, `--json/-j`

The `--days` flag accepts 7, 30, or 90 (default: 7). Other values produce an error.

Shows success/failure rates, trend indicators (↑ improving, → stable, ↓ degrading), execution duration, token usage, costs, and warnings when success rate drops below threshold.

Runs that never dispatched a job are excluded from these metrics: `skipped` runs (the activation condition evaluated to false, for example a comment that does not contain the workflow's command) and `action_required` runs (created by GitHub but held for manual approval, for example comment events authored by a bot actor). Including them would report command workflows as near-0% success even when every dispatched run succeeded.

#### `checks`

Classify CI check state for a pull request and emit a normalized result.

```bash wrap
gh aw checks 42                              # Classify checks for PR #42
gh aw checks 42 --repo owner/repo           # Specify repository
gh aw checks 42 --json                      # Output in JSON format
gh aw checks 42 --head-sha <sha>            # Skip the PR head SHA lookup (use pre-resolved SHA)
```

**Options:** `--repo/-r`, `--json/-j`, `--head-sha`

Maps PR check rollups to one of the following normalized states: `success`, `failed`, `pending`, `no_checks`, `policy_blocked`. JSON output includes two state fields: `state` (aggregate across all checks) and `required_state` (derived from required checks only, ignoring optional third-party statuses like deployment integrations).

`--head-sha` accepts a pre-resolved commit SHA (e.g. from `gh pr list --json headRefOid`) and skips the REST call that would otherwise fetch it from the PR. Use this flag when the SHA is already available to reduce API consumption.

#### `forecast`

Forecast AI Credit (AIC) usage for agentic workflows using recent run history and statistical simulation. All forecasts are estimates derived from historical samples and may be inaccurate.

```bash wrap
gh aw forecast                              # Forecast all workflows (monthly)
gh aw forecast ci-doctor                    # Forecast a specific workflow
gh aw forecast ci-doctor daily-news         # Compare two workflows
gh aw forecast --period week                # Weekly projections
gh aw forecast --days 7                     # Use 7-day history window
gh aw forecast --sample 50                  # Sample up to 50 runs per workflow
gh aw forecast --json                       # Machine-readable JSON output
gh aw forecast --repo owner/repo            # Forecast in another repository
gh aw forecast --eval                       # Backtest forecast quality against past data
```

**Options:** `--concurrency`, `--days`, `--period`, `--sample`, `--eval`, `--timeout`, `--repo/-r`, `--json/-j`

The `--days` flag accepts only `7` or `30` (default: `30`). Other values produce an error.

#### `experiments`

Inspect experiment state tracked in `experiments/*` branches. The default command behavior matches `experiments list`; use `experiments analyze` for per-workflow statistics.

```bash wrap
gh aw experiments                          # List experiment workflow branches
gh aw experiments list --json             # List all experiments in JSON format
gh aw experiments analyze my-workflow     # Analyze one experiment workflow
gh aw experiments analyze my-workflow --json  # Analyze one experiment workflow as JSON
gh aw experiments analyze my-workflow --repo owner/repo  # Analyze experiments in another repository
```

**Options:** all `experiments` commands accept `--repo/-r`, `--json/-j`

### Management

#### `enable`

Enable one or more workflows by ID, or all workflows if no IDs provided.

```bash wrap
gh aw enable                                # Enable all workflows
gh aw enable ci-doctor                      # Enable specific workflow
gh aw enable ci-doctor daily                # Enable multiple workflows
gh aw enable ci-doctor --repo owner/repo    # Enable in specific repository
```

**Options:** `--repo/-r`

#### `disable`

Disable one or more workflows and cancel any in-progress runs.

```bash wrap
gh aw disable                               # Disable all workflows
gh aw disable ci-doctor                     # Disable specific workflow
gh aw disable ci-doctor daily               # Disable multiple workflows
gh aw disable ci-doctor --repo owner/repo   # Disable in specific repository
```

**Options:** `--repo/-r`

#### `remove`

Remove workflows (both `.md` and `.lock.yml`). Accepts a workflow ID (basename without `.md`) or a substring pattern matching multiple workflows. By default, also removes orphaned include files no longer referenced by any workflow.

```bash wrap
gh aw remove my-workflow                        # Remove specific workflow
gh aw remove test-                              # Remove all workflows containing 'test-' in their name
gh aw remove my-workflow --no-remove-orphans    # Remove but keep orphaned include files
```

**Options:** `--dir/-d`, `--no-remove-orphans`

#### `update`

Update workflows based on `source` field (`owner/repo/path@ref`). By default, performs a 3-way merge to preserve local changes; use `--no-merge` to override with upstream. Semantic versions update within same major version.

By default, `update` also force-updates all GitHub Actions referenced in your workflows (both in `actions-lock.json` and workflow files) to their latest major version. Use `--no-release-bump` to restrict force-updates to core `actions/*` actions only.

If no workflows in the repository contain a `source` field, the command exits gracefully with an informational message rather than an error. This is expected behavior for repositories that have not yet added updatable workflows.

```bash wrap
gh aw update                              # Update all with source field
gh aw update ci-doctor                    # Update specific workflow (3-way merge)
gh aw update ci-doctor --no-merge         # Override local changes with upstream
gh aw update ci-doctor --major --force    # Allow major version updates
gh aw update --no-release-bump            # Update workflows; only force-update core actions/*
gh aw update --repo owner/repo            # Update workflows in another repository
gh aw update --create-pull-request        # Update and open a pull request
gh aw update --org my-org --create-issue --yes  # Auto-accept per-repo confirmations (required in CI)
```

**Options:** `--dir/-d`, `--no-merge`, `--major`, `--force/-f`, `--engine/-e`, `--no-stop-after`, `--stop-after`, `--no-release-bump`, `--no-security-scanner`, `--approve`, `--create-pull-request`, `--create-issue`, `--org`, `--repos`, `--yes/-y`, `--no-compile`, `--no-redirect`, `--cool-down`, `--repo/-r`

Org mode (`--org`) previews or creates workflow update pull requests across every repository in an organization. Use `--repos` to limit org mode to repositories matching one or more glob patterns, `--create-issue` to open an issue in each repository that has pending updates (requires `--org`), and `--yes/-y` to auto-accept per-repository confirmations (required in CI).

The `--no-redirect` flag causes `update` to fail when the source workflow has a [`redirect`](/gh-aw/reference/frontmatter/) field, rather than following the redirect to its new location. Use this when you want explicit control over redirect handling.

The `--repo/-r` flag runs the update against a different repository. The target repository is checked out in an isolated shallow clone under `.github/aw/updates/<sanitized-repo-id>`. When combined with `--create-pull-request`, the resulting PR is opened against the target repository instead of the current one.

#### `deploy`

Roll out one or more workflows to a target repository through a pull request. The command clones the target repository into an isolated shallow checkout, refreshes existing sourced workflows, adds the requested workflows, recompiles lock files with purge enabled, and opens a pull request against the target repository.

```bash wrap
gh aw deploy githubnext/agentics/ci-doctor --repo owner/repo
gh aw deploy githubnext/agentics/repo-assist githubnext/agentics/ci-doctor --repo owner/repo --force
gh aw deploy ./my-workflow.md --repo owner/repo
gh aw deploy githubnext/agentics/ci-doctor --org my-org --repos '*-service' --yes  # Deploy across an org
```

**Options:** `--repo/-r` (required unless `--org`), `--name/-n`, `--engine/-e`, `--force/-f`, `--append`, `--no-gitattributes`, `--dir/-d`, `--no-stop-after`, `--stop-after`, `--no-security-scanner`, `--cool-down`, `--org`, `--repos`, `--yes/-y`

Org mode (`--org`) deploys workflows across every repository in an organization instead of a single `--repo` target. Use `--repos` to limit org mode to repositories matching one or more glob patterns, and `--yes/-y` to auto-accept org-mode deploy confirmations (required in CI).

The `--repo` flag accepts `owner/repo` form and is required unless `--org` is provided. The target repository is checked out under `.github/aw/updates/<sanitized-repo-id>` inside the current working tree, so the command must be run from inside a git repository. Workflows already present in the target with a `source` frontmatter field are refreshed through the update phase and skipped by the add phase to avoid duplicate-add errors. The pull request commit title is `chore: deploy agentic workflows`. The default `--cool-down` value is `7d`.

#### `upgrade`

Upgrade repository with latest agent files and apply codemods to all workflows.

```bash wrap
gh aw upgrade                              # Upgrade repository agent files and all workflows
gh aw upgrade --no-fix                     # Update agent files only (skip codemods, actions, and compilation)
gh aw upgrade --create-pull-request        # Upgrade and open a pull request
gh aw upgrade --engine claude              # Override AI engine for compilation
gh aw upgrade --repo owner/repo            # Upgrade workflows in another repository
gh aw upgrade --audit                      # Run dependency health audit
gh aw upgrade --audit --json               # Dependency audit in JSON format
gh aw upgrade --org my-org --create-issue --yes  # Auto-accept per-repo confirmations (required in CI)
```

**Options:** `--dir/-d`, `--engine/-e`, `--repo/-r`, `--no-fix`, `--no-actions`, `--no-compile`, `--disable-codemod`, `--create-pull-request`, `--create-issue`, `--org`, `--repos`, `--yes/-y`, `--audit`, `--json/-j`, `--approve`, `--pre-releases`

Org mode (`--org`) previews or creates upgrade pull requests across every repository in an organization. Use `--repos` to limit org mode to repositories matching one or more glob patterns, `--create-issue` to open an issue in each org repository with agentic workflows (requires `--org`), and `--yes/-y` to auto-accept org-mode upgrade confirmations (required in CI).

Use `--disable-codemod` (repeatable) to skip specific codemod IDs during the embedded fix step. This flag is ignored when `--no-fix` is set.

Unlike `gh aw compile --fix`, `gh aw upgrade` runs codemods, action version updates, and workflow compilation by default and uses `--no-fix` to skip all three steps.

#### `env`

Manage compiler defaults as GitHub variables at repository, organization, or enterprise scope.

##### `env get [file]`

Download default compiler variables into a YAML file (`file.yml` by default).

```bash wrap
gh aw env get
gh aw env get defaults.yml --scope repo
gh aw env get org-defaults.yml --scope org --org my-org
gh aw env get ent-defaults.yml --scope ent --enterprise my-enterprise
```

**Options:** `--scope`, `--repo/-r`, `--org`, `--enterprise`

For repository scope, `--repo` currently accepts `owner/repo` only. To target GitHub Enterprise Server, select the host via `GH_HOST` rather than prefixing the repository with `[HOST/]`.

##### `env update [file]`

Upload default compiler variables from a YAML file (`file.yml` by default). Use `null` (or omit a field) to delete that variable in the selected scope.

```bash wrap
gh aw env update defaults.yml --scope repo
gh aw env update defaults.yml --scope org --org my-org --dry-run
gh aw env update defaults.yml --scope ent --enterprise my-enterprise --yes
```

**Options:** `--scope` (required), `--repo/-r`, `--org`, `--enterprise`, `--yes/-y`, `--dry-run`

For repository scope, `--repo` currently accepts `owner/repo` only. To target GitHub Enterprise Server, select the host via `GH_HOST` rather than prefixing the repository with `[HOST/]`.

### Advanced

#### `mcp`

Manage MCP (Model Context Protocol) servers in workflows. `mcp inspect` auto-detects mcp-scripts.

```bash wrap
gh aw mcp list workflow                    # List servers for workflow
gh aw mcp list-tools --server github           # List tools for a server (all workflows)
gh aw mcp list-tools workflow --server github  # List tools for a server in a specific workflow
gh aw mcp inspect                          # List workflows with MCP servers
gh aw mcp inspect workflow                 # Inspect and test servers
gh aw mcp inspect workflow --server github  # Inspect only one server
gh aw mcp inspect workflow --server github --tool create_issue  # Show one tool in detail
gh aw mcp inspect workflow --inspector      # Launch the MCP inspector
gh aw mcp inspect workflow --check-secrets  # Check required GitHub Actions secrets
gh aw mcp add                              # List available MCP servers from the registry
gh aw mcp add workflow server              # Add an MCP server to a workflow
gh aw mcp add workflow server --transport stdio   # Prefer stdio transport
gh aw mcp add workflow server --registry https://custom.registry.com/v1  # Use custom registry
gh aw mcp add workflow server --tool-id my-server  # Override the tool ID
```

**`mcp inspect` options:** `--check-secrets`, `--inspector`, `--server`, `--tool`

**`mcp add` options:** `--transport`, `--registry`, `--tool-id`

See [MCPs Guide](/gh-aw/guides/mcps/).

#### `pr transfer`

Transfer pull request to another repository, preserving changes, title, and description.

```bash wrap
gh aw pr transfer <pr-url> --repo target-owner/target-repo
```

**Options:** `--repo/-r`

#### `mcp-server`

Run MCP server exposing gh-aw commands as tools. Spawns subprocesses to isolate GitHub tokens.

```bash wrap
gh aw mcp-server                      # stdio transport
gh aw mcp-server --port 8080          # HTTP server with SSE
gh aw mcp-server --validate-actor     # Enable actor validation
```

**Options:** `--port/-p` (HTTP server port), `--cmd` (custom subprocess command), `--validate-actor` (enforce actor validation for logs and audit tools)

**Available Tools:** status, compile, logs, audit, checks, mcp-inspect, add, update, fix

When `--validate-actor` is enabled, logs and audit tools require write+ repository access via GitHub API (permissions cached for 1 hour). See [MCP Server Guide](/gh-aw/reference/gh-aw-as-mcp-server/).

#### `domains`

List network domains configured in agentic workflows.

```bash wrap
gh aw domains                           # List all workflows with domain counts
gh aw domains weekly-research           # List domains for specific workflow
gh aw domains --json                    # Output summary in JSON format
gh aw domains weekly-research --json    # Output workflow domains in JSON format
```

**Options:** `--json/-j`

When no workflow is specified, lists all workflows with a summary of allowed and blocked domain counts. When a workflow is specified, lists all effective allowed and blocked domains including domains expanded from ecosystem identifiers (e.g., `node`, `python`, `github`) and engine defaults.

### Utility Commands

#### `version`

Print the current version and build information for the gh aw CLI extension.

```bash wrap
gh aw version
```

#### `completion`

Generate and manage shell completion scripts for tab completion.

```bash wrap
gh aw completion install              # Auto-detect and install
gh aw completion uninstall            # Remove completions
gh aw completion bash                 # Generate bash script
gh aw completion zsh                  # Generate zsh script
gh aw completion fish                 # Generate fish script
gh aw completion powershell           # Generate powershell script
```

**Shell arguments:** `bash`, `zsh`, `fish`, `powershell` — **Subcommands:** `install`, `uninstall`. See [Shell Completions](#shell-completions).

#### `project`

Create and manage GitHub Projects V2 boards.

##### `project new`

Create a new GitHub Project V2 owned by a user or organization with optional repository linking.

```bash wrap
gh aw project new "My Project" --owner @me                      # Create user project
gh aw project new "Team Board" --owner myorg                    # Create org project
gh aw project new "Bugs" --owner myorg --link myorg/myrepo     # Create and link to repo
gh aw project new "Project Q1" --owner myorg --with-project-setup # Create with standard views and fields
```

**Options:**
- `--owner` (required): Project owner - use `@me` for current user or specify organization name
- `--link/-l`: Repository to link project to (format: `owner/repo`)
- `--with-project-setup`: Create standard project views and custom fields

**Token Requirements:**

> [!IMPORTANT]
> The default `GITHUB_TOKEN` cannot create projects. Use a Personal Access Token (PAT) with Projects permissions:
>
> - **Classic PAT**: `project` scope (user projects) or `project` + `repo` (org projects)
> - **Fine-grained PAT**: Organization permissions → Projects: Read & Write
>
> Configure via `GH_AW_PROJECT_GITHUB_TOKEN` environment variable or `gh auth login`. See [Authentication](/gh-aw/reference/auth/).

#### `hash-frontmatter`

Compute a deterministic SHA-256 hash of workflow frontmatter for detecting configuration changes.

```bash wrap
gh aw hash-frontmatter my-workflow.md
gh aw hash-frontmatter .github/workflows/audit-workflows.md
```

Includes all frontmatter fields, imported workflow frontmatter (BFS traversal), template expressions containing `env.` or `vars.`, and version information (gh-aw, awf, agents).

## Shell Completions

Enable tab completion for workflow names, engines, and paths. After running `gh aw completion install`, restart your shell or source your configuration file.

### Manual Installation

```bash wrap
# Bash
gh aw completion bash > ~/.bash_completion.d/gh-aw && source ~/.bash_completion.d/gh-aw

# Zsh
gh aw completion zsh > "${fpath[1]}/_gh-aw" && compinit

# Fish
gh aw completion fish > ~/.config/fish/completions/gh-aw.fish

# PowerShell
gh aw completion powershell | Out-String | Invoke-Expression
```

## Advanced and enterprise setup

<details>
<summary><strong>GitHub Enterprise Server support</strong></summary>

### GitHub Enterprise Server support

For GitHub Enterprise Server deployments:

```bash wrap
export GH_HOST="github.enterprise.com"                           # Set hostname
gh auth login --hostname github.enterprise.com                   # Authenticate
gh aw logs workflow --repo github.enterprise.com/owner/repo      # Use with commands
```

For GHE Cloud with data residency (`*.ghe.com`), see the dedicated [Debugging GHE Cloud guide](/gh-aw/troubleshooting/debug-ghe/).

Commands that support `--create-pull-request` — including `gh aw add`, `gh aw init`, `gh aw update`, and `gh aw upgrade` — automatically detect the enterprise host from the git remote and route PR creation to the correct GHES instance. `gh aw audit` and `gh aw add-wizard` do the same, so running them inside a GHES repository usually does not require setting `GH_HOST` manually.

#### Configuring `gh` CLI on GHES

The compiled agent job automatically runs `configure_gh_for_ghe.sh` before the agent starts. The script reads `GITHUB_SERVER_URL` and configures `gh` for that host, so the agent can use `gh` commands on GHES without extra setup.

Custom workflow jobs and the safe-outputs job also derive `GH_HOST` from `GITHUB_SERVER_URL` at startup. On github.com this is a no-op; on GHES or GHEC it ensures `gh` targets the correct instance automatically.

For custom `steps:` that require additional authentication setup (for example, when running `gh` commands without a `GH_TOKEN` in scope), the helper script is available:

```yaml wrap
steps:
  - name: Configure gh for GHE
    run: source /opt/gh-aw/actions/configure_gh_for_ghe.sh

  - name: Fetch repository data
    env:
      GH_TOKEN: ${{ github.token }}
    run: |
      gh issue list --state open --limit 500 --json number,labels
      gh pr list --state open --limit 200 --json number,title
```

The setup action installs the script at `/opt/gh-aw/actions/configure_gh_for_ghe.sh`. If `GH_TOKEN` is already set, the script skips `gh auth login` and only exports `GH_HOST`.

> [!NOTE]
> Custom steps run outside the agent firewall sandbox and have access to standard GitHub Actions environment variables including `GITHUB_SERVER_URL`, `GITHUB_TOKEN`, and `GH_TOKEN`.
</details>

## Debug Logging

Enable detailed debugging with namespace, message, and time diffs.

```bash wrap
DEBUG=* gh aw compile                # All logs
DEBUG=cli:* gh aw compile            # CLI only
DEBUG=*,-tests gh aw compile         # All except tests
```

Use `--verbose` flag for user-facing details.

## Smart Features

### Fuzzy Workflow Name Matching

Auto-suggests similar workflow names on typos using Levenshtein distance.

```bash wrap
gh aw compile audti-workflows
# ✗ workflow file not found
# Did you mean: audit-workflows?
```

Works with: compile, enable, disable, logs, mcp commands.

## Troubleshooting

| Issue | Solution |
|-------|----------|
| `command not found: gh` | Install from [cli.github.com](https://cli.github.com/) |
| `extension not found: aw` | Run `gh extension install github/gh-aw` |
| Compilation fails with YAML errors | Check indentation, colons, and array syntax in frontmatter |
| Workflow not found | Check typo suggestions or run `gh aw status` to list available workflows |
| Permission denied | Check file permissions or repository access |
| Trial creation fails | Check GitHub rate limits and authentication |

See [Common Issues](/gh-aw/troubleshooting/common-issues/) and [Error Reference](/gh-aw/troubleshooting/errors/) for detailed troubleshooting.

## Learn More

- [Quick Start](/gh-aw/setup/quick-start/) - Get your first workflow running
- [Frontmatter](/gh-aw/reference/frontmatter/) - Configuration options
- [Adding Existing Workflows](/gh-aw/guides/working-with-workflows/#adding-existing-workflows) - Adding workflows from other repositories
- [Security Guide](/gh-aw/introduction/architecture/) - Security best practices
- [MCP Server Guide](/gh-aw/reference/gh-aw-as-mcp-server/) - MCP server configuration
- [Agent Factory](/gh-aw/agent-factory-status/) - Agent factory status
