# GitHub Actions Workflow Layout Specification

> Auto-generated specification documenting patterns used in compiled `.lock.yml` files.
> Last updated: February 04, 2026

## Overview

This document catalogs all file paths, folder names, artifact names, and other patterns used across our compiled GitHub Actions workflows (`.lock.yml` files).

**Compilation Summary:**
- **Lock files analyzed**: 145
- **Total action uses**: 6,472
- **Unique actions**: 30
- **Unique artifacts**: 18
- **Unique job names**: 24
- **Step IDs documented**: 30+

## GitHub Actions

Common GitHub Actions used across workflows:

| Action | Version/SHA | Description | Context |
|--------|-------------|-------------|---------|
| `./actions/setup` | Local action | Initializes gh-aw runtime environment | Used in virtually all workflows for setting up JavaScript runtime, MCP servers, and safe outputs |
| `actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683` | SHA pinned | Checks out repository code at specific commit | Primary version - used in most workflows |
| `actions/checkout@8e8c483db84b4bee98b60c0593521ed34d9990e8` | SHA pinned | Checks out repository code at specific commit | Alternative version for specific workflows |
| `actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd` | SHA pinned | Checks out repository code at specific commit | Another pinned version |
| `actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f` | SHA pinned | Uploads build artifacts | Primary version for artifact uploads |
| `actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02` | SHA pinned | Uploads build artifacts | Alternative artifact upload version |
| `actions/download-artifact@018cc2cf5baa6db3ef3c5f8a56943fffe632ef53` | SHA pinned | Downloads artifacts from previous jobs | Used in safe-output jobs and conclusion jobs |
| `actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd` | SHA pinned | Runs GitHub API scripts using JavaScript | Primary version for GitHub API interactions |
| `actions/github-script@f28e40c7f34bde8b3046d885e986cb6290c5673b` | SHA pinned | Runs GitHub API scripts | Alternative version |
| `actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f` | SHA pinned | Sets up Node.js environment | Primary version for workflows requiring npm/node |
| `actions/setup-node@6044e13b5dc448c55e2357c09f80417699197238` | SHA pinned | Sets up Node.js environment | Alternative Node.js setup version |
| `actions/setup-python@a26af69be951a213d495a4c3e4e4022e16d87065` | SHA pinned | Sets up Python environment | Used in Python-based workflows |
| `actions/setup-go@3041bf56c941b39c61721a86cd11f3bb1338122a` | SHA pinned | Sets up Go environment | Primary Go setup version |
| `actions/setup-go@4dc6199c7b1a012772edbd06daecab0f50c9053c` | SHA pinned | Sets up Go environment | Alternative Go version |
| `actions/setup-go@7a3fe6cf4cb3a834922a1244abfce67bcef6a0c5` | SHA pinned | Sets up Go environment | Another Go setup version |
| `actions/setup-java@c1e323688fd81a25caa38c78aa6df2d33d3e20d9` | SHA pinned | Sets up Java environment | Used in Java-based workflows |
| `actions/setup-dotnet@67a3573c9a986a3f9c594539f4ab511d57bb3ce9` | SHA pinned | Sets up .NET environment | Used in .NET-based workflows |
| `actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830` | SHA pinned | Caches dependencies and build outputs | Full cache action |
| `actions/cache/restore@0057852bfaa89a56745cba8c7296529d2fc39830` | SHA pinned | Restores cache only | Used for cache restoration step |
| `actions/cache/save@0057852bfaa89a56745cba8c7296529d2fc39830` | SHA pinned | Saves cache only | Used for cache saving step |
| `actions/ai-inference@a6101c89c6feaecc585efdd8d461f18bb7896f20` | SHA pinned | AI inference action | Used for AI model integrations |
| `github/gh-aw/actions/setup@623e612ff6a684e9a8634449508bdda21e2c178c` | SHA pinned | Remote reference to setup action | Used in some external workflow references |
| `docker/setup-buildx-action@8d2750c68a42422c14e847fe6c8ac0403b4cbd6f` | SHA pinned | Sets up Docker Buildx | Used for Docker-based workflows |
| `docker/login-action@5e57cd118135c172c3672efd75eb46360885c0ef` | SHA pinned | Logs into Docker registry | Docker authentication |
| `docker/metadata-action@c299e40c65443455700f0fdfc63efafe5b349051` | SHA pinned | Extracts Docker metadata | Docker image metadata generation |
| `docker/build-push-action@263435318d21b8e681c14492fe198d362a7d2c83` | SHA pinned | Builds and pushes Docker images | Docker build and publish |
| `astral-sh/setup-uv@d4b2f3b6ecc6e67c4457f6d3e41ec42d3d0fcb86` | SHA pinned | Sets up uv (Python package installer) | Modern Python package management |
| `anchore/sbom-action@62ad5284b8ced813296287a0b63906cb364b73ee` | SHA pinned | Generates Software Bill of Materials | Security and compliance |
| `super-linter/super-linter@2bdd90ed3262e023ac84bf8fe35dc480721fc1f2` | SHA pinned | Runs multi-language linting | Code quality checks |
| `github/stale-repos@3477b6488008d9411aaf22a0924ec7c1f6a69980` | SHA pinned | Identifies stale repositories | Repository maintenance |

