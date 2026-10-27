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

## Utility and advanced commands

| Command | Description | When to use |
|---------|-------------|-------------|
| [`gh aw skill`](#skill) | Print the bundled agentic-workflows skill | Loading instructions into an agent without adding repository files |
| [`gh aw work-queue`](#work-queue) | Experimental Git-backed DAG work queue | Managing queue policy, scheduling, and operator recovery |

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

### The `--push` Flag

`gh aw run --push` stages workflow files (including transitive imports), commits them, and pushes before dispatching the workflow. It refuses to proceed when unrelated files are already staged.

For `init`, `update`, and `upgrade`, use `--create-pull-request` instead.

Commands that create pull requests print the full pull request URL to stderr immediately after creation, including in non-verbose and quiet modes, so terminals can display it as a clickable link. The URL remains visible if subsequent local branch cleanup fails.

### Pull request descriptions

`init`, `add`, `add-wizard`, `update`, `upgrade`, `deploy`, and `pr transfer` include a structured summary and a `Generated by gh aw v...` footer in created pull requests. Update and upgrade descriptions list dependency pin changes, affected files, and links to the versions used; upgrade also reports the codemods actually applied.

Use `--pr-message` to append a custom Markdown message without replacing the generated description. The message appears before the version footer and is passed to each created pull request in `--repo` and `--org` modes. This option does not enable PR creation by itself.

Large summaries group compiler updates and applied codemods, and show up to 50 changed file paths; the full pin-change list is retained. Descriptions that still exceed GitHub's 65,536-character limit are rejected before committing or pushing the PR branch.

```bash wrap
gh aw update --create-pull-request --pr-message "Part of the quarterly automation refresh."
gh aw upgrade --org my-org --pr --yes --pr-message "Tracking issue: #123."
```

### Pull request auto-merge

`--auto-merge` enables GitHub-native auto-merge on the created pull request. Without this flag, command behavior is unchanged.

| Commands | Options |
|----------|---------|
| `init`, `add`, `update`, `upgrade` | `--auto-merge` requires `--pr` or its equivalent `--create-pull-request` |
| `add-wizard` | `--pr` / `--create-pull-request` forces pull request delivery instead of asking for confirmation; `--auto-merge` requires one of these flags |
| `deploy`, `pr transfer` | Always create a pull request, but `--auto-merge` still requires the compatibility flag `--pr` or `--create-pull-request` |

The repository must allow auto-merge and at least one merge method, and the authenticated token must have permission to enable it through the GitHub API. The CLI uses the GraphQL `enablePullRequestAutoMerge` mutation, choosing the repository's `viewerDefaultMergeMethod` when allowed, otherwise an allowed merge method. It does not immediately merge through `gh pr merge`, force a merge, or bypass required checks or reviews.

If auto-merge is disabled, no merge method is allowed, API permissions are insufficient, or GitHub otherwise rejects the request, the command reports the error and pull request URL; the created pull request remains open. In organization mode, `update`, `upgrade`, and `deploy` enable auto-merge independently for each repository and report per-repository failures without preventing attempts for other repositories.

## Commands

Commands are organized by workflow lifecycle: creating, building, testing, monitoring, and managing workflows.

To choose between the setup commands: [`add-wizard`](#add-wizard) is guided and interactive (prompts for engine auth and secrets), [`add`](#add) installs a local, remote, or packaged workflow non-interactively, and [`new`](#new) scaffolds a new workflow template.

### Getting Workflows

#### `skill`

Print the bundled `/agentic-workflows` `SKILL.md` content to standard output without initializing a repository or creating files:

```bash wrap
gh aw skill
```

The command works outside a Git repository, requires no authentication or network access, and uses the file list bundled with the installed CLI version. The skill includes public raw-URL fallback instructions for GitHub API downloads blocked by HTTP 403 or cross-organization SAML authorization.

#### `init`

Initialize repository for agentic workflows. Configures `.gitattributes`, creates the dispatcher skill file (`.github/skills/agentic-workflows/SKILL.md`), and performs non-interactive setup. With the Copilot engine (`--engine copilot`), it also creates the Agentic Workflows custom agent (`.github/agents/agentic-workflows.md`) and enables MCP server integration by default (use `--no-mcp`/`--no-agent` to skip these Copilot-specific artifacts). Use `--no-skill` to skip dispatcher skill creation. Non-Copilot engines skip Copilot-specific artifacts; see [Initializing for non-Copilot engines](#initializing-for-non-copilot-engines).

Initialization can use a token SAML-authorized for a different organization: if the public skill file-list API returns HTTP 403, `init` fetches the list from `raw.githubusercontent.com` without authentication. If that also fails, it warns and uses the bundled list. Creating a pull request still requires authorization for the target repository.

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
gh aw init --pr --auto-merge            # Initialize, open a PR, and enable auto-merge
```

**Options:** `--engine/-e`, `--no-mcp`, `--no-skill`, `--no-agent`, `--codespaces`, `--completions`, `--create-pull-request` / `--pr`, `--auto-merge` (requires PR creation; see [auto-merge requirements](#pull-request-auto-merge))

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

`--pr` / `--create-pull-request` forces pull request delivery but does not make the wizard non-interactive. With `--auto-merge`, GitHub-native auto-merge is enabled; the wizard's review and merge-confirmation loop still runs before post-merge secret setup, bootstrap, and status steps.

```bash wrap
gh aw add-wizard githubnext/agentics/ci-doctor           # Interactive setup
gh aw add-wizard https://github.com/org/repo/blob/main/workflows/my-workflow.md
gh aw add-wizard https://example.com/workflows/my-workflow.json   # Arbitrary URL (JSON workflow)
gh aw add-wizard githubnext/agentics/ci-doctor --no-secret  # Skip secret prompt
gh aw add-wizard githubnext/agentics/ci-doctor --pr --auto-merge  # Force PR delivery and enable auto-merge
```

**Options:** `--no-secret`, `--dir/-d`, `--engine/-e`, `--gh-aw-ref`, `--no-gitattributes`, `--no-stop-after`, `--stop-after`, `--append`, `--no-security-scanner`, `--no-config`, `--create-pull-request` / `--pr` (force PR delivery), `--auto-merge` (requires explicit PR flag; see [auto-merge requirements](#pull-request-auto-merge))

When the Copilot engine is selected, the wizard prompts the user to choose an authentication method: organization billing via [`permissions.copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission) (no PAT required), or a [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token) personal access token (a separate token from the default `GITHUB_TOKEN`, because the agent needs elevated Copilot API access that the ephemeral workflow token does not carry). When `COPILOT_GITHUB_TOKEN` already exists, using it is the default; because GitHub does not expose stored secret values, the user asserts that it is a suitable fine-grained PAT with Copilot Requests permission. The alternative replacement path opens a preconfigured fine-grained PAT creation page, validates the pasted token, and updates the repository secret.

When the Claude engine is selected, the wizard configures [`ANTHROPIC_API_KEY`](/gh-aw/reference/auth/#anthropic_api_key) on the pull request path: it reuses an available repository or organization secret, uploads a key from the local environment, or prompts for a key from Anthropic Console and stores it as a repository secret. Secret setup requires repository write access and is skipped with `--no-secret`. For keyless authentication, configure [Anthropic Workload Identity Federation (WIF)](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif) separately in `engine.auth`, grant `permissions.id-token: write`, and use `--no-secret` to skip the API-key prompt. `CLAUDE_CODE_OAUTH_TOKEN`, including a token from `claude login`, is not supported; when no API key is available and this environment variable is set, the wizard fails with an error directing the user to `ANTHROPIC_API_KEY` or Anthropic WIF.

#### `add`

Add workflows from the Agentics collection or other repositories to `.github/workflows`. For remote workflows, this command follows frontmatter [`redirect`](/gh-aw/reference/frontmatter/#redirect-redirect) declarations before installation.

```bash wrap
gh aw add githubnext/agentics/ci-doctor           # Add single workflow
gh aw add githubnext/agentics/ci-doctor@v1.0.0   # Add specific version
gh aw add ./my-workflow.md                       # Add a local workflow file
gh aw add ./*.md                                 # Add multiple local workflow files
gh aw add githubnext/agentics/ci-doctor --dir .github/workflows/shared  # Organize in subdirectory
gh aw add githubnext/agentics/ci-doctor --create-pull-request        # Create PR instead of commit
gh aw add githubnext/agentics/ci-doctor --pr --auto-merge             # Create PR and enable auto-merge
gh aw add https://example.com/workflows/my-workflow.md               # Arbitrary HTTPS URL (markdown)
gh aw add https://example.com/workflows/my-workflow.json             # Arbitrary HTTPS URL (JSON workflow definition)
```

**Options:** `--dir/-d`, `--create-pull-request` / `--pr`, `--auto-merge` (requires PR creation; see [auto-merge requirements](#pull-request-auto-merge)), `--no-gitattributes`, `--append`, `--no-security-scanner`, `--engine/-e`, `--force/-f`, `--gh-aw-ref`, `--name/-n`, `--no-stop-after`, `--stop-after`

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

For Claude workflows, set `ANTHROPIC_API_KEY` or configure [Anthropic WIF](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif). `CLAUDE_CODE_OAUTH_TOKEN`, including a token from `claude login`, is not supported; the CLI rejects it with an error naming `CLAUDE_CODE_OAUTH_TOKEN` and pointing to these alternatives.

##### `secrets bootstrap`

Analyze workflows to determine required secrets and interactively prompt for missing ones. Auto-detects engines in use and checks which required secrets are already configured.

```bash wrap
gh aw secrets bootstrap                                  # Analyze all workflows and prompt for missing secrets
gh aw secrets bootstrap --engine copilot                 # Check only Copilot secrets
gh aw secrets bootstrap --non-interactive                # Display missing secrets without prompting
```

**Options:** `--engine/-e` (copilot, claude, codex, gemini, pi, agy (experimental)), `--non-interactive`, `--repo/-r`

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

#### `format`

Apply all available codemods and normalize YAML frontmatter. Formatting uses two-space indentation and deterministic field ordering while retaining comments and Markdown content. If no workflow is specified, all Markdown files in `.github/workflows` are formatted.

```bash wrap
gh aw format                           # Format all workflows
gh aw format my-workflow               # Format specific workflow
gh aw format --dir custom/workflows    # Format a different directory
```

**Options:** `--dir/-d`

#### `edit`

Edit schema-validated workflow frontmatter and recompile the generated file. Changes are validated before writing.

This command is experimental. Frontmatter is re-serialized whenever it changes, so YAML comments, key ordering, and quoting styles are not preserved. Edits that change nothing leave the workflow untouched.

```bash wrap
gh aw edit repo-assist "max-turns: 20"
gh aw edit repo-assist --schedule "every 6h"
gh aw edit repo-assist --set model=small --unset engine.model
gh aw edit repo-assist --add-import shared/common.md
gh aw edit repo-assist --add-skill shared/review
```

The workflow argument accepts a workflow name, a Markdown filename, or a path. Workflows managed by a `source:` declaration can be edited locally, and future updates merge in those local changes by default.

**Options:** `--set`, `--unset`, `--add`, `--remove`, `--add-import`, `--remove-import`, `--add-skill`, `--remove-skill`, `--schedule`, `--dry-run`, `--dir/-d`

The `--set`, `--unset`, `--add`, and `--remove` flags are repeatable and take a frontmatter path, such as `--set engine.model=small`. Use `--schedule off` to remove an existing schedule.

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

For `--zizmor` and `--poutine`, compilation automatically uses a binary on `PATH` when its version is at least 1.31.1 or 1.1.6, respectively. The version is checked with `zizmor --version` or `poutine version --disable-version-check`. If the binary is missing, too old, or its version cannot be determined, the scanner uses the pinned Docker image instead. Other Docker-based checks still require Docker.

If the repository root contains an [`aw.yml` manifest](/gh-aw/reference/aw-yml-package-manifest/), `gh aw compile` validates it before compiling workflows.

Unlike `gh aw upgrade`, `gh aw compile` does not run codemods unless you pass `--fix`.

**Options:** `--action-mode`, `--action-tag`, `--actionlint`, `--actions-repo`, `--allow-action-refs`, `--allow-experimental`, `--approve`, `--dependabot`, `--dry-run`, `--dir/-d`, `--engine/-e`, `--environment`, `--fail-fast`, `--fix`, `--force/-f`, `--force-refresh-action-pins`, `--force-refresh-container-pins`, `--gh-aw-ref`, `--ghes`, `--grant`, `--grype`, `--json/-j`, `--logical-repo/-l`, `--models`, `--no-check-update`, `--no-emit`, `--no-lint`, `--no-logo`, `--poutine`, `--purge`, `--refresh-stop-time`, `--require-self-hosted-runners`, `--runner-guard`, `--schedule-seed`, `--shellcheck`, `--show-all`, `--staged`, `--stats`, `--strict`, `--syft`, `--trial`, `--validate`, `--validate-images`, `--watch/-w`, `--yamllint`, `--zizmor`

**`--no-lint` flag:** Disable default built-in Actions/AW AST linting. By default, compilation checks the generated execution graph and authored source before emitting a lock file, including with `--no-emit`, and fails on lint errors. This flag does not disable existing compiler validation or opt-in external scanners and cannot be combined with `--dry-run`. The compile MCP input is `skip_lint`. Native Actions YAML and downstream dispatch targets require a separate `gh aw lint` invocation.

`add`, `update`, and `validate` also run built-in lint whenever they recompile workflows. Only `compile` exposes `--no-lint`; fix the reported findings before using other recompiling commands.

To migrate existing workflows, run `gh aw compile --no-emit`, fix findings in the authored source, then rerun the original command. `--no-lint` is a per-compilation escape hatch, not a persistent setting for other commands.

Compile-time lint is Actions/AW AST-only and does not require a native JavaScript parser. Run standalone `gh aw lint --javascript-module <module-path>` for deeper authored JavaScript driver and custom-grader checks. Missing-parser coverage warnings remain advisory and do not block compiler dry-run; successful compilation does not establish complete JavaScript analysis.

**`--no-logo` flag:** Omit the ASCII logo from generated workflow headers without removing metadata or regeneration instructions. To persist this setting for all compiles, including upgrades, set [`"lock_file_logo": false` in `.github/workflows/aw.json`](/gh-aw/reference/compilation-process/#repository-configuration-awjson). Either opt-out disables the logo; `--banner` only controls terminal output.

**`--dry-run` flag:** Compile with strict source validation, staged safe outputs, shellcheck, and model checks. Existing and added medium-or-higher or unclassified scanner findings fail; baseline counts remain available for comparison. Low-severity findings and zizmor's `secrets-outside-env` rule do not fail the gate. Docker-based image validation and scanners are opt-in; native shellcheck provides required run-step linting without Docker. Missing required checks or model inventory block compilation. Compiler-managed GitHub mutations and authored custom jobs are disabled. Custom jobs and custom safe-output jobs become credential-free placeholders, including reusable calls with inherited secrets. Authored host steps, custom MCP servers, `mcp-scripts`, custom safe-output scripts, enabled Bash graders, and authored OIDC permissions are rejected, including imported declarations. Remove unsupported declarations in a separate diagnostic workflow. Emitted locks remove telemetry environment variables and automatic OTLP authentication. Agent shell commands, custom engine execution, custom credentials, and hosted identities still require review; dry-run is not a hosted sandbox. Explicitly disabling a required check is rejected; `--no-emit`, `--watch`, `--approve`, and `--allow-action-refs` are incompatible. This flag does not upload or execute workflows and is distinct from `--action-mode dev`. The compile MCP input is `dry_run`; `validate_images: true` opts into required image validation. See [Development Testing Mode](/gh-aw/reference/compilation-process/#development-testing-mode).

Dry-run locks also omit daily credit accounting, retain existing per-run caps or promote configured/imported daily limits, and add manual dispatch restricted to maintainer/admin actors without bot exemptions. `DEBUG=workflow:compiler_development,cli:compile_development` reports mutations without configuration values.

**`--environment NAME` flag:** Replace the environment on every compiled job, including approval and custom jobs. The name must be literal, non-blank, at most 255 characters, and contain no control characters or GitHub Actions expressions. Reusable-workflow caller jobs cause an error because GitHub Actions forbids an environment on them. Environment overrides do not isolate authorized repository/organization/enterprise secrets or grant execution permission; review replacement approval protections before testing.

**`--gh-aw-ref` flag:** Convenience alias for `--action-mode release --action-tag <ref>`. Accepts a branch name, tag, or commit SHA targeting the `github/gh-aw` repository. Branch and tag names are resolved to their full commit SHA at compile time, so the baked-in reference is immutable and reproducible. Useful for E2E-testing workflows compiled against a specific gh-aw revision.

**`--models` flag:** Refreshes the observed model inventory using the same data sources as `gh aw models`, then warns when `models.allowed`, `models.blocked`, or `engine.models` references an unknown model. Built-in and workflow model aliases are accepted. If no observed model data is available, the check is skipped.

**`--approve` flag:** When compiling a workflow that already has a lock file, the compiler enforces *safe update mode* — any newly added secrets or custom actions not present in the previous manifest require explicit approval. Pass `--approve` to accept these changes and regenerate the manifest baseline. On first compile (no existing lock file), enforcement is skipped automatically and `--approve` is not needed.

**Error Reporting:** Displays detailed error messages with file paths, line numbers, column positions, and contextual code snippets.

**JSON Output (`--json`):** Emits an array of `ValidationResult` objects. Each result includes a `labels` field listing all repository labels referenced in safe-outputs (`create-issue.labels`, `create-discussion.labels`, `create-pull-request.labels`, `add-labels.allowed`). Use `--json --no-emit` to collect label references without writing compiled files.

With `--dry-run`, actionable workflow warnings invalidate only the affected workflow. Low-severity and built-in experimental-feature notices are nonblocking and remain visible; `--allow-experimental` additionally records explicit acknowledgement in `accepted_experimental_warnings`. Validation errors, unclassified compiler warnings, and scanner execution/parsing failures remain fatal. The JSON summary reports each scanner's baseline, existing, added, and ignored finding counts. Compiler/scanner diagnostics that cannot be attributed to one workflow appear as separate results with `scope: "batch"`; their `workflow` field identifies the diagnostic source rather than a workflow path. Any invalid result fails development compilation, even when individual workflows remain valid.

**Dependabot Integration (`--dependabot`):** Generates dependency manifests and `.github/dependabot.yml` by analyzing runtime tools across all workflows. See [Dependabot Support reference](/gh-aw/reference/dependabot/).

**Strict Mode (`--strict`):** Enforces security best practices: no write permissions (use [safe-outputs](/gh-aw/reference/safe-outputs/)), explicit `network` config, no wildcard domains, pinned actions, no deprecated fields. See [Strict Mode reference](/gh-aw/reference/frontmatter/#strict-mode-strict).

**Security and Compliance Scanners:**
- **`--syft`:** Generates a Software Bill of Materials (SBOM) for container images referenced in compiled workflows using the Syft scanner.
- **`--grype`:** Scans container images referenced in compiled workflows for known vulnerabilities using the Grype vulnerability scanner. When a `.grype.yaml` file exists at the repository root it is mounted into the scanner and passed to grype via `--config`, so repository-level ignore rules (documented risk acceptances for findings with no upstream fix) are applied.
- **`--runner-guard`:** Runs taint analysis on compiled workflows to detect unsafe data flows from untrusted inputs to sensitive runner operations.

Grype reports each image as planned, then completed with a finding count or failed, using the pinned image reference when available. A completed scan with zero findings confirms that the image was scanned; a failed or missing completion leaves coverage incomplete. Empty or malformed JSON and missing or null `matches` arrays are scan failures, not zero-finding results. Scan failures produce warnings in normal compilation and errors with `--strict`; development compilation with `--dry-run` also treats scanner execution or parsing failures as fatal.

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

Built-in Actions/AW AST lint, external linters (`zizmor`, `actionlint`, `poutine`), `--validate`, and `--no-emit` are always-on defaults and cannot be disabled. Accepts the same workflow ID format as `compile`.

#### `lint`

Lint AW `.md` or Actions `.yml`/`.yaml` files, including compiled `.lock.yml` files. Rules consume parsed ASTs only; file discovery and parsing belong to the CLI adapter. The command never recompiles workflows. Actionlint runs alongside the built-in rules on YAML inputs only. Use `--rules` to select AST rules and skip actionlint. Neither mode runs `zizmor` or `poutine`.

```bash wrap
gh aw lint                                          # Lint workflows in .github/workflows
gh aw lint .github/workflows/foo.lock.yml           # Lint a specific lock file
gh aw lint .github/workflows/foo.md                 # Check the authored AW AST without compiling
gh aw lint .github/workflows/ci.yml                 # Check a native Actions AST
gh aw lint --dir .github/workflows                  # Lint workflows in a directory
gh aw lint --shellcheck --pyflakes                  # Enable actionlint script integrations
gh aw lint --list-rules                             # List built-in AST rules
gh aw lint --rules secret-leak                      # Check existing workflows for secret disclosures
gh aw lint --rules secret-leak --json               # Emit structured diagnostics
gh aw lint --rules secret-leak --debug --json       # Include incomplete-analysis warnings
```

**Options:** `--dir/-d`, `--shellcheck`, `--pyflakes`, `--rules`, `--list-rules`, `--json`, `--debug`, `--javascript-module`, `--javascript-mode`, `--javascript-root`, `--javascript-runtime-path`

By default, shellcheck and pyflakes integrations are disabled to reduce noise for generated `run:` scripts. Built-in actionlint ignore patterns cover gh-aw-specific extensions such as `job.workflow_*` context properties and the `copilot-requests` permission scope.

`--rules` accepts comma-separated rule IDs; `--rules=` selects all registered rules without actionlint. Unknown IDs fail explicitly. `--json` requires `--rules` and has no `-j` shorthand; `--shellcheck` and `--pyflakes` apply only to actionlint. In the default mode, both linters run even if one reports errors, and either linter's failure produces a nonzero exit status.

Secret-leak coverage warnings are hidden by default in text and JSON output, unless JavaScript analysis is explicitly enabled. Use `--debug` to include them otherwise. Error findings, analysis failures, and other linters' warnings remain visible. This is a presentation setting: the AST runner retains all coverage diagnostics, and an empty CLI result is not proof of complete analysis.

Directory scans are nonrecursive and prefer an existing `.lock.yml` over its paired `.md` to avoid duplicate diagnostics. They include uncompiled Markdown with frontmatter and native Actions YAML, but skip plain Markdown documentation without frontmatter. Explicit Markdown paths are always accepted. `--list-rules` lists each rule's ID, default severity, supported ASTs, description, and known compiler threat ID (`CTR-*`).

**Migration from actionlint-only linting:** the default now also enforces registered AST rules and may fail on findings that actionlint did not report. Review those findings, or run `actionlint` directly on the generated `.lock.yml` files to retain actionlint-only behavior. `--rules` selects AST rules and skips actionlint; it does not provide an actionlint-only mode.

See [Workflow Linters](/gh-aw/reference/workflow-linters/) for the rule catalog and compiler threat IDs, severity policy, analysis boundaries, optional JavaScript configuration, and performance diagnostics.

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

Unlike `run --repo/-r`, `trial` has no `--repo/-r`: `--logical-repo/-l` sets the simulated `github.repository` context, while `--host-repo` selects where the trial actually runs. `--json/-j` is available for trial output.

`--timeout` sets the execution timeout in integer minutes (default: `30`; `0` disables the timeout). For example, use `--timeout 10`, not `--timeout 10m`.

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
gh aw list --stale                          # List workflows with stale or missing lock files
gh aw list --label automation               # Filter by label
gh aw list --dir custom/workflows           # List from a local custom directory
gh aw list --repo owner/repo --path .github/workflows  # List from a remote repository
```

**Options:** `--json/-j`, `--label`, `--stale`, `--dir/-d`, `--path`, `--repo/-r`

`--dir` (`-d`) overrides the local workflow directory and applies only when `--repo` is not set. `--path` specifies the workflow directory in a remote repository and is used with `--repo`.

`list` enumerates workflows locally without GitHub API queries. For enabled/disabled state and run information, use `status` instead.

For local workflows, `compiled` is `Yes` when the source frontmatter matches the generated `.lock.yml`, `No` when the hashes differ, and `N/A` when the lock file is missing. JSON output is a top-level array with one object per workflow and the same values in each object's `compiled` field. Use `--stale` to inspect only stale or missing lock files:

```bash
gh aw list --stale
```

For CI, fail when any lock file is stale or missing:

```bash
gh aw list --json | jq -e 'all(.[]; .compiled == "Yes")'
```

Run the check from the repository root. If it reports workflows, run `gh aw compile <workflow-id>` for each one and commit the regenerated `.lock.yml` files with their sources.

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

Episode grouping requires a nonempty `context.episode_id`; runs without it remain in `.runs[]` and overall totals but have no episode rollup or edges. There is no call-ID or metadata-based lineage inference. Episode totals cover selected canonical members only. `recorded_roots[]` preserves recorded root identities, while `representative_run_id` and `representative_workflow` name the selected-data representative. The episode-level `root_run_id` and `primary_workflow` fields are removed; migrate to the representative fields for selected-data identity or `recorded_roots[]` for canonical identity. `coverage` reports `scope: selected_runs`, `status: unknown` or `partial`, reason codes, missing recorded parent run IDs, and collection truncation independently of lineage confidence. No logs selection establishes complete episode coverage. Both compact and verbose JSON retain all canonical episodes. See [episode-level usage](/gh-aw/reference/cost-management/#interpret-episode-level-usage) for field semantics.

`--count` defaults to 10, including with `--start-date` or `--end-date`. A larger date range does not remove that cap. `.continuation` and `.continuations[]` provide continuation parameters when collection is truncated; their absence does not prove that an episode has no unselected ancestors or descendants.

> [!NOTE]
> A downstream warning that a workflow file returned HTTP 404 does not mean `gh aw logs` failed to download the run. The command downloads run artifacts by run ID; workflow source lookup is a separate operation performed by the consumer. GitHub's workflows API can retain entries for deleted or renamed files. Live-inventory consumers should ignore non-active workflows and paths absent from the repository contents listing. Historical consumers should use each JSON run's `repository`, `workflow_path`, and `sha` fields instead of the current default branch, and treat missing source as unavailable metadata rather than a failed log download.

Logs preserve each run's raw GitHub conclusion. `job_count: 0` means the Jobs API
confirmed no jobs for that attempt; an absent `job_count` means unavailable evidence.
Confirmed jobless failures remain visible as `confirmed_no_jobs` clusters, but
are not runtime-audit candidates. They can represent validation failures or
platform-side lifecycle invalidation; missing artifacts alone establishes neither.

An originally queued `pull_request` run is classified with
`exclusion_reason: obsolete_pr` only when its exact attempt has no jobs and the
uniquely associated PR closed after the attempt was queued but before GitHub
finalized it as failed or cancelled. `obsolete_pr` retains the PR number,
closure time, and attempt as evidence. Such runs remain in the raw run list and
`summary.obsolete_pr_runs`, but are excluded from actionable failure clusters
and investigations. Explicit startup-validation conclusions, observed failure
signals, job-backed failures, unavailable API evidence, and closed-event runs
are not classified as obsolete. Association is resolved from the run's exact
commit, even if that PR's head advanced before closure; ambiguous associations
remain unknown.

`summary.executed_runs` and `summary.executed_success_runs` count only completed
runs with confirmed jobs. Use these for executed-run success rates rather than
the raw `total_runs`; unknown evidence and jobless runs are not successes.
`summary.non_execution_runs` counts confirmed jobless runs. No in-job guard can
change GitHub's platform-side conclusion for a run whose jobs never started.

To diagnose an actual download failure, enable the logs API and download debug namespaces:

```bash wrap
DEBUG=cli:logs_github_api,cli:logs_download gh aw logs --verbose --json
```

`logs` accepts both workflow IDs (kebab-case filename without `.md`, for example `ci-failure-doctor`) and display names from frontmatter (for example `CI Failure Doctor`); matching is case-insensitive.

```bash wrap
gh aw logs ci-failure-doctor               # Workflow ID
gh aw logs "ci failure doctor"             # Display name (case-insensitive)
```

`--cache-before` deletes cached `run-{ID}` folders older than a cutoff before downloading. It accepts the same relative and absolute date formats as `--start-date` and `--end-date`; the deprecated hidden alias `--after` still works. The command reads the run creation time from `run_summary.json` when present, otherwise it falls back to the directory modification time.

```bash wrap
gh aw logs --cache-before -1w                        # Evict cache older than 1 week, then download as normal
gh aw logs --cache-before 2024-01-01                 # Evict cache entries before a specific date
gh aw logs my-workflow --cache-before -1mo -c 20     # Evict, then download 20 runs of one workflow
```

`--train` writes `drain3_weights.json` to the logs output directory using downloaded runs. Those weights improve anomaly detection in later `gh aw audit` and `gh aw logs` runs. To embed them as defaults, copy the file to `pkg/agentdrain/data/default_weights.json` and rebuild.

```bash wrap
gh aw logs --train                    # Train on last 10 runs
gh aw logs my-workflow --train -c 50  # Train on up to 50 runs of a specific workflow
```

`--stdin` reads run IDs or URLs from standard input, one per line, instead of discovering runs from the GitHub API. It cannot be combined with a workflow-name positional argument. Date, count, and workflow-name filters are ignored in this mode, but content filters such as `--engine`, `--firewall`, and `--safe-output` still apply. Blank lines and `#` comments are ignored. Bare numeric IDs require `--repo owner/repo`; full run URLs do not.

```bash wrap
echo "1234567890" | gh aw logs --stdin --engine claude
cat run-ids.txt | gh aw logs --stdin --repo owner/repo   # required for bare numeric IDs
gh aw logs --runtime cloud-hypervisor                    # Filter to runs using a specific sandbox agent runtime
```

**Options:** `--after-run-id`, `--artifacts`, `--audit`, `--before-run-id`, `--cache-before`, `--cached-jsonl`, `--count/-c`, `--drain3-weights`, `--dry-run`, `--end-date`, `--engine/-e`, `--evals`, `--exclude-staged`, `--filtered-integrity`, `--firewall`, `--format`, `--graders`, `--ignore-workflow-runs`, `--json/-j`, `--last`, `--max-github-api-rate-limit`, `--max-storage`, `--no-dry-run`, `--no-firewall`, `--output/-o`, `--parse`, `--prune-older-runs`, `--ref`, `--report-file`, `--repo/-r`, `--runtime`, `--safe-output`, `--start-date`, `--stdin`, `--summary-file`, `--timeout`, `--tool-graph`, `--train`

`logs` defaults `--artifacts` to `usage` for faster, compact downloads. When usage lacks `aw_info.json`, metadata is fetched from the compact `info` artifact before falling back to legacy activation artifacts. The `--last` flag is an alias for `--count/-c`.
`--dry-run` selects only dry-run runs; `--no-dry-run` excludes them. These flags are mutually exclusive and work with workflow targets or `--stdin`. Older valid metadata without `dry_run` counts as a normal run; missing or malformed `aw_info.json` is excluded when either filter is active. Unified sessions preserve the flag as `workflow.info.data.dryRun`, and audit engine configuration exposes `dry_run`.
When multiple targets run concurrently, `--count` limits the combined number of workflow runs and `--timeout` limits the total wall-clock download time across all targets. `--timeout` accepts integer minutes (for example, `--timeout 10`); the default `0` disables the timeout.

`--evals` and `--graders` each pull in a matching artifact set automatically: `--evals` includes results from `evals.jsonl`, `--graders` includes deterministic grader results and their artifacts. `--audit` generates `audit.json` in each downloaded run's cache directory (comparisons use only the runs downloaded in that invocation). `--ignore-workflow-runs` excludes specific run IDs (or `slug/ID` values, where the slug is informational and matching uses the numeric ID) from an otherwise matching set.

The `graders` artifact set includes conclusion's built-in results in `usage`
and the optional combined `graders` artifact from the separate custom-grader job,
with workflow-call-prefixed and historical agent artifacts supported.

`--max-storage` bounds total logs cache size in MB after pruning non-essential data. It defaults to 1024 MB (1 GB); `--max-storage 0` disables the limit. Concurrent in-flight downloads can temporarily exceed this soft budget. `--prune-older-runs` additionally removes the oldest completed runs when that pruning alone can't satisfy `--max-storage`. `--max-github-api-rate-limit` pauses (single target) or stops (multiple targets) further downloads once the given number of core API requests have been used — pass a positive value for an absolute budget or a negative value to reserve that many requests from the current rate limit. `--drain3-weights` points `--train` at an existing `drain3_weights.json` to seed pattern training instead of starting from scratch.

`--cached-jsonl` reuses compatible, schema-versioned run records and workflow-run discovery responses. It writes exactly one JSON value per line, appending every complete `gh run list` payload before downloading artifacts and available GitHub API rate-limit reports after collection. Each enriched `run` record includes job execution data, sanitized MCP tool-call metadata, and available engine, model, runtime, and component versions for downstream dashboards. Raw tool errors, arguments, responses, and artifact bodies are excluded. Discovered runs therefore remain available when a timeout or API limit interrupts processing. Records from incompatible schema versions are ignored. Use `gh aw json-schema logs-jsonl` to generate the schema for each JSON Lines item.

Each safe-output entity recorded for a run (the same provider-neutral records surfaced in `usage/activity/summary.json`) is also appended as its own `safe_output_item` line immediately after the `run` record, carrying a `run_id` correlating it back to the run plus the entity's type, provider, durable ID, human-readable identifier, URL, and target/state metadata. These per-entity lines are informational projections of the `run` record's `safe_outputs` array and are ignored by the in-memory run cache used to skip already-downloaded runs.

Pass a trailing wildcard prefix such as `--cached-jsonl 'logs-*'` to load all matching `logs-*.jsonl` files as the starting cache and write newly downloaded data to a unique `logs-<unix-time>-<random>.jsonl` file. With `--start-date` or `--end-date`, wildcard cache files containing exclusively dated run records outside the requested range are deleted.

##### `logs episode <run-id-or-url>`

Retrieve a canonical episode starting from a recorded run. The command visits
recorded parents and roots, then scans the allowed repositories for runs sharing
the seed's `context.episode_id`. It does not infer membership from workflow names,
revisions, or event types.

```bash wrap
gh aw logs episode 1234567890 --repo owner/repo \
  --allow-repo owner/downstream --start-date -2d \
  --max-runs 100 --max-api-requests 500 --timeout 5m --json > episode.json
```

| Option | Default | Bound |
|--------|---------|-------|
| `--repo/-r` | Repository in the seed URL | Required for numeric run IDs; accepts `[HOST/]owner/repo`. |
| `--allow-repo` | None | Additional `owner/repo` repositories on the seed host; repeatable. Recorded metadata cannot expand this allowlist. |
| `--start-date`, `--end-date` | Seven days ago, now | Candidate creation-time window, including explicitly recorded parents and roots. Accepts dates, RFC3339 timestamps, and relative dates. |
| `--max-runs` | `100` | Distinct candidates examined, including the seed, nonmembers, and failed lookups. |
| `--max-api-requests` | `500` | REST requests initiated by the collector, including metadata, discovery pages, artifact inventories, and archive downloads. Artifact-storage redirect transfers are excluded. |
| `--timeout` | `5m` | Collection deadline, expressed as a duration. |
| `--output/-o` | `.github/aw/logs` | Compact artifacts, isolated by host, repository, run, attempt, and immutable artifact ID. |
| `--json` | Disabled | Standard logs JSON plus `episode_retrieval` bounds, counters, stop reasons, and evidence gaps. |
| `--view` | `summary` | Display `summary` or recorded `hops`; JSON includes both views' data. |
| `--refresh` | Disabled | Recollect instead of reading the cached episode snapshot; immutable artifact downloads are still reused. |

Run, request, and timeout bounds must be positive. This subcommand downloads only
compact `info` and `usage` artifacts, with a 64 MiB limit on each archive and its
total extracted contents. Missing seed identity is an error; unavailable
related runs, conflicting identity, excluded repositories, expired artifacts, and
truncated discovery qualify the returned episode coverage. Coverage remains
`selected_runs` with status `unknown` or `partial`, never complete, even after all
pages in the bounded window are exhausted.

The command automatically caches a snapshot for the seed, host, repository
allowlist, dates, and collection bounds beneath `--output`. Repeating the same
query or switching `--view` reads that snapshot without GitHub requests, even
offline. Snapshots remain unchanged until `--refresh`; relative dates such as
`-7d` retain the original snapshot window until refreshed. Changing bounds
collects a separate snapshot. `episode_retrieval.collected_at` records collection
time and `cached` marks reuse; REST counters describe the original collection.

Use `--refresh` to rescan with the same bounds. A larger-bound query also reuses
downloaded immutable artifacts; discovery rescans rather than resuming an
unstable page cursor. GitHub's 1,000-result
limit on filtered run discovery is reported as a gap; narrow the time window
when reached. Unlike `logs --max-github-api-rate-limit`, the request cap applies
to this retrieval, not the account's previously consumed quota.

Each collection records its own artifact inventory and `logs_path`. Later
collections do not delete cached evidence referenced by earlier reports, and
completed local downloads remain usable after remote expiration.

The MCP `logs` tool exposes the same operation through `episode_run`,
`episode_repository`, `allow_repositories`, `max_api_requests`, and `refresh`; `count`
becomes the candidate cap. Workflow-discovery filters cannot be combined with
episode retrieval.

To retrieve an ordinary workflow named `episode`, use `gh aw logs -- episode`.

Use `--view hops` on the same episode command to inspect recorded invocations:

```bash wrap
gh aw logs episode 1234567890 --repo owner/repo --view hops
```

No separate explanation command or JSON input file is needed. The first query
collects and caches the episode; subsequent queries with the same collection
options reuse it. `--json` always returns the standard logs report, described by
`gh aw json-schema logs`, regardless of the selected display view.

The view shows recorded hop IDs, parent-hop and run edges, reusable-workflow
hops sharing one run ID, and missing or conflicting evidence. Costs appear
once per selected run in `run_costs`, not on hops. Missing hop IDs remain
missing; the view does not reconstruct them or claim complete membership.
Logs JSON includes the same data under `episodes[].explanation`, including in
compact and MCP reports.

#### `audit`

Analyze workflow runs with detailed reports. The `audit` command has two modes: a single-run audit (default) and a multi-run analysis.

##### `audit <run-id>`

Analyze a single run with a multi-section report. Accepts run IDs and workflow run, job, or step URLs. Auto-detects Copilot coding agent runs. Job URLs extract that job's logs; step URLs extract that step, otherwise the first failing step.

```bash wrap
gh aw audit 12345678                                      # By run ID
gh aw audit https://github.com/owner/repo/actions/runs/123 # By workflow run URL
gh aw audit https://github.com/owner/repo/actions/runs/123/job/456 # By job URL (extracts first failing step)
gh aw audit https://github.com/owner/repo/actions/runs/123/job/456#step:7:1 # By step URL (extracts specific step)
gh aw audit 12345678 --parse                              # Parse logs to markdown
gh aw audit 12345678 --repo owner/repo                    # Specify repository for bare run ID
```

**`--stdin` flag:** Reads run IDs or URLs from stdin (one per line) instead of positional arguments (mutually exclusive). Blank and `#`-prefixed lines are ignored. Bare numeric IDs require `--repo owner/repo`; full URLs carry their own repo context.

```bash wrap
echo -e "1234567890\n9876543210" | gh aw audit --stdin   # diff mode: first is base
cat run-ids.txt | gh aw audit --stdin --repo owner/repo
gh aw audit 1234567890 --runtime cloud-hypervisor        # Skip run unless sandbox agent runtime matches
```

**Options:** `--artifacts`, `--evals`, `--experiment`, `--format`, `--group`, `--json/-j`, `--no-baseline`, `--output/-o`, `--parse`, `--repo/-r`, `--runtime`, `--search`, `--search-limit`, `--stdin`, `--timeout`, `--variant`

`--search "query"` retrieves evidence from the run's redacted unified session using local BM25 ranking, without changing audit findings. `--search-limit` bounds returned evidence groups (default: `5`, range: `1–20`). Search requires a single run and cannot be combined with `--group` or job/step URLs. See [Searching session evidence](/gh-aw/reference/audit/#searching-session-evidence) for input limits and automatic repetition grouping.

The `--timeout` flag sets a duration such as `50s` or `5m` (default: `0`, no additional deadline). Timed-out single-run JSON audits exit unsuccessfully and return available metadata or collected analysis with `partial: true`, `complete: false`, and the interrupted `stage`. Incomplete reports are not persisted as complete audit caches.

The `--repo` flag accepts `owner/repo` and is required for a bare numeric run ID.

The `--artifacts` flag selects which artifact sets to download (default: `all`, unlike `gh aw logs`, which defaults to `usage`). Valid sets include `activation`, `agent`, `all`, `detection`, `evals`, `experiment`, `firewall`, `github-api`, `graders`, `info`, `mcp`, `usage`, and `work-queue`. The `work-queue` set downloads queue snapshots, worker finish intents, and workflow logs for coordinator operation reporting. The `--experiment` flag filters to runs that include the named experiment; `--variant` restricts to a variant value and requires `--experiment`. The `--output/-o` flag overrides the output directory.

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

Compare two or more runs to detect policy regressions, new unauthorized domains, and behavioral drift. Pass multiple run IDs directly to `audit` — the first is the base, the rest are comparisons:

```bash wrap
gh aw audit 12345 12346                     # Compare two runs
gh aw audit 12345 12346 12347 12348         # Compare base against 3 runs
gh aw audit 12345 12346 --format markdown   # Markdown for PR comments (or --json for CI)
```

The diff shows new or removed network domains, status changes (allowed ↔ denied), volume changes (>100%), MCP tool invocation changes, run metrics (tokens, duration, turns, tokens per turn), and per-tool and per-bash-command call breakdowns.

**Options:** `--artifacts`, `--format` (pretty, markdown; default: pretty), `--json/-j`, `--output/-o`, `--repo/-r`

#### `sessions`

Experimental: download a unified agent session for an existing workflow run. The command writes
`usage/aw_session.jsonl` to standard output without changing its contents. If that
file is absent, it reconstructs the session from the agent artifact using the same
engine parsers as `audit`, including available MCP and firewall evidence.
Reconstruction does not fetch missing downstream artifacts; the collection record
identifies absent components and source warnings.
Runs from older `gh-aw` versions are parsed into the current unified format from native Copilot events, Pi streams, raw engine logs, legacy `agent-artifacts`, or `agent-stdio-log`/`agent-stdio.log` artifacts, along with `aw-info`/`aw_info` or activation metadata.

```bash wrap
gh aw sessions download 1234567890 --repo owner/repo
gh aw sessions download https://github.com/owner/repo/actions/runs/1234567890 --format markdown
gh aw sessions download 1234567890 -o session.jsonl
```

**Options:** `--format` (`jsonl` or `markdown`; default: `jsonl`),
`--output/-o` (output file), `--repo/-r` (`[HOST/]owner/repo`; defaults to the
current repository).

JSONL preserves the complete unified session payload. Markdown uses the shared,
bounded summary renderer, omits user prompts and private tool payloads, and
reports display truncation. Node.js is required for reconstruction and Markdown
rendering, but not for downloading an existing unified JSONL file. Missing
artifacts, download failures, and invalid session files return errors.
The command and its output formats may change.

#### `graders`

Run one enabled built-in, custom JavaScript, or operational-value Bash grader against a unified session from a saved run's
`usage` artifact or from standard input. Input accepts unified-session JSONL
or a JSON event array. The ten fast built-ins are available without explicit
frontmatter configuration. Custom JavaScript uses a separate restricted worker
with the same in-memory interface as the downstream graders job. Repository-owned
`graders.operational-value.run` Bash uses that job's session-file compatibility
runtime: session JSONL in `$1` / `GH_AW_SESSION_FILE`, config JSON in
`GH_AW_GRADER_CONFIG`, and legacy metric-array output.

Pure built-in/JavaScript replay requires Node.js 20+ with the permission model. The CLI picks
`--permission` or `--experimental-permission` as supported, restricts reads to
the staged runtime modules, and passes the session in memory to the grader.
Bash replay requires Node.js and Bash with the same bounded, stripped-environment
runtime as the separate job. It is trusted code, not a filesystem/network sandbox
or microVM; compilation and lint reject detected file writes.

```bash wrap
gh aw graders run weekly-research loops 123456789
cat aw_session.jsonl | gh aw graders run weekly-research loops
cat aw_session.jsonl | gh aw graders run weekly-research operational-value
```

**Options:** `--repo/-r`

#### `logs --outcomes`

Check what happened to a workflow run's safe outputs (accepted, rejected, ignored, or pending).

```bash wrap
gh aw logs weekly-research --outcomes --json
gh aw logs weekly-research --episodes --outcomes --json
printf '%s\n' 1234567890 | gh aw logs --stdin --repo owner/repo --outcomes --json
gh aw logs episode 1234567890 --repo owner/repo --outcomes
gh aw logs --outcomes --outcomes-dir ./otlp
```

**Options:** `--json/-j`, `--repo/-r`, `--output/-o`, `--episodes`, `--outcomes-dir`

The flag applies to selected runs in discovery, multi-workflow, stdin, and
episode collection. JSON includes `.outcomes[]` with per-run observations and
`.episode_outcomes` with selected-episode summaries. Outcome checks are live
and require additional API requests even when execution artifacts are cached.
`--outcomes-dir` requires `--outcomes` and writes JSONL observations.
The standalone `gh aw outcomes` command has been removed.

`--episodes` adds recorded episode membership and hops to text reports. Combine
`--episodes --outcomes` to perform the episode-outcome join in the same
invocation, using collected membership, live observations, and downloaded
usage. No separate command or JSONL export is needed.
Standard logs JSON retains episode membership and outcome summaries with or
without the explicit `--episodes` flag. Membership is limited to selected runs; this flag
does not expand discovery to all episode members.

Reports are scoped to selected runs and qualify missing membership, outcomes,
and cost. See [Episode Outcome Summaries](/gh-aw/reference/outcomes/#episode-outcome-summaries)
for the JSON contract and unavailable-cost semantics.

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
gh aw health --start-date -24h      # Evidence from the last 24 hours
gh aw health --timeout 30s --json   # Return available evidence within a 30-second collection budget
gh aw health --start-date 2026-10-14T04:49:11Z  # Evidence since a release timestamp
gh aw health --start-date 2026-10-14 --end-date 2026-10-15  # Bounded window
gh aw health issue-monster --days 90  # 90-day metrics for workflow
```

**Options:** `--days`, `--start-date`, `--end-date`, `--logs-dir`, `--timeout`, `--threshold`, `--repo/-r`, `--json/-j`

The `--days` flag accepts 7, 30, or 90 (default: 7). Other values produce an error.

`--timeout` bounds evidence collection (default: `2m`; `0` disables the deadline). The shared deadline cancels API quota checks, requests, local Git revision reads, and work-queue ledger reads. Expiry skips further cache reads and returns available evidence with `collection.partial: true`, `reason: "time_budget"`, and a warning, rather than discarding the report. JSON includes `timeout_seconds` when bounded and `elapsed_seconds`, measured after cache enrichment. An in-progress local cache read and report rendering may finish after the deadline. Partial results do not establish fleet health; increase the budget or narrow the workflow/time window to collect more evidence. `DEBUG=cli:health` logs request progress and elapsed times to stderr without logging response bodies.

`--start-date` and `--end-date` filter run creation times inclusively. Both accept `YYYY-MM-DD` dates, RFC3339 timestamps with time zones, and relative times such as `-24h`, `-1d`, or `-1w`. Dates use UTC; an end date includes that entire day. The end defaults to the current time. Without an explicit start, `--days` counts backward from the end. An explicit start replaces the default seven-day horizon and cannot be combined with an explicit `--days`.

For release-scoped evidence, pass the release's publication timestamp as `--start-date`; this filters by time, not compiler version. Text output and JSON `collection.start_date` / `collection.end_date` show the resolved UTC window. Health still samples current workflow revisions toward five completed first-attempt runs per scope, subject to API limits; it does not enumerate every run or reconstruct historical branch heads. A narrow window can leave insufficient evidence.

Runs with `skipped` or `action_required` conclusions are excluded from health metrics, revision evidence, and failure clusters. Waiting and failed runs without jobs are also excluded when the Jobs API confirms their run attempt has no jobs, including runs whose approval expired. Job-backed waiting runs remain pending revision evidence. Successful job-presence lookups are reused per run ID and attempt within the collection, including across overlapping search windows. Collection continues past ignored runs to find executed runs within the requested window and API limits. An unavailable or malformed Jobs API response is reported as an error, not treated as evidence that no jobs ran.

For confirmed jobless failures, health checks the attempt-specific GitHub run page for the annotation `This workflow run required approval but was not approved before it expired.` Confirmed approval expiries are also removed from `failure_candidates` and `potential_issues`, without counting as successes. JSON `collection.approval_expiry_evidence` and text `[health_approval_expiry]` entries retain the run URL, attempt, classification, and reason; JSON also retains the matched message and update time. Missing annotations, inaccessible pages, or unrecognized messages never establish approval expiry. These optional page requests use the configured host and repository, share the collection's request budget, and are capped at 20 per collection (`approval_requests`). Results are reused only for the same run attempt and update time. Other jobless failures remain approximate potential issues.

PR-review cancellations are also excluded when an unambiguous PR associated with the run's exact head commit and branch was merged, closed, or had its head deleted during that attempt. Complete Jobs API evidence must show no genuine failed jobs or steps. A closed PR today or a missing branch today does not explain an earlier cancellation. Open PRs, unrelated cancellations, and genuine job failures remain failure evidence.

For runs marked `failure`, health requires the failed agent step to be `Checkout PR branch`, the exact `PR #... closed before its head could be fetched. Stopping rather than running on the previous checkout.` job-log diagnostic for that PR, and a matching closure/merge timestamp no later than the failed checkout. Only a subsequent conclusion job's `Handle agent failure` step is accepted as downstream aftermath; other failures are retained.

Ignored runs do not count as successes or consume the minimum sample. They are excluded from rates, durations, token averages, revision/recovery evidence, failure candidates, clusters, and investigation targets. JSON `collection.pr_lifecycle_evidence` and text `[health_pr_lifecycle]` entries report `ignored`, `retained`, or `unknown` classifications and reasons. Missing, ambiguous, incomplete, or unavailable evidence leaves a run unfiltered by this check. Lifecycle API responses (including errors and targeted checkout-job logs) are cached within the collection, use the configured repository and host, and share its API budget. `collection.lifecycle_requests` is capped at 40 additional requests; branch-event listings are limited to two pages. Health downloads no artifacts.

Filtered runs do not consume the minimum sample. Health searches backward for five completed first-attempt runs per current-revision scope, stopping at the requested time boundary or API budget rather than a fixed page count. It narrows the search window to continue beyond GitHub's 1,000-run search limit, overlapping boundary timestamps and deduplicating runs. If timestamp ties prevent further progress, the report explicitly marks collection as partial.

Collection prioritizes early detection over exhaustive verification. It inventories registered AW `failure` conclusions using run-list payloads alone, then checks jobs and revisions for one job-backed representative per branch/event. The inventory retains at most five candidates per workflow and stops when all registered workflows reach that count or after ten pages. Confirmed approval expiries free candidate slots: backfill first reuses overflow retained from fetched pages, then requests older pages if needed, within the same ten-page listing and shared request budgets. Confirmed expiries therefore do not consume the five-retained-candidate limit; the 20-request annotation cap still applies. Hitting the page limit marks collection partial with `failure_listing_limit`; chronological sampling continues. A named-workflow check scopes that inventory to the selected workflow rather than scanning unrelated repository runs. The normal chronological sample adds successful, pending, and other failed conclusions, visiting recently failing workflows first and deduplicating runs across passes. `collection.failure_listing_complete` covers the bounded inventory; `failure_pass_complete` covers representative verification.

`collection.failure_candidates`, `failure_candidate_workflows`, and `potential_issues` retain approximate signals even when verification exhausts the budget. Potential issues include a representative run URL and `run_list_only` evidence; these counts may include jobless, historical, or superseded failures. `representative_status` distinguishes unverified, jobless, superseded, revision-unknown, and revision-checked representatives. These signals do not establish a current issue, recovery, or a fleet failure rate. Confirmed current-revision clusters remain separate.

Job-presence checks inspect only the jobs array, without decoding job/step fields. Current failure-first representatives separately fetch typed jobs in at most three 100-job pages per run attempt, without downloading artifacts. Only complete live inventories contribute job/step failure signals; incomplete runs remain metadata-only unless compatible cached evidence is available. Shared failed steps cluster across workflow names; conclusion-job aftermath remains separate from the original failure boundary. `collection.job_evidence` reports each representative's source, fetched job count, attempt, update time, and completeness. Failed or capped optional enrichment marks collection partial with a warning and preserves collected runs while sampling continues; primary inventory and presence errors remain errors, and quota or request-budget limits still stop collection. `job_requests` counts actual Jobs API requests within `api_requests`.

JSON `collection.producer_version`, `report_schema_version`, `capabilities`, and `observed_at` identify the producer and supported evidence. The creation-time bounds do not make the API responses an atomic snapshot. Investigation representatives include branch, event, head SHA, attempt, and update time when known. `status_counts` partitions all workflow entries, including intentional-failure workflows. `healthy_workflows` counts non-intentional workflows with `healthy` or `recovery_observed` status that meet the threshold and have no observed sub-agent failures; an above-threshold rate alone does not establish health. `observed_failure_workflows` counts non-intentional workflows with sampled current-revision Actions failures, including insufficient samples. `degraded_workflows` remains the sub-agent-only count, not a count of all failing workflows.

For events whose head SHA identifies the executing workflow, health also compares the workflow's Markdown source at that SHA with the pushed source on the run's branch. Source blob SHAs are included as `revisions[].current.source_sha` and `previous.source_sha`. A source change stops older evidence collection for that branch/event and excludes superseded failures from current failure clusters and investigation targets, even if the lock file is unchanged. A pushed source with no matching runs has `no_current_revision_runs`, not a healthy or fixed status. Unrelated repository commits do not stop collection. Unavailable source revisions produce a warning and unknown revision evidence rather than proof of recovery.

Revision reads prefer committed Git objects in the current checkout when its origin matches the target repository and host. Dirty files, local-only branch tips, and stale remote-tracking refs are not source evidence. The pushed branch head is resolved once, then local objects are reused; missing history or blobs fall back to GitHub without fetching or deepening the checkout. Fleet fallback shares workflow-directory snapshots across workflows at each ref and lock metadata across identical blob SHAs. `collection.local_revision_reads` reports successful local reads; `revision_lookups` counts remote file and directory requests. Workflow-registry timestamps are not used as source-change evidence. Actions run discovery remains REST-based because the GraphQL repository schema does not expose a workflow-run connection.

Shows success/failure rates, trend indicators (↑ improving, → stable, ↓ degrading), execution duration, token usage, costs, and warnings when success rate drops below threshold.

When a selected workflow's published default-branch lock file enables work-queue, health also reads and validates the repository's current `work-queue` ledger once. This includes workflows without recent runs. Text `[health_work_queue]` and JSON `collection.work_queue` report backlog, eligible and blocked work by pool, oldest available work, pauses, native reservations, and pending, verified, or failed delivery. Pauses and failed deliveries produce `attention_required`; an otherwise readable snapshot is `observed`, not proof of live worker health. Detection warnings do not change the status of a successfully read ledger. Missing or invalid definitions, policy, ledger, or credentials remain explicit, privacy-safe warnings rather than healthy results. Queue diagnostics are current ledger evidence independent of `--days` or date bounds, do not download artifacts, and do not mutate the queue or launch workers. Optional workflow detection runs after primary Actions collection, uses its remaining API budget, and reuses downloaded lock content. The ledger uses the authenticated, isolated Git transport without fetching into the checkout, and validates resource references against the selected GitHub host. Actions rates and fleet workflow counts remain unchanged.

Health also reads matching `run_summary.json` files from `--logs-dir` (default: `.github/aw/logs`) for failed sub-agent invocations, including workflows whose Actions conclusion is `success`. An affected current revision has status `subagent_failures_observed` and is excluded from `healthy_workflows`; its Actions success/failure counts and success rate remain unchanged. JSON reports include `subagent_failure_count` (invocations), `subagent_failure_runs` (distinct runs), and per-workflow `subagent_failures` with run URLs, agent names, and requested models. Fleet JSON also includes `degraded_workflows`.

This check does not download artifacts. Populate the cache with `gh aw logs --artifacts usage` or `gh aw audit <run-id>` before checking health. `collection.cache_evidence` reports per-run `compatible`, `missing`, `unreadable`, `invalid`, `stale`, or `version_mismatch_raw_jobs_only` status, with the cached producer version when readable. A producer-version mismatch permits reuse of raw Jobs API observations only, after checking run identity, repository URL, attempt, head SHA, workflow, status, conclusion, and update time. Derived usage, classification, and sub-agent metrics still require the current CLI version; cached producer metadata is never rewritten. Missing or rejected derived evidence does not establish failure-free delegation. Historical-revision failures do not degrade the current revision.

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

`--timeout` gracefully stops computation after the specified number of integer minutes (for example, `--timeout 10`). The default `0` disables the timeout.

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

Container digest pins are refreshed on a best-effort basis using Docker Buildx or `crane`. If a lookup fails, `update` prints one short informational message with the reason and preserves existing pins. Set `DEBUG=cli:update_container_pins` for per-image diagnostics.

If no workflows in the repository contain a `source` field, the command exits gracefully with an informational message rather than an error. This is expected behavior for repositories that have not yet added updatable workflows.

```bash wrap
gh aw update                              # Update all with source field
gh aw update ci-doctor                    # Update specific workflow (3-way merge)
gh aw update ci-doctor --no-merge         # Override local changes with upstream
gh aw update ci-doctor --major --force    # Allow major version updates
gh aw update --no-release-bump            # Update workflows; only force-update core actions/*
gh aw update --repo owner/repo            # Update workflows in another repository
gh aw update --create-pull-request        # Update and open a pull request
gh aw update --pr --auto-merge             # Update, open a PR, and enable auto-merge
gh aw update --org my-org --pr --auto-merge --yes  # Enable auto-merge per repository
gh aw update --org my-org --create-issue --yes  # Auto-accept per-repo confirmations (required in CI)
```

**Options:** `--dir/-d`, `--no-merge`, `--major`, `--force/-f`, `--engine/-e`, `--no-stop-after`, `--stop-after`, `--no-release-bump`, `--no-security-scanner`, `--approve`, `--create-pull-request` / `--pr`, `--auto-merge` (requires PR creation; see [auto-merge requirements](#pull-request-auto-merge)), `--create-issue`, `--org`, `--repos`, `--yes/-y`, `--no-compile`, `--no-redirect`, `--cool-down`, `--repo/-r`

Org mode (`--org`) previews or creates workflow update pull requests across every repository in an organization. Use `--repos` to limit matches, `--create-issue` to open an issue in each repository with pending updates, and `--yes/-y` to auto-accept per-repository confirmations in CI.

`--no-redirect` makes `update` fail when the source workflow has a [`redirect`](/gh-aw/reference/frontmatter/) field instead of following it. `--repo/-r` runs the update against a different repository by checking out an isolated shallow clone under `.github/aw/updates/<sanitized-repo-id>`; with `--create-pull-request`, the PR targets that repository instead of the current one.

#### `deploy`

Roll out one or more workflows to a target repository through a pull request. The command clones the target repository into an isolated shallow checkout, refreshes existing sourced workflows, adds the requested workflows, recompiles lock files with purge enabled, and opens a pull request against the target repository.

```bash wrap
gh aw deploy githubnext/agentics/ci-doctor --repo owner/repo
gh aw deploy githubnext/agentics/repo-assist githubnext/agentics/ci-doctor --repo owner/repo --force
gh aw deploy ./my-workflow.md --repo owner/repo
gh aw deploy ./my-workflow.md --repo owner/repo --pr --auto-merge  # Enable auto-merge on the deployment PR
gh aw deploy githubnext/agentics/ci-doctor --org my-org --repos '*-service' --yes  # Deploy across an org
gh aw deploy ./my-workflow.md --org my-org --pr --auto-merge --yes  # Enable auto-merge per repository
```

**Options:** `--repo/-r` (required unless `--org`), `--name/-n`, `--engine/-e`, `--force/-f`, `--append`, `--no-gitattributes`, `--dir/-d`, `--no-stop-after`, `--stop-after`, `--no-security-scanner`, `--cool-down`, `--org`, `--repos`, `--yes/-y`, `--create-pull-request` / `--pr` (compatibility flags; PR creation is always enabled), `--auto-merge` (requires explicit PR flag; see [auto-merge requirements](#pull-request-auto-merge))

Org mode (`--org`) deploys workflows across every repository in an organization instead of a single `--repo` target. Use `--repos` to limit org mode to repositories matching one or more glob patterns, and `--yes/-y` to auto-accept org-mode deploy confirmations (required in CI).

The `--repo` flag accepts `owner/repo` form and is required unless `--org` is provided. The target repository is checked out under `.github/aw/updates/<sanitized-repo-id>` inside the current working tree, so the command must be run from inside a git repository. Workflows already present in the target with a `source` frontmatter field are refreshed through the update phase and skipped by the add phase to avoid duplicate-add errors. The pull request commit title is `chore: deploy agentic workflows`. The default `--cool-down` value is `7d`.

#### `upgrade`

Upgrade repository with latest agent files and apply codemods to all workflows.

```bash wrap
gh aw upgrade                              # Upgrade repository agent files and all workflows
gh aw upgrade --no-fix                     # Update agent files only (skip codemods, actions, and compilation)
gh aw upgrade --create-pull-request        # Upgrade and open a pull request
gh aw upgrade --pr --auto-merge             # Upgrade, open a PR, and enable auto-merge
gh aw upgrade --org my-org --pr --auto-merge --yes  # Enable auto-merge per repository
gh aw upgrade --engine claude              # Override AI engine for compilation
gh aw upgrade --repo owner/repo            # Upgrade workflows in another repository
gh aw upgrade --audit                      # Run dependency health audit
gh aw upgrade --audit --json               # Dependency audit in JSON format
gh aw upgrade --org my-org --create-issue --yes  # Auto-accept per-repo confirmations (required in CI)
```

**Options:** `--dir/-d`, `--engine/-e`, `--repo/-r`, `--no-fix`, `--no-actions`, `--no-compile`, `--no-agent`, `--no-mcp`, `--no-skill`, `--disable-codemod`, `--create-pull-request` / `--pr`, `--auto-merge` (requires PR creation; see [auto-merge requirements](#pull-request-auto-merge)), `--create-issue`, `--org`, `--repos`, `--yes/-y`, `--audit`, `--json/-j`, `--approve`, `--pre-releases`

The dispatcher skill is updated by default. The Copilot custom agent and setup-steps workflow are created only when a workflow uses Copilot (including when the engine is omitted), or updated if the respective file already exists. An explicit `--engine` overrides workflow engine detection. Use `--no-agent`, `--no-mcp`, or `--no-skill` to skip the corresponding files, even when they already exist.

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

**Options:** `--scope` (required), `--repo/-r`, `--org`, `--enterprise`, `--visibility`, `--yes/-y`, `--dry-run`

The `--visibility` flag sets access for newly created org or ent variables (`all`, `private`, or `selected`; default: `all`). Existing variables keep their current visibility. The flag is rejected with `--scope repo`.

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

**`mcp list-tools` options:** `--server` (required)

**`mcp inspect` options:** `--check-secrets`, `--inspector`, `--server`, `--tool`

**`mcp add` options:** `--transport`, `--registry`, `--tool-id`

See [MCPs Guide](/gh-aw/guides/mcps/).

#### `pr transfer`

Transfer pull request to another repository, preserving changes, title, and description.

```bash wrap
gh aw pr transfer <pr-url> --repo target-owner/target-repo
gh aw pr transfer <pr-url> --repo target-owner/target-repo --pr --auto-merge
```

**Options:** `--repo/-r`, `--create-pull-request` / `--pr` (compatibility flags; PR creation is always enabled), `--auto-merge` (requires explicit PR flag; see [auto-merge requirements](#pull-request-auto-merge))

#### `mcp-server`

Run MCP server exposing gh-aw commands as tools. Spawns subprocesses to isolate GitHub tokens.

```bash wrap
gh aw mcp-server                      # stdio transport
gh aw mcp-server --port 8080          # HTTP server with SSE
gh aw mcp-server --validate-actor     # Enable actor validation
```

**Options:** `--port/-p` (HTTP server port), `--cmd` (custom subprocess command), `--allow-write-tools` (enable workflow-writing tools for trusted clients), `--validate-actor` (enforce actor validation for logs, audit, audit-diff, and enabled workflow-writing tools)

**Available Tools:** status, compile, logs, audit, checks, work-queue (read-only), mcp-inspect, add, update, fix

The `compile` tool uses a dedicated read-only mode with `--no-emit` by default; remote imports and nested body-level includes must already be cached, and container validation never pulls Docker images. Ordinary CLI `compile --no-emit` and `validate` retain import-cache population. Workflow-writing tools (`add`, `update`, and `fix`), emitted compilation, dry-run compilation, and external scanners require `--allow-write-tools`. Enable writes only for trusted MCP clients and workflow sources.

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

When no workflow is specified, lists all workflows with a summary of allowed and blocked domain counts. When a workflow is specified, lists all effective allowed and blocked domains including domains expanded from named domain sets (e.g., `node`, `python`, `github`, `copilot`) and engine defaults.

### Utility Commands

#### `work-queue`

This command is experimental. Inspect and publish the fair Git-backed DAG work queue. The alias `work` accepts the same subcommands and flags. Direct claims and operator finishes are not supported; `dispatch-next` grants scheduled work.

```bash wrap
gh aw work-queue state --repo owner/repo --json
gh aw work-queue explain --repo owner/repo --work-id my-work
gh aw work-queue stats --repo owner/repo
```

**Persistent options:** `--branch` (queue branch, default `work-queue`), `--json/-j`, `--repo/-r` (explicit `owner/repo` for repository operations; no host prefix or current-repository default), `--request-id` (publication handle or committed request selector), `--storage` (`git` only).

**Subcommands:** `cancel-claim`, `cancel-work`, `clear`, `compact`, `control`, `deploy`, `dispatch`, `dispatch-next`, `evidence`, `explain`, `inspect`, `policy`, `reconcile`, `replay`, `reprioritize`, `state`, `stats`, `submit-work`, `submit-graph`, `sync-issues`, `trace`, `tui`. Use `gh aw work-queue <subcommand> --help` for all subcommand-specific options.

| Subcommand | Options and behavior |
| --- | --- |
| `sync-issues` | `--request-id ID` selects one committed terminal request for repair of eligible existing Issue mirrors. See [Synchronization and recovery](/gh-aw/reference/work-queue/#synchronization-and-recovery) for projection and authorization rules. |
| `submit-work` | `--graph-id` sets a nonempty graph namespace (default: canonical payload SHA-256); `--node-key` sets a nonempty node identity within the graph (default: `root`). `--fairness-key` selects an entitled accounting key (default: one shared bucket). |
| `dispatch-next` | `--max-bytes` limits assignment bytes (default: 49,152), subject to policy and host limits. Grants and reserves assignments without launching workflows. |
| `dispatch <dispatcher>` | Starts a verified dispatcher at the repository's default-branch snapshot revision. `--dry-run` previews the route without granting Claims or launching workflows; `--wait` waits for the dispatcher, not verification of worker Results. |
| `control` | `--name credential_generation` requires `--generation`, an opaque identifier for an equivalent-scope credential generation, never a secret. Pause controls (`admission_paused`, `grants_paused`) require explicit `--paused=true` or `--paused=false`. All controls require `--reason`. |
| `reconcile` | `--cancel-reserved` requires `--dispatch-id` and cancels only when compare-and-set proves no launch-start marker exists. It cannot force-release a possible native run and cannot be combined with `--work-id`. |
| `clear` | Requires maintain or admin permission and `--reason CODE`. `--dry-run` validates and previews without publishing; `--hard` resets a paused, fully settled ledger. All modes, including previews, refuse CI. Mutations require interactive confirmation. |

Normal `clear` cancels unfinished snapshot Work without pending deliveries and preserves completed Work, history, fairness debt, and native reservations. It neither stops native runs nor pauses admission or grants. `--hard` requires admission and grants already paused, all reservations released, and all effects settled; it forgets active identities and fairness debt while retaining Git ancestry, so old identities can be submitted again and repeat effects. See [clearing unfinished Work](/gh-aw/reference/work-queue/#clear-unfinished-work) for limits and operator procedures.

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

#### `json-schema`

Print the JSON Schema for structured command output.

```bash wrap
gh aw json-schema audit        # Schema for gh aw audit --json
gh aw json-schema logs         # Schema for gh aw logs --json
gh aw json-schema logs-jsonl   # Schema for each gh aw logs --cached-jsonl line
```

**Schema arguments:** `audit`, `logs`, `logs-jsonl`. The schema is written to stdout. See [JSON output schemas](/gh-aw/reference/audit/#json-output-schemas).

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

Commands that support `--create-pull-request`, including `gh aw add`, `gh aw init`, `gh aw update`, and `gh aw upgrade`, auto-detect the enterprise host from the git remote and route PR creation to the correct GHES instance. `gh aw audit` and `gh aw add-wizard` do the same, so running them inside a GHES repository usually does not require setting `GH_HOST` manually.

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

Enable detailed debugging with namespace, message, and time diffs:

```bash wrap
DEBUG=* gh aw compile                # All logs
DEBUG=cli:* gh aw compile            # CLI only
DEBUG=*,-tests gh aw compile         # All except tests
```

Use `--verbose` for user-facing detail.

## Smart Features

### Fuzzy Workflow Name Matching

Typos in workflow names trigger suggestions based on Levenshtein distance.

```bash wrap
gh aw compile audti-workflows
# ✗ workflow file not found
# Did you mean: audit-workflows?
```

Supported in `compile`, `enable`, `disable`, `logs`, and `mcp` commands.

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

- [Quick Start](/gh-aw/setup/quick-start/) for your first workflow
- [Frontmatter](/gh-aw/reference/frontmatter/) for configuration options
- [Adding Existing Workflows](/gh-aw/setup/working-with-workflows/#adding-existing-workflows) for installing workflows from other repositories
- [Security Guide](/gh-aw/introduction/architecture/) for security best practices
- [MCP Server Guide](/gh-aw/reference/gh-aw-as-mcp-server/) for MCP server configuration
- [Agent Factory](/gh-aw/agent-factory-status/) for agent factory status
