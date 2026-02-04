---
title: CLI Commands
description: Complete guide to all available CLI commands for managing agentic workflows with the GitHub CLI extension, including installation, compilation, and execution.
sidebar:
  order: 200
---

The `gh aw` CLI extension enables developers to create, manage, and execute AI-powered workflows directly from the command line. It transforms natural language markdown files into GitHub Actions.

## 🚀 Most Common Commands

Most users only need these 6 commands:

- **`gh aw init`** - Set up your repository for agentic workflows  
  [→ Documentation](#init)

- **`gh aw add (workflow)`** - Add workflows from other repositories  
  [→ Documentation](#add)

- **`gh aw compile`** - Convert markdown to GitHub Actions YAML after editing  
  [→ Documentation](#compile)

- **`gh aw list`** - Quick listing of all workflows without status checks  
  [→ Documentation](#list)

- **`gh aw run (workflow)`** - Execute workflows immediately in GitHub Actions  
  [→ Documentation](#run)

- **`gh aw status`** - Check current state of all workflows  
  [→ Documentation](#status)

## Installation

Install the GitHub CLI extension:

```bash wrap
gh extension install github/gh-aw
```

### Pinning to a Specific Version

Pin to specific versions for production environments, team consistency, or avoiding breaking changes:

```bash wrap
gh extension install github/gh-aw@v0.1.0          # Pin to release tag
gh extension install github/gh-aw@abc123def456    # Pin to commit SHA
gh aw version                                         # Check current version

# Upgrade pinned version
gh extension remove gh-aw
gh extension install github/gh-aw@v0.2.0
```

### Alternative: Standalone Installer

Use the standalone installer if extension installation fails (common in Codespaces or with auth issues):

```bash wrap
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash                # Latest
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash -s v0.1.0      # Pinned
```

Installs to `~/.local/share/gh/extensions/gh-aw/gh-aw` and works with all `gh aw` commands. Supports Linux, macOS, FreeBSD, and Windows.

### GitHub Actions Setup Action

Install the CLI in GitHub Actions workflows using the `setup-cli` action:

``````yaml wrap
- name: Install gh-aw CLI
  uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v0.37.18
``````

The action automatically installs the specified version with checksum verification and platform detection. Useful for CI/CD pipelines, workflow testing, and automation scripts.

**Features:**
- Version validation against GitHub releases
- SHA256 checksum verification for security
- Automatic platform and architecture detection
- Fallback installation methods for reliability
- Works on Linux, macOS, Windows, and FreeBSD

**Example with verification:**

``````yaml wrap
- name: Install gh-aw CLI
  id: install
  uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v0.37.18

- name: Verify installation
  run: |
    gh aw version
    echo "Installed: ${{ steps.install.outputs.installed-version }}"
``````

See the [setup-cli action README](https://github.com/github/gh-aw/blob/main/actions/setup-cli/README.md) for complete documentation and examples.

### GitHub Enterprise Server Support

Configure for GitHub Enterprise Server deployments:

```bash wrap
export GH_HOST="github.enterprise.com"                           # Set hostname
gh auth login --hostname github.enterprise.com                   # Authenticate
gh aw logs workflow --repo github.enterprise.com/owner/repo      # Use with commands
```

## Global Options

| Flag | Description |
|------|-------------|
| `-h`, `--help` | Show help (`gh aw help [command]` for command-specific help) |
| `-v`, `--verbose` | Enable verbose output with debugging details |

## Commands

Commands are organized by workflow lifecycle: creating, building, testing, monitoring, and managing workflows.

### Getting Workflows

#### `init`

Initialize repository for agentic workflows. Configures `.gitattributes`, Copilot instructions, prompt files, and logs `.gitignore`. Enables MCP server integration by default (use `--no-mcp` to skip).

When run without arguments, enters interactive mode to guide you through engine selection (Copilot, Claude, or Codex) and secret configuration.

```bash wrap
gh aw init                              # Interactive mode: select engine and configure secrets
gh aw init --engine copilot             # Non-interactive with specific engine
gh aw init --no-mcp                     # Skip MCP server integration
gh aw init --tokens --engine copilot    # Check Copilot token configuration
gh aw init --codespaces                 # Configure devcontainer for current repo
gh aw init --codespaces repo1,repo2     # Configure devcontainer for additional repos
gh aw init --completions                # Install shell completions
gh aw init --push                       # Initialize and automatically commit/push changes
```

**Interactive Mode:** When invoked without `--engine`, prompts you to select an engine and optionally configure repository secrets using the `gh` CLI.

**Options:** `--engine` (copilot, claude, codex), `--no-mcp`, `--tokens`, `--codespaces`, `--completions`, `--push`

##### `--push` Flag

The `--push` flag automatically commits and pushes initialization changes to the remote repository:

1. **Remote check**: Requires a remote repository to be configured
2. **Branch validation**: Verifies current branch matches repository default branch
3. **User confirmation**: Prompts for confirmation before committing/pushing (skipped in CI)
4. **Pre-check**: Validates working directory is clean before starting
5. **Initialization**: Runs normal init process
6. **Automatic commit**: Stages all changes with commit message "chore: initialize agentic workflows"
7. **Pull and push**: Pulls latest changes with rebase, then pushes to remote

Safety features:
- Prevents accidental pushes to non-default branches
- Requires explicit user confirmation outside CI environments
- Auto-confirms in CI (detected via `CI`, `CONTINUOUS_INTEGRATION`, `GITHUB_ACTIONS` env vars)

When used, requires a clean working directory (no uncommitted changes) before starting.

#### `add`

Add workflows from The Agentics collection or other repositories to `.github/workflows`.

```bash wrap
gh aw add githubnext/agentics/ci-doctor           # Add single workflow
gh aw add "githubnext/agentics/ci-*"             # Add multiple with wildcards
gh aw add ci-doctor --dir shared --number 3      # Organize in subdirectories with copies
gh aw add ci-doctor --create-pull-request        # Create PR instead of commit
```

**Options:** `--dir`, `--number`, `--create-pull-request` (or `--pr`), `--no-gitattributes`

#### `new`

Create a workflow template in `.github/workflows/`. Opens for editing automatically.

```bash wrap
gh aw new                      # Interactive mode
gh aw new my-custom-workflow   # Create template (.md extension optional)
gh aw new my-workflow --force  # Overwrite if exists
```

#### `secrets`

Manage GitHub Actions secrets and tokens.

##### `secrets set`

Create or update a repository secret (from stdin, flag, or environment variable).

```bash wrap
gh aw secrets set MY_SECRET                                    # From stdin
gh aw secrets set MY_SECRET --value "secret123"                # From flag
gh aw secrets set MY_SECRET --value-from-env MY_TOKEN          # From env var
```

**Options:** `--owner`, `--repo`, `--value`, `--value-from-env`, `--api-url`

##### `secrets bootstrap`

Check token configuration and print setup instructions for missing secrets (read-only).

```bash wrap
gh aw secrets bootstrap --engine copilot   # Check Copilot tokens
gh aw secrets bootstrap --engine claude    # Check Claude tokens
```

**Options:** `--engine` (copilot, claude, codex), `--owner`, `--repo`

See [GitHub Tokens reference](/gh-aw/reference/tokens/) for details.

### Building

#### `fix`

Auto-fix deprecated workflow fields using codemods. Runs in dry-run mode by default; use `--write` to apply changes.

```bash wrap
gh aw fix                              # Check all workflows (dry-run)
gh aw fix --write                      # Fix all workflows
gh aw fix my-workflow --write          # Fix specific workflow
gh aw fix --list-codemods              # List available codemods
```

**Options:** `--write`, `--list-codemods`

Available codemods: `timeout_minutes` → `timeout-minutes`, `network.firewall` → `sandbox.agent`, `on.command` → `on.slash_command`

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
gh aw compile --dependabot                 # Generate dependency manifests
gh aw compile --purge                      # Remove orphaned .lock.yml files
```

**Options:** `--validate`, `--strict`, `--fix`, `--zizmor`, `--dependabot`, `--json`, `--watch`, `--purge`

**Strict Mode (`--strict`):** Enforces security best practices: no write permissions (use [safe-outputs](/gh-aw/reference/safe-outputs/)), explicit `network` config, no wildcard domains, pinned Actions, no deprecated fields. See [Strict Mode reference](/gh-aw/reference/frontmatter/#strict-mode-strict).

**Shared Workflows:** Workflows without an `on` field are automatically detected as shared workflow components intended for import by other workflows. These files are validated using a relaxed schema that permits optional markdown content and skip compilation with an informative message. To use a shared workflow, import it in another workflow's frontmatter or with markdown directives. See [Imports reference](/gh-aw/reference/imports/).

### Testing

#### `trial`

Test workflows in temporary private repositories (default) or run directly in specified repository (`--repo`). Results saved to `trials/`.

```bash wrap
gh aw trial githubnext/agentics/ci-doctor          # Test remote workflow
gh aw trial ./workflow.md --use-local-secrets      # Test with local API keys
gh aw trial ./workflow.md --logical-repo owner/repo # Act as different repo
gh aw trial ./workflow.md --repo owner/repo        # Run directly in repository
```

**Options:** `-e`, `--engine`, `--auto-merge-prs`, `--repeat`, `--delete-host-repo-after`, `--use-local-secrets`, `--logical-repo`, `--clone-repo`, `--trigger-context`, `--repo`

#### `run`

Execute workflows immediately in GitHub Actions. Displays workflow URL for tracking.

```bash wrap
gh aw run workflow                          # Run workflow
gh aw run workflow1 workflow2               # Run multiple workflows
gh aw run workflow --repeat 3               # Repeat 3 times
gh aw run workflow --use-local-secrets      # Use local API keys
gh aw run workflow --push                   # Auto-commit, push, and dispatch workflow
gh aw run workflow --push --ref main        # Push to specific branch
```

**Options:** `--repeat`, `--use-local-secrets`, `--push`, `--ref`

##### `--push` Flag

The `--push` flag automatically handles workflow updates before execution:

1. **Remote check**: Requires a remote repository to be configured
2. **Branch validation**: Verifies current branch matches repository default branch (or branch specified with `--ref`)
3. **User confirmation**: Prompts for confirmation before committing/pushing (skipped in CI)
4. **Auto-recompilation**: Detects when `.lock.yml` is outdated and recompiles workflow
5. **Transitive imports**: Collects and stages all imported files recursively
6. **Smart staging**: Stages workflow `.md` and `.lock.yml` files plus dependencies
7. **Automatic commit**: Creates commit with message "Updated agentic workflow"
8. **Workflow dispatch**: Triggers workflow run after successful push

Safety features:
- Prevents accidental pushes to non-default branches (unless explicitly specified with `--ref`)
- Requires explicit user confirmation outside CI environments
- Auto-confirms in CI (detected via `CI`, `CONTINUOUS_INTEGRATION`, `GITHUB_ACTIONS` env vars)

When `--push` is not used, warnings are displayed for missing or outdated lock files.

> [!NOTE]
> Codespaces Permissions
> Requires `workflows:write` permission. In Codespaces, either configure custom permissions in `devcontainer.json` ([docs](https://docs.github.com/en/codespaces/managing-your-codespaces/managing-repository-access-for-your-codespaces)) or authenticate manually: `unset GH_TOKEN && gh auth login`

### Monitoring

#### `list`

List workflows with basic information (name, engine, compilation status) without checking GitHub Actions state. Fast enumeration for discovering available workflows.

```bash wrap
gh aw list                                  # List all workflows
gh aw list ci-                              # Filter by pattern (case-insensitive)
gh aw list --json                           # Output in JSON format
gh aw list --label automation               # Filter by label
```

**Options:** `--json`, `--label`

Unlike `status`, this command does not query GitHub API for workflow state or execution history. Use this for quick discovery and filtering. For detailed status including enabled/disabled state and latest run information, use `status` instead.

#### `status`

List workflows with state, enabled/disabled status, schedules, and labels. With `--ref`, includes latest run status.

```bash wrap
gh aw status                                # All workflows
gh aw status --ref main                     # With run info for main branch
gh aw status --label automation             # Filter by label
gh aw status --repo owner/other-repo        # Check different repository
```

**Options:** `--ref`, `--label`, `--json`, `--repo`

#### `logs`

Download and analyze logs with tool usage, network patterns, errors, warnings. Results cached for ~10-100x speedup on subsequent runs.

```bash wrap
gh aw logs workflow                        # Download logs for workflow
gh aw logs -c 10 --start-date -1w         # Filter by count and date
gh aw logs --ref main --parse --json      # With markdown/JSON output for branch
```

**Options:** `-c`, `--count`, `-e`, `--engine`, `--start-date`, `--end-date`, `--ref`, `--parse`, `--json`, `--repo`

#### `audit`

Analyze specific runs with overview, metrics, tool usage, MCP failures, firewall analysis, noops, and artifacts. Accepts run IDs, workflow run URLs, job URLs, and step-level URLs. Auto-detects Copilot agent runs for specialized parsing.

When provided with a job URL, automatically extracts logs for the specific job. When a step fragment is included, extracts only that step's output. If no step is specified, automatically identifies and extracts the first failing step.

```bash wrap
gh aw audit 12345678                                      # By run ID
gh aw audit https://github.com/owner/repo/actions/runs/123 # By workflow run URL
gh aw audit https://github.com/owner/repo/actions/runs/123/job/456 # By job URL (extracts first failing step)
gh aw audit https://github.com/owner/repo/actions/runs/123/job/456#step:7:1 # By step URL (extracts specific step)
gh aw audit 12345678 --parse                              # Parse logs to markdown
```

Logs are saved to `logs/run-{id}/` with filenames indicating the extraction level (job logs, specific step, or first failing step).

### Management

#### `enable`

Enable one or more workflows by ID, or all workflows if no IDs provided.

```bash wrap
gh aw enable                                # Enable all workflows
gh aw enable ci-doctor                      # Enable specific workflow
gh aw enable ci-doctor daily                # Enable multiple workflows
gh aw enable ci-doctor --repo owner/repo    # Enable in specific repository
```

**Options:** `--repo`

#### `disable`

Disable one or more workflows and cancel any in-progress runs.

```bash wrap
gh aw disable                               # Disable all workflows
gh aw disable ci-doctor                     # Disable specific workflow
gh aw disable ci-doctor daily               # Disable multiple workflows
gh aw disable ci-doctor --repo owner/repo   # Disable in specific repository
```

**Options:** `--repo`

#### `remove`

Remove workflows (both `.md` and `.lock.yml`).

```bash wrap
gh aw remove my-workflow
```

#### `update`

Update workflows based on `source` field (`owner/repo/path@ref`). Default replaces local file; `--merge` performs 3-way merge. Semantic versions update within same major version.

```bash wrap
gh aw update                              # Update all with source field
gh aw update ci-doctor --merge            # Update with 3-way merge
gh aw update ci-doctor --major --force    # Allow major version updates
```

**Options:** `--dir`, `--merge`, `--major`, `--force`

#### `upgrade`

Upgrade repository with latest agent files and apply codemods to all workflows. Ensures workflows are compatible with the latest gh-aw version.

```bash wrap
gh aw upgrade                              # Upgrade repository agent files and all workflows
gh aw upgrade --no-fix                     # Update agent files only (skip codemods and compilation)
gh aw upgrade --push                       # Upgrade and automatically commit/push changes
gh aw upgrade --push --no-fix              # Update agent files and push
```

**Options:** `--dir`, `--no-fix`, `--push`

##### `--push` Flag

The `--push` flag automatically commits and pushes upgrade changes to the remote repository:

1. **Remote check**: Requires a remote repository to be configured
2. **Branch validation**: Verifies current branch matches repository default branch
3. **User confirmation**: Prompts for confirmation before committing/pushing (skipped in CI)
4. **Pre-check**: Validates working directory is clean before starting
5. **Version check**: Ensures gh-aw extension is on latest version
6. **Upgrade process**: Updates agent files, applies codemods, and recompiles workflows
7. **Automatic commit**: Stages all changes with commit message "chore: upgrade agentic workflows"
8. **Pull and push**: Pulls latest changes with rebase, then pushes to remote

Safety features:
- Prevents accidental pushes to non-default branches
- Requires explicit user confirmation outside CI environments
- Auto-confirms in CI (detected via `CI`, `CONTINUOUS_INTEGRATION`, `GITHUB_ACTIONS` env vars)

When used, requires a clean working directory (no uncommitted changes) before starting.

### Advanced

#### `mcp`

Manage MCP (Model Context Protocol) servers in workflows. `mcp inspect` auto-detects safe-inputs.

```bash wrap
gh aw mcp list workflow                    # List servers for workflow
gh aw mcp list-tools <mcp-server>          # List tools for server
gh aw mcp inspect workflow                 # Inspect and test servers
gh aw mcp add                              # Add MCP tool to workflow
```

See [MCPs Guide](/gh-aw/guides/mcps/).

#### `pr transfer`

Transfer pull request to another repository, preserving changes, title, and description.

```bash wrap
gh aw pr transfer <pr-url> --repo target-owner/target-repo
```

#### `mcp-server`

Run MCP server exposing gh-aw commands as tools. Spawns subprocesses to isolate GitHub tokens.

```bash wrap
gh aw mcp-server              # stdio transport
gh aw mcp-server --port 8080  # HTTP server with SSE
```

**Options:** `--port`, `--cmd`
**Available Tools:** status, compile, logs, audit, mcp-inspect, add, update

See [MCP Server Guide](/gh-aw/setup/mcp-server/).

### Utility Commands

#### `version`

Show gh-aw version and product information.

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

**Subcommands:** `install`, `uninstall`, `bash`, `zsh`, `fish`, `powershell`

See [Shell Completions](#shell-completions) for detailed installation instructions.

#### `project`

Create and manage GitHub Projects V2 boards. Use this to create project boards for tracking issues, pull requests, and tasks.

##### `project new`

Create a new GitHub Project V2 owned by a user or organization. Optionally link the project to a specific repository.

```bash wrap
gh aw project new "My Project" --owner @me                      # Create user project
gh aw project new "Team Board" --owner myorg                    # Create org project
gh aw project new "Bugs" --owner myorg --link myorg/myrepo     # Create and link to repo
```

**Options:**
- `--owner` (required): Project owner - use `@me` for current user or specify organization name
- `--link`: Repository to link project to (format: `owner/repo`)

**Token Requirements:**

> [!IMPORTANT]
> The default `GITHUB_TOKEN` cannot create projects. You must use a Personal Access Token (PAT) with Projects permissions:
>
> - **Classic PAT**: `project` scope (user projects) or `project` + `repo` (org projects)
> - **Fine-grained PAT**: Organization permissions → Projects: Read & Write
>
> Configure via `GH_AW_PROJECT_GITHUB_TOKEN` environment variable or use `gh auth login` with a suitable token.

**Related:** See [Tokens Reference](/gh-aw/reference/tokens/) for complete token configuration guide.

## Shell Completions

Enable tab completion for workflow names, engines, and paths.

### Automatic Installation (Recommended)

```bash wrap
gh aw completion install    # Auto-detects your shell and installs
gh aw completion uninstall  # Remove completions
```

After installation, restart your shell or source your configuration file.

### Manual Installation

For manual control, generate and install completion scripts for your shell:

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

Completes workflow names, engine names (copilot, claude, codex), and directory paths.

## Debug Logging

Enable detailed debugging with namespace, message, and time diffs. Zero overhead when disabled.

```bash wrap
DEBUG=* gh aw compile                # All logs
DEBUG=cli:* gh aw compile            # CLI only
DEBUG=*,-tests gh aw compile         # All except tests
```

Use `--verbose` flag for user-facing details instead of DEBUG.

## Smart Features

### Fuzzy Workflow Name Matching

Auto-suggests similar workflow names on typos using Levenshtein distance (up to 3 suggestions, edit distance ≤ 3).

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

## Related Documentation

- [Quick Start](/gh-aw/setup/quick-start/) - Get your first workflow running
- [Frontmatter](/gh-aw/reference/frontmatter/) - Configuration options
- [Packaging & Distribution](/gh-aw/guides/packaging-imports/) - Adding and updating workflows
- [Security Guide](/gh-aw/introduction/architecture/) - Security best practices
- [MCP Server Guide](/gh-aw/setup/mcp-server/) - MCP server configuration
- [Agent Factory](/gh-aw/agent-factory-status/) - Agennt factory status