## Artifact Names

Artifacts uploaded/downloaded between workflow jobs:

| Name | Type | Description | Context |
|------|------|-------------|---------|
| `agent-output` | Upload/Download | AI agent execution output | Contains the agent's response, analysis, and generated content |
| `agent-artifacts` | Upload | Complete agent execution artifacts | Includes all outputs from agent job |
| `agent_outputs` | Upload | Agent outputs (underscore variant) | Alternative naming for agent outputs |
| `safe-output` | Upload/Download | Safe outputs configuration | Configuration data passed from agent to safe-output jobs |
| `safe-outputs-assets` | Upload/Download | Safe output assets | Files and content generated by safe-output handlers |
| `cache-memory` | Upload/Download | Agent cache memory | Persistent memory for agent across runs |
| `cache-memory-focus-areas` | Upload/Download | Focused cache memory | Domain-specific cache for focus areas |
| `cache-memory-repo-audits` | Upload/Download | Repository audit cache | Cache for repository audit workflows |
| `repo-memory-default` | Upload/Download | Default repository memory | Standard repo memory storage |
| `repo-memory-campaigns` | Upload/Download | Campaign repository memory | Memory for campaign-based workflows |
| `python-source-and-data` | Upload | Python source files and data | Used in Python analysis workflows |
| `data-charts` | Upload | Generated data charts | Visualization outputs from data analysis |
| `trending-charts` | Upload | Trending data visualizations | Chart outputs for trending analysis |
| `trending-source-and-data` | Upload | Trending analysis data | Source data for trending workflows |
| `license-report` | Upload | License compliance report | Generated license analysis results |
| `sbom-artifacts` | Upload | Software Bill of Materials | SBOM generation outputs |
| `super-linter-log` | Upload/Download | Linter execution logs | Logs from super-linter runs |
| `threat-detection.log` | Upload | Security threat detection logs | Security analysis outputs |

## Common Job Names

Standard job names across workflows:

| Job Name | Description | Context |
|----------|-------------|---------|
| `activation` | Determines if workflow should run | Uses skip-if-match, skip-if-no-match, and other filters to conditionally activate workflow |
| `pre_activation` | Pre-flight checks before activation | Early validation and environment checks |
| `agent` | Main AI agent execution job | Runs the copilot/claude/codex engine with configured tools and permissions |
| `detection` | Post-agent analysis job | Analyzes agent output for patterns, safe-outputs, and required follow-up actions |
| `conclusion` | Final status reporting job | Runs after all other jobs complete, reports success/failure, creates summaries |
| `safe_outputs` | Executes safe output operations | Dispatcher job that triggers safe-output handler jobs |
| `add_comment` | Adds comment to issue/PR | Safe-output job for commenting via GitHub API |
| `post-issue` | Posts new GitHub issue | Safe-output job for issue creation |
| `post_to_slack_channel` | Posts message to Slack | Integration with Slack notifications |
| `send_slack_message` | Sends Slack message | Alternative Slack integration pattern |
| `search_issues` | Searches GitHub issues | Query and filter issues based on criteria |
| `notion_add_comment` | Adds comment to Notion | Notion integration for commenting |
| `update_cache_memory` | Updates agent cache memory | Persists agent memory to artifacts |
| `push_repo_memory` | Pushes repository memory | Commits repo memory to git repository |
| `upload_assets` | Uploads generated assets | Handles file uploads to various destinations |
| `check_ci_status` | Checks CI build status | Validates CI pipeline completion |
| `check_external_user` | Validates external user access | Security check for external contributors |
| `config` | Configuration and setup job | Prepares workflow configuration |
| `test_environment` | Tests workflow environment | Validates runtime environment setup |
| `release` | Release management job | Handles version releases and publishing |
| `super_linter` | Runs code linting | Multi-language linting across codebase |
| `license-check` | Validates license compliance | Checks dependency licenses |
| `ast_grep` | AST-based code search | Structural code pattern matching |

## Step IDs

Common step IDs used across workflows:

| Step ID | Description | Context |
|---------|-------------|---------|
| `check_membership` | Checks if user is org member | Security validation in activation job |
| `check_stop_time` | Validates workflow stop time | Prevents workflows from running past configured time |
| `check_skip_if_match` | Conditional skip based on pattern match | Activation filter for pattern-based execution |
| `check_skip_if_no_match` | Conditional skip when pattern doesn't match | Activation filter for required patterns |
| `check_command_position` | Validates command position in text | Checks if agent command is in valid position (start/end/anywhere) |
| `check_actor` | Validates workflow actor | Security check for authorized users |
| `checkout-pr` | Checks out pull request code | PR-specific checkout step |
| `ci_check` | CI status validation | Checks CI pipeline status |
| `agentic_execution` | Main agent execution step | Runs the AI agent with configured engine |
| `generate_aw_info` | Generates workflow info JSON | Creates metadata about workflow execution |
| `create_agent_session` | Creates agent session | Initializes agent execution environment |
| `collect_output` | Collects agent output | Gathers outputs from agent execution |
| `detect` | Detection analysis step | Analyzes outputs for patterns |
| `compute_config` | Computes configuration | Calculates runtime configuration |
| `compute-text` | Computes text output | Processes text-based outputs |
| `conclusion` | Conclusion summary step | Generates final workflow summary |
| `handle_agent_failure` | Handles agent failure | Error handling for agent failures |
| `handle_create_pr_error` | Handles PR creation errors | Error handling for PR creation |
| `missing_tool` | Reports missing tool | Safe-output for missing tool reporting |
| `noop` | No-operation step | Placeholder or completion marker |
| `add-comment` | Adds comment step | Comment addition step |
| `assign_to_agent` | Assigns issue to agent | GitHub issue assignment |
| `lock-issue` | Locks GitHub issue | Issue locking step |
| `build` | Build step | Compilation and build operations |
| `check-cache` | Checks cache availability | Cache validation step |
| `check-results` | Checks operation results | Result validation step |
| `get_release` | Fetches release information | GitHub release data retrieval |
| `determine-automatic-lockdown` | Determines if issue should be locked | Issue lockdown decision logic |
| `opencode` | OpenCode integration step | External tool integration |
| `meta` | Metadata processing step | Workflow metadata handling |

## File Paths

Common file paths referenced in workflows:

| Path | Type | Description | Context |
|------|------|-------------|---------|
| `.github/workflows/` | Directory | Workflow definition directory | Contains all `.md` (source) and `.lock.yml` (compiled) workflow files |
| `.github/aw/` | Directory | Agentic workflow configuration | Contains `actions-lock.json` and workflow-specific configs |
| `.github/aw/actions-lock.json` | File | Action version lock file | Pins GitHub Actions to specific SHAs for security |
| `.github/agents/` | Directory | Custom agent definitions | Stores agent markdown files referenced in workflows |
| `/tmp/gh-aw/` | Directory | Runtime temporary directory | Primary workspace for agent execution and artifacts |
| `/tmp/gh-aw/agent-stdio.log` | File | Agent standard I/O log | Captures agent execution logs |
| `/tmp/gh-aw/aw-prompts/prompt.txt` | File | Agent prompt file | Stores the prompt sent to the AI engine |
| `/tmp/gh-aw/aw.patch` | File | Git patch file | Contains code changes generated by agent |
| `/tmp/gh-aw/aw_info.json` | File | Workflow metadata JSON | Runtime workflow information |
| `/tmp/gh-aw/cache-memory` | Directory | Cache memory storage | Persistent agent memory |
| `/tmp/gh-aw/cache-memory-chroma` | Directory | Chroma vector database cache | Vector DB for semantic memory |
| `/tmp/gh-aw/cache-memory-focus-areas` | Directory | Focus area cache | Domain-specific memory storage |
| `/tmp/gh-aw/cache-memory-repo-audits` | Directory | Repository audit cache | Audit-specific memory storage |
| `/tmp/gh-aw/layout-cache` | Directory | Layout specification cache | Cache for layout spec generation |
| `/tmp/gh-aw/prompt-cache` | Directory | Prompt cache storage | Caches prompts for reuse |
| `/tmp/gh-aw/mcp-config/logs/` | Directory | MCP server configuration logs | Logs from MCP server setup |
| `/tmp/gh-aw/mcp-logs/` | Directory | MCP server runtime logs | Runtime logs from MCP servers |
| `/tmp/gh-aw/redacted-urls.log` | File | Redacted URL log | URLs that were redacted for security |
| `/tmp/gh-aw/repo-memory/default` | Directory | Default repo memory | Standard repository memory location |
| `/tmp/gh-aw/repo-memory/campaigns` | Directory | Campaign repo memory | Campaign-specific memory storage |
| `/tmp/gh-aw/safe-inputs/logs/` | Directory | Safe inputs logs | Logs from safe-inputs validation |
| `/tmp/gh-aw/safeoutputs/` | Directory | Safe outputs staging | Staging area for safe-output processing |
| `/tmp/gh-aw/safeoutputs/assets/` | Directory | Safe output assets | Assets generated by safe outputs |
| `/tmp/gh-aw/sandbox/agent/logs/` | Directory | Sandboxed agent logs | Logs from sandboxed agent execution |
| `/tmp/gh-aw/sandbox/firewall/logs/` | Directory | Firewall logs | Security firewall execution logs |
| `/tmp/gh-aw/threat-detection/` | Directory | Threat detection workspace | Security analysis workspace |
| `/tmp/gh-aw/threat-detection/detection.log` | File | Threat detection log | Security threat analysis log |
| `/tmp/gh-aw/python/*.py` | File pattern | Python source files | Python scripts for analysis |
| `/tmp/gh-aw/python/charts/*.png` | File pattern | Generated chart images | PNG chart outputs |
| `/tmp/gh-aw/python/data/*` | File pattern | Python data files | Data files for Python processing |
| `/opt/gh-aw/safe-jobs/` | Directory | Safe output job scripts | Pre-installed safe-output handlers |
| `/opt/gh-aw/actions/` | Directory | Action scripts | Pre-installed action JavaScript files |
| `actions/setup/js/` | Directory | Setup action JavaScript | Source JavaScript for setup action |
| `pkg/workflow/` | Directory | Workflow compilation code | Go package for compiling workflows |
| `pkg/workflow/js/` | Directory | JavaScript runtime code | CommonJS modules for GitHub Actions |
| `pkg/constants/` | Directory | Go constants package | Constant definitions for workflows |
| `scratchpad/` | Directory | Specification documents | Documentation and specification directory |
| `licenses.csv` | File | License report CSV | License compliance report output |

## Working Directories

Common working directories set in workflow steps:

| Directory | Description | Context |
|-----------|-------------|---------|
| `actions/setup/js` | Setup action JavaScript source | Used when running setup action scripts |
| `./docs` | Documentation directory | Documentation build and generation steps |

## Environment Variables

Common environment variables used across workflows:

### Standard Variables

| Variable | Type | Description | Example Value |
|----------|------|-------------|---------------|
| `GH_AW_AGENT_OUTPUT` | Path | Agent output directory | Set by workflow, referenced in artifacts |
| `GH_AW_SAFE_OUTPUTS` | Path | Safe outputs directory | Set by workflow, used by detection job |
| `AGENT_OUTPUT_TYPES` | Output | Types of agent outputs detected | `${{ needs.agent.outputs.output_types }}` |
| `AGENT_CONCLUSION` | Output | Agent job result status | `${{ needs.agent.result }}` |
| `BASH_DEFAULT_TIMEOUT_MS` | Config | Default bash command timeout | `60000`, `300000`, `600000` |
| `BASH_MAX_TIMEOUT_MS` | Config | Maximum bash command timeout | `60000`, `300000`, `600000` |
| `AWF_LOGS_DIR` | Path | Firewall logs directory | `/tmp/gh-aw/sandbox/firewall/logs` |

### Branch Names

| Variable | Type | Description | Example Value |
|----------|------|-------------|---------------|
| `BRANCH_NAME` | Config | Target branch for memory/updates | `memory/campaigns`, `memory/cli-performance`, `daily/default` |

### Artifact Directories

| Variable | Type | Description | Example Value |
|----------|------|-------------|---------------|
| `ARTIFACT_DIR` | Path | Directory for artifact storage | `/tmp/gh-aw/repo-memory/campaigns`, `/tmp/gh-aw/repo-memory/default` |

### Secret References

| Variable | Type | Description | Context |
|----------|------|-------------|---------|
| `ANTHROPIC_API_KEY` | Secret | Anthropic Claude API key | `${{ secrets.ANTHROPIC_API_KEY }}` |
| `AZURE_CLIENT_ID` | Secret | Azure AD client ID | `${{ secrets.AZURE_CLIENT_ID }}` |
| `AZURE_CLIENT_SECRET` | Secret | Azure AD client secret | `${{ secrets.AZURE_CLIENT_SECRET }}` |
| `AZURE_TENANT_ID` | Secret | Azure AD tenant ID | `${{ secrets.AZURE_TENANT_ID }}` |

### Workflow Inputs

| Variable | Type | Description | Example Value |
|----------|------|-------------|---------------|
| `ORGANIZATION` | Input | GitHub organization name | `${{ github.event.inputs.organization || 'github' }}` |
| `ADDITIONAL_METRICS` | Input | Additional metrics to collect | `release,pr` |

## Go Code Constants

Key constants defined in `pkg/constants/`:

```go
// Job Names
const AgentJobName JobName = "agent"
const ActivationJobName JobName = "activation"
const PreActivationJobName JobName = "pre_activation"
const DetectionJobName JobName = "detection"

// Artifact Names
const SafeOutputArtifactName = "safe-output"
const AgentOutputArtifactName = "agent-output"

// Step IDs
const CheckMembershipStepID StepID = "check_membership"
const CheckStopTimeStepID StepID = "check_stop_time"
const CheckSkipIfMatchStepID StepID = "check_skip_if_match"
const CheckSkipIfNoMatchStepID StepID = "check_skip_if_no_match"
const CheckCommandPositionStepID StepID = "check_command_position"
```

## JavaScript Path Patterns

Common path patterns in JavaScript files (`actions/setup/js/*.cjs`):

```javascript
// Setup action destination
const SetupActionDestination = "/tmp/gh-aw/actions"

// Common imports
const { setupGlobals } = require('/tmp/gh-aw/actions/setup_globals.cjs');
const { main } = require('/opt/gh-aw/actions/assign_issue.cjs');
const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

// Artifact references
const artifactDir = '/tmp/gh-aw/threat-detection/';
const patchPath = 'agent-artifacts/tmp/gh-aw/aw.patch';
```

## Docker Integration

Docker-related actions and patterns:

| Component | Description | Context |
|-----------|-------------|---------|
| `docker/setup-buildx-action` | Docker Buildx setup | Enables multi-platform builds |
| `docker/login-action` | Docker registry authentication | Logs into container registries |
| `docker/metadata-action` | Docker metadata extraction | Generates tags and labels |
| `docker/build-push-action` | Docker build and push | Builds and publishes container images |

## Usage Guidelines

### Artifact Naming
- Use descriptive hyphenated names (e.g., `agent-output`, `mcp-logs`)
- Separate words with hyphens, not underscores (except legacy patterns)
- Use singular nouns (e.g., `safe-output` not `safe-outputs`)
- Prefix with domain when needed (e.g., `cache-memory-focus-areas`)

### Job Naming
- Use snake_case for job names (e.g., `create_pull_request`, `check_membership`)
- Use descriptive verbs (e.g., `update_`, `check_`, `create_`, `send_`)
- Keep names under 30 characters when possible
- Standard suffixes: `_check`, `_status`, `_memory`

### Step ID Naming
- Use snake_case for step IDs (e.g., `check_actor`, `generate_aw_info`)
- Use descriptive action verbs (e.g., `check_`, `generate_`, `handle_`, `compute_`)
- Keep IDs concise but meaningful
- Standard prefixes: `check_` (validation), `handle_` (error handling), `compute_` (calculation)

### Path References
- Use relative paths from repository root for repo files
- Use absolute paths for `/tmp/gh-aw/` runtime files
- Use `/opt/gh-aw/` for pre-installed scripts
- Always use forward slashes, even on Windows

### Action Pinning
- **Always pin actions to full commit SHA for security** (40-character hex)
- Never use tag references (e.g., `@v3`) in production workflows
- Document the version/tag that corresponds to each SHA in comments
- Update SHAs via `.github/aw/actions-lock.json` configuration

### Environment Variables
- Use `GH_AW_` prefix for gh-aw specific variables
- Use uppercase with underscores (e.g., `AGENT_OUTPUT_TYPES`)
- Reference other job outputs using `${{ needs.job_name.outputs.output_name }}`
- Reference secrets using `${{ secrets.SECRET_NAME }}`

### File Organization
- Source workflows: `.github/workflows/*.md`
- Compiled workflows: `.github/workflows/*.lock.yml`
- Configuration: `.github/aw/`
- Runtime: `/tmp/gh-aw/`
- Pre-installed: `/opt/gh-aw/`
- Documentation: `scratchpad/`

## Pattern Analysis

### Most Common Patterns

1. **Setup Action**: Nearly every workflow uses `./actions/setup` as first step
2. **Checkout**: `actions/checkout` is used in 95%+ of workflows
3. **Artifact Upload/Download**: Extensive use of artifact passing between jobs
4. **Environment References**: Heavy use of job outputs via `needs.job.outputs.`
5. **Security**: All external actions pinned to full SHAs

### Workflow Structure Pattern

Typical workflow job sequence:

```
activation → agent → detection → safe_outputs → conclusion
     ↓                    ↓            ↓              ↓
     ↓                    ↓            ↓         (always runs)
(conditional)    (uploads artifacts) (dispatches)
```

### Artifact Flow Pattern

```
agent job → uploads agent-output artifact
         → uploads safe-output artifact

detection job → downloads agent-output
              → analyzes outputs
              → triggers safe_outputs job

safe_outputs → downloads safe-output
             → dispatches handler jobs

conclusion → downloads all artifacts (optional)
           → generates summary
```

## Version History

- **February 04, 2026**: Updated specification from 145 lock files (previously 137)
  - Documented 30 unique GitHub Actions (up from 25)
  - Cataloged 18 artifact names (up from 17)
  - Listed 24 common job names (up from 20)
  - Documented 30+ step IDs
  - Extracted 50+ file path patterns
  - Analyzed Go constants and JavaScript patterns
  - Added comprehensive usage guidelines
  - Expanded environment variables section
  - Added Docker integration section

- **January 23, 2026**: Initial specification generated from 137 lock files

---

*This document is automatically maintained by the Layout Specification Maintainer workflow.*
*To update this specification, trigger the `layout-spec-maintainer` workflow or run: `gh aw run layout-spec-maintainer.md`*
