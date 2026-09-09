---
title: Glossary
description: Definitions of technical terms and concepts used throughout GitHub Agentic Workflows documentation.
sidebar:
  order: 1000
---

Definitions of key terms used in GitHub Agentic Workflows, a system for running AI-powered repository automation through GitHub Actions.

## Core Concepts

### Agentic

Having agency - the ability to act independently, make context-aware decisions, and adapt behavior based on circumstances. Agentic workflows use AI to understand context and choose appropriate actions, contrasting with deterministic workflows that execute fixed sequences. From "agent" + "-ic" (having the characteristics of).

### Agentic Workflow

AI-powered repository automation that runs an AI agent through GitHub Actions. An agentic workflow is authored primarily as Markdown instructions with YAML frontmatter for triggers, permissions, tools, the AI engine, and controlled outputs. For example, instead of "if issue has label X, do Y", an author can ask the AI agent to analyze an issue and provide context based on its specific content.

### Orchestration

Workflows that coordinate one or more worker workflows toward a shared goal. An orchestrator decides what work to do next and dispatches workers, while workers execute concrete tasks with scoped tools and limits. See the [OrchestratorOps pattern](/gh-aw/patterns/orchestrator-ops/).

### Orchestrator Workflow

A workflow that fans out work by dispatching other workflows (workers), aggregates results, and optionally posts summaries.

### Worker Workflow

A workflow dispatched by an orchestrator that performs a focused unit of work (triage, analysis, code changes, validation).

### AI Agent

The reasoning component that interprets an agentic workflow's natural-language instructions, uses configured tools, and generates outputs from repository context. GitHub Actions runs the AI agent through a selected [AI engine](#engine).

### Frontmatter

Configuration section at the top of a workflow file, enclosed between `---` markers. Contains YAML settings controlling when the workflow runs, permissions, and available tools, separating technical configuration from natural language instructions.

### Intent (`intent:`)

Optional frontmatter field describing the durable outcome a workflow exists to achieve, rendered as a comment in the generated lock file. Unlike `description`, which explains what the workflow does, `intent` explains why it exists and stays implementation-independent, so it remains valid even as the workflow's implementation changes.

### Compilation

Translating Markdown workflows (`.md` files) into GitHub Actions YAML format (`.lock.yml` files), including validation, import resolution, tool configuration, and security hardening.

### Workflow Lock File (.lock.yml)

The compiled GitHub Actions workflow file from a workflow markdown file (`.md`). Contains complete GitHub Actions YAML with security hardening applied. Both `.md` and `.lock.yml` files should be committed to version control. At runtime, GitHub Actions executes the lock file using a coding agent while referencing the markdown for instructions.

## Tools and Integration

### MCP (Model Context Protocol)

A standardized protocol that allows AI agents to securely connect to external tools, databases, and services. MCP enables workflows to integrate with GitHub APIs, web services, file systems, and custom integrations while maintaining security controls.

### MCP Gateway

A transparent proxy service that enables unified HTTP access to multiple MCP servers using different transport mechanisms (stdio, HTTP). Provides protocol translation, server isolation, authentication, and health monitoring, allowing clients to interact with multiple backends through a single HTTP endpoint.

### Trusted Bots (`sandbox.mcp.trusted-bots`)

A frontmatter field that passes additional GitHub bot identity strings to the [MCP Gateway](#mcp-gateway). The gateway merges these with its built-in trusted identity list to determine which bot identities are permitted. This field is additive — it can only extend the gateway's internal list, not remove built-in entries. Configured under `sandbox.mcp:` and compiled into the `trustedBots` array in the generated gateway configuration. Example entries: `github-actions[bot]`, `copilot-swe-agent[bot]`. See [MCP Gateway Reference](/gh-aw/reference/mcp-gateway/).

### MCP Gateway Environment Injection

The mechanism that transports `sandbox.mcp.env` custom environment variable values from workflow frontmatter into the MCP Gateway's Docker container. Values are routed through compiler-controlled, indexed transport variables (`GH_AW_MCP_GATEWAY_ENV_0`, `GH_AW_MCP_GATEWAY_ENV_1`, …) and a companion manifest variable (`GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES`), rather than being interpolated directly into the generated shell script or Docker command string. A JavaScript launcher (`start_mcp_gateway.cjs`) reads the manifest and reconstructs atomic `-e NAME=VALUE` Docker arguments at runtime. Both the Go compiler and the JS launcher validate variable names against `^[A-Z_][A-Z0-9_]*$`, preventing shell metacharacters or dangerous names (such as `BASH_ENV`) in custom values from being sourced or interpreted before the gateway process starts. See [MCP Gateway Reference](/gh-aw/reference/mcp-gateway/).

### Agent Identifier Configuration (`agentId`, `agentIds`)

A [MCP Gateway](#mcp-gateway) configuration setting that identifies the agent or session(s) authenticating through the gateway. `agentId` specifies a single string identifier; `agentIds` specifies an array of one or more identifiers, enabling a primary agent and enclave sessions to share one gateway instance while retaining independent [DIFC](#difc-proxy-toolsgithubintegrity-proxy) state and policies. Exactly one of the two fields must be present in a given gateway configuration — specifying both, or neither, is rejected as invalid. Both replace the legacy `api_key`/`apiKey` spelling in generated configuration. See [MCP Gateway Reference](/gh-aw/reference/mcp-gateway/).

### MCP Gateway Mount-Roots Allowlist (`MCP_GATEWAY_ALLOWED_MOUNT_ROOTS`)

The compiler-computed value that authorizes host-path mounts for MCP backend server containers under the gateway's trusted host-path mount policy. `buildMCPGatewayAllowedMountRoots` collects every mount surface the compiler configures — the built-in workspace, gh-aw runtime, safeoutputs, and temp paths, gateway-level `sandbox.mcp.mounts`, and per-server `mounts` fields or `-v`/`--volume` args — and forwards the result to the gateway container via `-e MCP_GATEWAY_ALLOWED_MOUNT_ROOTS`. Without an explicit allowlist entry, the gateway's default mount policy rejects read-write access to paths like `$GITHUB_WORKSPACE`, breaking tool registration for backends (such as `safeoutputs`) that require it. See [MCP Gateway Reference](/gh-aw/reference/mcp-gateway/).

### MCP Server

A service that implements the Model Context Protocol to provide specific capabilities to AI agents. Examples include the GitHub MCP server (for GitHub API operations) or custom `mcp-servers` entries for specialized tools. The built-in `tools.playwright` integration no longer provisions an MCP server; see [Playwright CLI Mode](#playwright-cli-mode-toolsplaywrightmode-cli) for the browser automation replacement.

### Playwright CLI Mode (`tools.playwright.mode: cli`)

The only supported mode for the built-in `tools.playwright` tool. The compiler installs `@playwright/cli` as a global npm package on the runner, and the agent drives browser automation by invoking `playwright-cli <command>` (for example `goto`, `snapshot`, `screenshot`, `eval`, `run-code`) from bash instead of loading MCP tool schemas into context. Built-in Playwright MCP support was removed; `mode: mcp` is now a compile-time error, and workflows that still require an MCP-backed Playwright must configure it explicitly as a custom `mcp-servers` entry. When the workflow runs inside the [AWF](#awf-agent-workflow-firewall) sandbox, the compiler injects an additional policy prompt that reinforces binding local servers to `127.0.0.1`, waiting for loopback readiness before navigating, and never installing packages or browsers at runtime. See [Playwright Reference](/gh-aw/reference/playwright/).

### Required Field (`required`)

An MCP server field that controls startup criticality. By default every configured MCP server is startup-critical: the agent job fails if the server cannot be reached during the pre-flight connectivity check. Setting `required: false` marks a server as best-effort, so an unreachable server logs a warning and the workflow continues without it, rather than aborting the run. At least one server must still connect successfully for startup to proceed. See [MCPs Guide](/gh-aw/guides/mcps/).

### Partial Results (MCP Logs)

A response pattern used by the `gh aw` MCP server's `logs` tool when a gateway timeout or token budget guardrail prevents returning a complete result set. Instead of failing the call, the tool returns the JSON data it collected so far with `partial: true` and continuation parameters (such as `after_run_id`/`before_run_id`) so the caller can request the remaining results in a follow-up call. See [`gh aw` as an MCP Server](/gh-aw/reference/gh-aw-as-mcp-server/).

### Logs Storage Budget (`--max-storage`)

A `gh aw logs` flag that caps total on-disk storage in MB for downloaded run artifacts and cache data. When downloads approach the configured limit, the command performs selective pruning: it removes non-essential cache data (such as prunable log-cache files not needed by already-completed runs) before falling back to stopping further downloads with `errLogsStorageLimitReached`. Reserving space per download and recording pruned bytes lets long-running multi-workflow log collection stay within a fixed disk budget instead of exhausting it. See [Monitoring Costs with `gh aw logs`](/gh-aw/reference/cost-management/#monitoring-costs-with-gh-aw-logs).

### QMD Documentation Search (`qmd:`)

A built-in tool that provides vector similarity search over documentation files. Configured via `tools.qmd:` in frontmatter, the `qmd` tool runs [tobi/qmd](https://github.com/tobi/qmd) as an MCP server so agents can find relevant documentation by natural language query. The search index is built in a dedicated indexing job (which has `contents: read`) and shared with the agent job via `actions/cache`, so the agent job does not need `contents: read`. Supports indexing from repository checkouts, GitHub code search queries, and cache-only read-only mode. See [QMD Documentation Search](/gh-aw/experimental/qmd/).

### Tools

Capabilities that an AI agent can use during workflow execution. Tools are configured in the frontmatter and include GitHub operations ([`github:`](/gh-aw/reference/github-tools/)), file editing (`edit:`), web access (`web-fetch:`, `web-search:`), shell commands (`bash:`), browser automation ([`playwright:`](/gh-aw/reference/playwright/), CLI-only — see [Playwright CLI Mode](#playwright-cli-mode-toolsplaywrightmode-cli)), and custom MCP servers.

### GitHub Access Mode (`tools.github.mode`)

A `tools.github` field that controls how the agent accesses GitHub APIs. Three values are supported: `gh-proxy` (recommended — provides pre-authenticated `gh` CLI prompt guidance without mounting a GitHub MCP server, replacing the deprecated `features.cli-proxy: true`), `local` (Docker-based GitHub MCP server, the legacy default), and `remote` (hosted GitHub MCP server at `api.githubcopilot.com`). Use `gh-proxy` for better performance; use `local` or `remote` when MCP-based GitHub toolsets are required. See [GitHub Tools Reference](/gh-aw/reference/github-tools/).

### Allowed Repos (`tools.github.allowed-repos`)

A GitHub Tools configuration field that restricts which repositories the agent can access through GitHub tools during workflow execution. Accepts `"all"` (default — all repositories accessible by the token), `"public"` (public repositories only), `"current"` (the repository where the workflow is running, normalized to `${{ github.repository }}`), or an array of repository patterns (`"owner/repo"`, `"owner/*"`, `"owner/prefix*"`). Wildcards are only permitted at the end of the repository name component. Use `current` in reusable workflows to express "this repository only" without hard-coding owner/repo values. Patterns must be lowercase. See [GitHub Tools Reference](/gh-aw/reference/github-tools/).

```aw wrap
tools:
  github:
    toolsets: [issues, pull_requests]
    allowed-repos: current
    min-integrity: approved
```

### Tool Call Limits (`max-calls`)

A per-tool call cap declared inline on entries in `tools.github.allowed`. Each entry can be a plain string tool name (no limit) or an object with a `name` field and an optional `max-calls` integer. When `max-calls` is set, the MCP gateway enforces that the tool may be called at most that many times in a single workflow run, preventing runaway re-invocation of read tools. Call limits are emitted into the `allow-only.tool-call-limits` guard policy at compile time; enforcement is delegated to the AWF MCP gateway. See [GitHub Tools Reference](/gh-aw/reference/github-tools/).

```aw wrap
tools:
  github:
    allowed:
      - issue_read          # no limit
      - name: list_labels
        max-calls: 3
```

### Ignore If Missing (`ignore-if-missing`)

A GitHub App authentication field that gracefully skips token minting when `client-id` or `private-key` resolve to empty strings at runtime (e.g., on fork pull requests where App secrets are unavailable). When set to `true` under `github-app.ignore-if-missing`, the workflow falls back to the standard token chain (`secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN`) instead of failing. Applies consistently to all token mint paths: safe outputs, activation, pre-activation, and checkout. Default behavior (fail when keys are empty) remains unchanged when omitted or set to `false`. See [Authentication Reference](/gh-aw/reference/auth/).

```aw wrap
safe-outputs:
  github-app:
    client-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    ignore-if-missing: true
  create-issue:
```

### GitHub App Repositories (`github-app.repositories`)

A `safe-outputs.github-app` field that controls which repositories are included in the GitHub App installation token scope. Accepts `["*"]` to request an installation token without an explicit repository restriction (granting access to all repositories the App installation covers), or an array of explicit repository names to request a narrowly-scoped token. When `["*"]` is specified, implementations omit the `repositories` parameter from the token-minting API call — they do not substitute the triggering repository name. This behavior applies to both the activation-job token and subsequent safe-output-job tokens, including `workflow_call` reusable-workflow scenarios where the agent configuration may live in the callee repository. When the field is omitted, implementations default to the triggering repository as the token scope. See [Safe Outputs Specification](/gh-aw/specs/safe-outputs-specification/).

```aw wrap
safe-outputs:
  github-app:
    client-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    repositories: ["*"]
```

### Jira Tools (`jira:`)

A built-in tool that connects agentic workflows to Atlassian's official remote Rovo MCP endpoint (`https://mcp.atlassian.com/v1/mcp` by default) for read-only Jira access from non-interactive GitHub Actions workloads. Browser OAuth, device login, and user-consent flows are not supported. Authentication uses either a service account API key (HTTP bearer) or an Atlassian account email and API token (HTTP Basic), configured under `tools.jira.auth`. The required `allowed` list restricts the workflow to a fixed set of nine read-only operations (e.g., `getJiraIssue`, `searchJiraIssuesUsingJql`, `lookupJiraAccountId`); `allowed: ["*"]` expands to that same fixed list at compile time and never grants the full, unrestricted MCP tool set. See [Tools Reference](/gh-aw/reference/tools/#jira-tools-jira).

## Security and Outputs

### Enclaves (`enclaves:`)

A top-level frontmatter array that enables finite-disclosure access to approved repositories from within a public-facing workflow. Repository sensitivities are `public`, `trusted`, `internal`, `confidential`, or `sealed`; `trusted` is unmetered and permits free-form strings only inside a declared strict structured response schema, while the other sensitivities remain finite-schema-only. The compiler registers `enclave_run_script` or `enclave_run_agent` tools from the keyed `script`/`agent` entries present on the `awf-enclave` MCP route, compiled through [mcpg](#mcp-gateway) with run-scoped capability handoff, timeout derivation, and network validation. Enclaves require AWF network isolation, which every supported `sandbox.agent.runtime` profile provides, so the compiler can launch mcpg in bridge mode. Agent enclaves may opt into `github.cli: issues-read-v1`, which uses a distinct identity on the shared compiler-owned mcpg gateway and permits only `list_issues` and `issue_read` for the configured trusted repositories. AWF privately stages this identity and connects the enclave directly to `/mcp/github`; the enclave receives neither a GitHub token nor a `gh` executable. See [Private Repository Enclaves](/gh-aw/experimental/enclaves/).

### Dynamic Enclave Delegation Controller (`enclaves[].agent.dynamic`)

An [Enclaves](#enclaves-enclaves) `dynamic:` mode that lets an agent enclave select one admitted repository at invocation time (via `allowed-owners`/`allowed-repositories`) instead of enumerating every repository statically in `repos:`. The compiler emits a fixed-sensitivity policy envelope with finite resource limits, total quotas, audit labels, and an absolute `expires-at` timestamp, then starts mcpg's `github-repository-delegation-v1` controller and hands AWF a host-private delegation control endpoint that is excluded from both primary and enclave agent environments. The effective envelope expiry is `min(expires-at, job-start + enclave timeout)`. Requires AWF `v0.28.14`+ and mcpg `v0.4.17`+ (the first release accepting the controller's atomic bootstrap configuration). Dynamic mode is agent-only; script enclaves remain static and must declare `repos`. See [Private Repository Enclaves](/gh-aw/experimental/enclaves/).

### MCP Scripts

Custom MCP tools defined inline in workflow frontmatter using JavaScript or shell scripts. Enables lightweight tool creation while maintaining controlled secret access. Tools are generated at runtime and mounted as an MCP server with typed input parameters, default values, and environment variables. Configured via `mcp-scripts:` section. Use the `dependencies:` field to declare third-party npm or PyPI packages installed before first invocation. See [MCP Scripts Reference](/gh-aw/reference/mcp-scripts/).

### DeepSec

An on-demand agentic security scanning pattern that bootstraps a bounded vulnerability review and files one issue for actionable findings. DeepSec workflows accept configurable `limit` (maximum files to investigate), `thinking-level` (`low`, `medium`, `high`, `xhigh`), and `agent` (backend engine: `codex`, `claude`, `pi`) inputs. Typically triggered manually via `workflow_dispatch` and configured with `strict: true` and a scoped network allowlist. Results are written as a labeled issue using the `create-issue` safe output.

### SARIF

Static Analysis Results Interchange Format - a standardized JSON format for reporting results from static analysis tools. Used by GitHub Code Scanning to display security vulnerabilities and code quality issues. Workflows can generate SARIF files using the `create-code-scanning-alert` safe output.

### VulnHunter

An open-source vulnerability hunting methodology from Capital One that provides structured guidance for agentic security analysis. When used in GitHub Agentic Workflows, a preparatory job downloads the VulnHunter source tree and bundles it with a clean repository snapshot as an artifact. The agent job mounts the bundle and applies the VulnHunter methodology to produce structured findings. See [Daily VulnHunter Scan](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-vulnhunter-scan.md) for a reference implementation.

### Workload Identity Federation (WIF)

A keyless authentication mechanism that exchanges short-lived GitHub OIDC tokens for provider-specific credentials, eliminating the need for long-lived API key secrets in the repository. Supported for the `claude` engine (Anthropic WIF) and the `gemini` engine (Google Cloud WIF via Vertex AI). Configured under `engine.auth` with `type: github-oidc` and a `provider` discriminator (`anthropic` or `gcp`). When active, the compiler suppresses static-key validation and emits the appropriate authentication environment variables for the runtime. See [Auth Reference](/gh-aw/reference/auth/).

### Safe-Output Field Aliases

A compiler feature that maps common agent mistakes — including MCP tool name variants (e.g., `create_issue_comment`) and underscore-to-hyphen swaps (e.g., `add_comment`) — to their correct `safe-outputs` canonical field names (e.g., `add-comment`). When a schema validation error is raised under the `/safe-outputs` path, the compiler consults a curated alias map and emits a precise "Did you mean 'X'?" suggestion instead of a generic field list. Improves the authoring iteration cycle for agents and workflow authors encountering `safe-outputs` configuration errors.

### Safe Outputs

Pre-approved actions the AI can take without elevated permissions. The AI generates structured output describing what to create (issues, comments, pull requests), processed by separate permission-controlled jobs. Configured via `safe-outputs:` section, letting AI agents create GitHub content without direct write access.

### Disclosure Header (`safe-outputs.messages.disclosure-header`)

A `safe-outputs.messages` field that prepends an AI authorship disclosure to every message body created by safe outputs (issues, comments, pull requests, and discussions). Set to `true` to use the built-in default text ("Automated content generated by the {workflow_name} workflow"), or provide a custom template string with `{workflow_name}` and `{run_url}` placeholders. When omitted, no disclosure header is added. Configured under `safe-outputs.messages:`.

```aw wrap
safe-outputs:
  messages:
    disclosure-header: true
  create-issue:
```

### Outcome

The observable repository state that follows a [safe output](#safe-outputs). While safe outputs record what a workflow did, outcomes record what happened afterward — whether a pull request was merged, an issue was resolved, or a comment received follow-up activity. Outcome data is based on visible repository state, not on the workflow's self-assessment. See [Outcomes](/gh-aw/reference/outcomes/).

### Outcome State

The classification assigned to an evaluated safe output result. The six states are `accepted`, `rejected`, `pending`, `ignored`, `lifecycle`, and `lifecycle_close`. See [Outcomes](/gh-aw/reference/outcomes/) for the exact criteria for each state.

### Accepted Outcome

The simplest useful unit for measuring workflow effectiveness. An outcome is accepted when its result was kept, merged, completed, or acted on — for example, a merged pull request, a resolved issue, or a comment that received a reaction or reply. Different output types use type-specific rules to determine acceptance. See [Outcomes](/gh-aw/reference/outcomes/).

### Outcome Efficiency

A cost-quality metric computed as AI Credits (AIC) divided by accepted outcomes. Lower values indicate the workflow consumed fewer AI Credits per accepted result. Outcome efficiency makes the difference between cost savings from genuine efficiency gains and cost savings from doing less useful work. See [Measuring Impact](/gh-aw/practices/measuring-impact/).

### Pwn Request

A critical security vulnerability that occurs when a `pull_request_target` workflow checks out and executes code from a fork PR. Because `pull_request_target` runs in the context of the target (base) branch with full write permissions and access to repository secrets, executing untrusted fork code grants an attacker the ability to exfiltrate secrets or make unauthorized changes. The compiler emits a warning (non-strict mode) or a hard error (strict mode) when `pull_request_target` is used without `checkout: false`. Add `checkout: false` to prevent the insecure checkout; use `pull_request` instead when you do not need write-back access. See the [GitHub Security Lab advisory on pwn requests](https://securitylab.github.com/resources/github-actions-preventing-pwn-requests/).

### Threat Detection

Automated security analysis that scans agent output and code changes for potential security issues before application. When safe outputs are configured, a threat detection job automatically runs between the agent job and safe output processing to identify prompt injection attempts, secret leaks, and malicious code patches. See [Threat Detection Reference](/gh-aw/reference/threat-detection/).

### Threat Detection Max AI Credits (`safe-outputs.threat-detection.max-ai-credits`)

A `safe-outputs.threat-detection` field that caps the total AI Credits (AIC) the AWF proxy will spend for a single threat-detection run. Defaults to `400` AIC when omitted. Accepts an integer, a `K`/`M` suffix string (e.g., `750`), or `-1` to disable budget steering for detection runs. The organization-wide default can be overridden at runtime via `vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS` without recompiling. Precedence: frontmatter literal → `GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS` variable → built-in default of `400`. See [Compiler Enterprise Environment Controls](/gh-aw/reference/compiler-enterprise-environment-controls/).

```aw wrap
safe-outputs:
  create-pull-request:
  threat-detection:
    max-ai-credits: 750
```

### Staged Mode

A preview mode where workflows simulate actions without making changes. The AI generates output showing what would happen, but no GitHub API write operations are performed. Use for testing before production runs. See [Staged Mode](/gh-aw/reference/staged-mode/) for details.

### Integrity Filtering

A guardrail feature that controls which GitHub content an agent can access, filtering by author trust and merge status. Content below the configured `min-integrity` threshold is silently removed before the AI engine sees it. The four levels are `merged`, `approved`, `unapproved`, and `none` (most to least restrictive). For public repositories, `min-integrity: approved` is applied automatically — restricting content to owners, members, and collaborators — even without additional authentication. Set `min-integrity: none` to allow all content through for workflows designed to process untrusted input (e.g., triage bots).

Three additional fields extend integrity filtering beyond the level threshold: `trusted-users` elevates specific GitHub usernames to `approved` integrity regardless of their author association; `blocked-users` unconditionally denies content from listed usernames regardless of level; and `approval-labels` promotes items bearing any listed label to `approved` integrity, enabling human-review workflows. See [Integrity Filtering](/gh-aw/reference/integrity/).

### DIFC Proxy (`tools.github.integrity-proxy`)

Controls full Data Integrity and Flow Control (DIFC) proxy enforcement. When `tools.github.min-integrity` is configured, the compiler injects proxy steps around the agent job that enforce integrity-level isolation at the network boundary. The proxy is **enabled by default** — set `tools.github.integrity-proxy: false` to disable it and rely solely on MCP gateway-level filtering. Filtered content is recorded as `DIFC_FILTERED` events in `gateway.jsonl` for later inspection. See [Integrity Filtering](/gh-aw/reference/integrity/).

### `private-to-public-flows` (`tools.github.private-to-public-flows`)

A frontmatter field that opts a workflow out of cross-visibility protections enforced by the [MCP Gateway](#mcp-gateway). By default, workflows running in private repositories are prevented from writing to public repositories (the gateway enforces `sink-visibility="public"` which blocks agents with non-empty secrecy). Setting `private-to-public-flows: allow` disables this enforcement for all MCP servers; setting it to a list of server IDs disables it only for those servers. Incompatible with `guards_mode: strict` when using the blanket `allow` form. See [MCP Gateway Reference](/gh-aw/reference/mcp-gateway/).

```aw wrap
tools:
  github:
    private-to-public-flows: allow
```

### `sink-visibility`

An MCP Gateway write-sink guard field that declares the visibility of the safe-outputs target repository (`"public"`, `"private"`, or `"internal"`). When set to `"public"`, any agent with non-empty secrecy is blocked from writing — the DIFC write check fails regardless of `accept` patterns. The gh-aw compiler sets this automatically at runtime using repository visibility detected by the activation job; workflow authors do not need to configure it manually. Non-`"public"` values (or an omitted field) leave `accept` pattern enforcement unchanged. See [MCP Gateway Reference](/gh-aw/reference/mcp-gateway/).

### Integrity Reactions (`features.integrity-reactions`)

A feature flag that enables GitHub reactions (👍, ❤️, 👎, 😕) to promote or demote content past the integrity filter. When `integrity-reactions: true` is set, trusted members can add a reaction to an issue or comment to elevate its integrity to `approved` (endorsement reactions) or demote it to `none` (disapproval reactions) — without modifying labels. Enabling this flag automatically activates `cli-proxy` mode, which is required to identify reaction authors at the network boundary. Available from gh-aw v0.68.2. See [Promoting and demoting items via reactions](/gh-aw/reference/integrity/#promoting-and-demoting-items-via-reactions).

### Soft-Skip

A safe output processing behavior where a handler skips an operation with a warning message instead of failing the workflow step. A soft-skip occurs when a condition prevents execution but is not considered a hard error — for example, when `target: triggering` is configured but no triggering PR context exists, or when `resolveReviewThread` is rejected by the GitHub API due to token scope limitations. Soft-skips allow the workflow to continue processing other safe outputs rather than aborting entirely. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/).

### Status Comment

A comment posted on the triggering issue, pull request, or discussion that shows workflow run status (started and completed). Configured via `status-comment: true` under `on:`. Defaults to `true` for `slash_command` and `label_command` triggers; must be explicitly enabled for other trigger types. Set `status-comment: false` to disable. Not automatically bundled with `ai-reaction` — each must be configured independently.

### Permissions

Access controls defining workflow operations. Workflows follow least privilege, starting with read-only access by default. Write operations are typically handled through safe outputs.

### Safe Output Messages

Customizable messages workflows can display during execution. Configured in `safe-outputs.messages` with types `run-started`, `run-success`, `run-failure`, and `footer`. Supports GitHub context variables like `{workflow_name}` and `{run_url}`, plus individual AI cost and detection variables such as `{ai_model}`, `{ai_credits}`, `{ai_credits_formatted}`, `{agent_ai_credits_formatted}`, `{evals_ai_credits_formatted}`, `{threat_detection_ai_credits_formatted}`, `{detection_conclusion}`, and `{detection_reason}` for fine-grained cost and outcome attribution in custom footer templates. See [Footers Reference](/gh-aw/reference/footers/).

### Egress Context Validation (MCE1)

A safe-output handler safeguard, defined by Safe Outputs Specification requirement MCE1 ("Early Validation"), that checks for the required triggering context (a pull request, issue, or discussion number) *before* the handler writes its NDJSON entry. Tools that target the triggering entity implicitly — for example `close_pull_request` or `add_labels` called without an explicit `pull_request_number` or `issue_number` — need that context to resolve which item to act on. On `schedule` or `workflow_dispatch` runs, no triggering item exists, so without this check the tool call would pass validation but hard-fail later during output processing. Egress context validation surfaces an actionable error immediately, telling the agent to supply an explicit item number instead. Applied to `close_pull_request`, `merge_pull_request`, `mark_pull_request_as_ready_for_review`, `add_reviewer`, `reply_to_pull_request_review_comment`, `close_issue`, `add_labels`, `remove_labels`, `update_discussion`, and `close_discussion`. See [Safe Outputs Specification](/gh-aw/specs/safe-outputs-specification/).

### Failure Issue Reporting (`report-failure-as-issue:`)

A `safe-outputs` option that controls whether workflow failures are automatically reported as GitHub issues. Defaults to `true` when safe outputs are configured.

Use `false` to disable failure issues entirely, or provide a list of included and excluded categories. Entries prefixed with `!` are exclusions; if the list contains only exclusions, every other category is reported.

```yaml
safe-outputs:
  report-failure-as-issue:
    - agent_failure
    - missing_safe_outputs
    - "!inference_access_error"
```

Common categories include `agent_failure`, `timed_out`, `missing_safe_outputs`, `report_incomplete`, `missing_tool`, `missing_data`, `inference_access_error`, and `ai_credits_rate_limit_error`. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) for the full list.

### Report Failed Jobs (`report-failed-jobs:`)

A workflow-level control field under `safe-outputs:` that applies to safe-output processing as a whole rather than to an individual handler. Set `report-failed-jobs: false` to disable the automatic failed-job reporting issue that the framework otherwise creates when a job in the workflow fails. Defaults to `true` when omitted, distinct from [Failure Issue Reporting (`report-failure-as-issue:`)](#failure-issue-reporting-report-failure-as-issue), which controls category-based failure reporting for the agent job itself.

```yaml wrap
safe-outputs:
  create-issue:
    max: 1
  report-failed-jobs: false
```

See [Frontmatter Reference](/gh-aw/reference/frontmatter/).

### Failure Issue Repository (`failure-issue-repo:`)

A `safe-outputs` option that redirects failure tracking issues to a different repository. Useful when the workflow's repository has issues disabled:

```yaml
safe-outputs:
  failure-issue-repo: github/docs-engineering
```

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/).

### Action-Failure Issue Expiry (`action_failure_issue_expires`)

An `aw.json` governance setting, in hours, for the expiration of failure issues opened by the conclusion job (including grouped parent issues when `group-reports: true`). Setting `action_failure_issue_expires` explicitly is treated as opt-in: it causes `agentics-maintenance.yml` to be generated, if not already generated by another expiring safe output, so the scheduled `close-expired-entities` job can enforce the expiration. If left unset and no other workflow output requires scheduled maintenance, the implicit 168-hour (7-day) default is not written into failure issues, since no scheduled job exists to close them, and failure issues are created without an expiration marker. See [Ephemerals Reference](/gh-aw/reference/ephemerals/).

### Upload Assets

A safe output capability for uploading generated files (screenshots, charts, reports) to an orphaned git branch for persistent storage. The AI calls the `upload_asset` tool to register files, which are committed to a dedicated assets branch by a separate permission-controlled job. Assets are accessible via GitHub raw URLs. Commonly used for visual testing artifacts, data visualizations, and generated documentation.

### Base Branch

Configuration field in the `create-pull-request` safe output specifying which branch the pull request should target. Defaults to `github.base_ref || github.ref_name` if not specified. Useful for cross-repository pull requests targeting non-default branches.

### Minimize Comment

A safe output capability for hiding or minimizing GitHub comments without requiring write permissions. When minimized, comments are classified as SPAM. Requires GraphQL node IDs to identify comments. Useful for content moderation workflows.

### Hide Comment (`hide-comment:`)

A safe output capability that collapses comments, issues, pull requests, or discussion comments in the GitHub UI with a reason (`spam`, `abuse`, `off_topic`, `outdated`, `resolved`, or `low_quality`). Requires GraphQL node IDs rather than REST numeric IDs. Supports a `max` limit (default: 5), cross-repository targeting via `target-repo`, and an opt-in `discussions: true` field that requests the `discussions:write` permission needed to hide discussion comments — following the [least privilege](#least-privilege) principle by defaulting discussion support to off. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#hide-comment-hide-comment).

### Least Privilege

A security principle applied throughout gh-aw's safe-output design: features that require elevated permissions default to disabled and must be explicitly opted into (for example, `hide-comment.discussions: true` requesting `discussions:write`), rather than requesting the broadest permission set by default.

### Hide Older Comments (`hide-older-comments`)

A field on `add-comment:` safe outputs that minimizes previous comments before posting a new one. Accepts a boolean (`true`) or an object with `enabled` and `match` keys. The boolean form hides earlier comments from the same workflow only (identified by `GITHUB_WORKFLOW`). The object form adds a `match` list of additional workflow IDs whose older comments are also minimized — using exact full-string matching — so a single run can clean up comments from multiple related workflows.

```aw wrap
safe-outputs:
  add-comment:
    hide-older-comments:
      enabled: true
      match:
        - my-other-workflow
```

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#hide-older-comments).

### Add Labels (`add-labels:`)

A safe output capability for adding labels to issues or pull requests. Supports an `allowed` list to restrict which labels can be applied, and a `blocked` list using glob patterns to reject specific labels regardless of the allow list — providing protection against prompt injection via label manipulation. Accepts `target` (`"triggering"`, `"*"`, or a specific number), a `max` limit (default: 3), and cross-repository configuration via `target-repo`. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#add-labels-add-labels).

### Remove Labels (`remove-labels:`)

A safe output capability for removing labels from issues or pull requests. Supports `allowed` to restrict which labels can be removed and `blocked` to prevent removal of labels matching glob patterns. Silently skips labels not present on the target. Accepts per-target `issues` and `pull-requests` boolean fields (both default to `true`) to omit the corresponding write permission from the compiled workflow when a target type is not needed; disabling both is rejected at compile time. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#remove-labels-remove-labels).

### Assign to Agent

A safe output capability (`assign-to-agent:`) that programmatically assigns the GitHub Copilot coding agent to existing issues or pull requests. Automates the standard GitHub workflow for delegating implementation tasks to Copilot. Supports cross-repository PR creation via `pull-request-repo` and agent model selection via `model`. See [Copilot Cloud Agent](/gh-aw/reference/copilot-cloud-agent/).

### GH_AW_AGENT_TOKEN

A recognized "magic" repository secret name that GitHub Agentic Workflows automatically uses as a fallback Personal Access Token for `assign-to-agent` operations. When set, no explicit `github-token:` reference is needed in workflow frontmatter — the token is injected automatically. Required because GitHub App installation tokens are rejected by the Copilot assignment API. The token fallback chain is: `assign-to-agent.github-token` → `safe-outputs.github-token` → `GH_AW_AGENT_TOKEN` → `GH_AW_GITHUB_TOKEN` → `GITHUB_TOKEN`. See [Copilot Cloud Agent](/gh-aw/reference/copilot-cloud-agent/).

### GH_AW_GITHUB_TOKEN

A recognized "magic" repository secret name used as the default fallback token for GitHub operations outside engine inference when no explicit `github-token:` is configured. Commonly used for safe outputs and tool operations that require permissions beyond the default `GITHUB_TOKEN`, and as the non-App fallback when `github-app.ignore-if-missing: true` is enabled. Fallback chain: `custom github-token` → `GH_AW_GITHUB_TOKEN` → `GITHUB_TOKEN`. See [Authentication Reference](/gh-aw/reference/auth/).

### COPILOT_GITHUB_TOKEN

A repository secret name used to authenticate Copilot inference with a specific user's fine-grained Personal Access Token (PAT). Required in personal repositories or when centralized organization billing is unavailable. In `gh aw add-wizard`, the PAT setup flow auto-opens a preconfigured GitHub token creation page, but the token still must be created and confirmed in GitHub's browser UI. The activation job validates that this token is not a GitHub OAuth token (`gho_` prefix) — OAuth tokens are rejected with an actionable error because they cannot be scoped to a specific repository. When `permissions: copilot-requests: write` is set in the workflow, this secret is ignored for inference and `GITHUB_TOKEN` is used instead. See [Authentication Reference](/gh-aw/reference/auth/#copilot_github_token).

### Copilot Org Billing Opt-Out (`copilot-requests: none`)

A frontmatter setting, `permissions: copilot-requests: none`, that explicitly declines centralized organization billing for the Copilot engine. Without it, `gh aw compile` emits an informational tip on every compilation suggesting `copilot-requests: write` when the conditions for org billing are otherwise met; setting `none` silences that tip for workflows intentionally using individual/seat billing via `COPILOT_GITHUB_TOKEN`. See [Billing Reference](/gh-aw/reference/billing/).

### Per-Handler GitHub App Override (`safe-outputs.<type>.github-app`)

A type-specific `github-app:` field that lets an individual safe output handler mint and use a dedicated GitHub App installation token, scoped to only the permissions that handler requires, instead of sharing the global `safe-outputs.github-app` credential. When set, the compiler emits a dedicated token-minting step (`{handler-key}-app-token`) during workflow generation, and that handler uses the dedicated token in preference to the shared `safe-outputs.github-token` or global `safe-outputs.github-app` token. Handlers without an override continue to use the global `safe-outputs.github-app` fallback. Enables least-privilege separation between output types — for example, `add-comment` can use an App scoped to `issues:write` while `dispatch-workflow` uses a separate App scoped to `actions:write`. See [Safe Outputs Specification](/gh-aw/specs/safe-outputs-specification/#gp5a-type-specific-github-app-override).

### Custom Safe Outputs

An extension mechanism for safe outputs that enables integration with third-party services beyond built-in GitHub operations. Defined under `safe-outputs.jobs:`, custom safe outputs separate read and write operations: agents use read-only MCP tools for queries, while custom jobs execute write operations with secret access after agent completion. Supports services like Slack, Notion, Jira, or any external API. See [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/).

### Dispatch Repository (`dispatch-repository`)

An experimental safe output type that triggers `repository_dispatch` events in external repositories for cross-repository orchestration. Each key under `safe-outputs.dispatch-repository:` defines a named tool exposed to the agent. A tool requires a `workflow` identifier (forwarded in `client_payload` for routing), an `event_type`, and either a static `repository` slug or an `allowed_repositories` list. GitHub Actions expressions (`${{ ... }}`) are supported in repository fields and are passed through without format validation. At compile time the compiler emits a warning: `Using experimental feature: dispatch-repository`. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#repository-dispatch-dispatch-repository).

### Runner Group (`runs-on: {group: ...}`)

An object form of the `runs-on:` field that targets a named GitHub Actions runner group, optionally filtered by `labels:`. Supported by the top-level `runs-on`, `runs-on-slim`, `safe-outputs.runs-on`, `safe-outputs.threat-detection.runs-on`, and custom `safe-outputs.jobs.<job>.runs-on` fields, alongside the plain string and label-array forms. Useful for routing custom safe-jobs or framework jobs to a specific pool of self-hosted runners. See [Self-Hosted Runners Reference](/gh-aw/reference/self-hosted-runners/#runs-on-formats).

### Safe Output Actions

A mechanism for mounting any public GitHub Action as a once-callable MCP tool within the consolidated safe-outputs job. Defined under `safe-outputs.actions:`, each action is specified with a `uses` field (matching GitHub Actions syntax) and an optional `description` override. At compile time, `gh aw compile` fetches the action's `action.yml` to resolve its inputs and pins the reference to a specific SHA. Unlike [Custom Safe Outputs](#custom-safe-outputs) (separate jobs) and [Safe Output Scripts](#safe-output-scripts) (inline JavaScript), actions run as steps inside the safe-outputs job with full secret access via `env:`. Useful for reusing existing marketplace actions as agent tools. See [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/#github-action-wrappers-safe-outputsactions).

### Safe Output Scripts

Lightweight inline JavaScript handlers defined under `safe-outputs.scripts:` that execute inside the consolidated safe-outputs job handler loop. Unlike [Custom Safe Outputs](#custom-safe-outputs) (`safe-outputs.jobs`), which create a separate GitHub Actions job per tool call, scripts run in-process with no job scheduling overhead. Scripts do not have direct access to repository secrets, making them suitable for lightweight processing and logging. Each script declares `description`, `inputs`, and a `script` body; the compiler wraps the body and registers the handler as an MCP tool available to the agent. See [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/#inline-script-handlers-safe-outputsscripts).

### Safe Outputs Dependencies (`safe-outputs.needs:`)

A `safe-outputs` option that extends the consolidated `safe_outputs` job dependencies with custom workflow jobs. `safe-outputs.needs` is merged with built-in dependencies (`agent`, `activation`, optional `detection`, optional `unlock`) and deduplicated. Useful for injecting credential-fetching or secret-provisioning jobs that the safe-outputs job depends on. Values must reference custom jobs from the top-level `jobs:` section; built-in job names are rejected at compile time with an actionable error. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#safe-outputs-dependencies-needs).

### Unassign from User

A safe output capability for removing user assignments from issues or pull requests. Supports an `allowed` list to restrict which users can be unassigned, and a `blocked` list using glob patterns to prevent unassignment of specific users regardless of the allow list. Configured via `unassign-from-user:` in `safe-outputs`.

### URL Sanitization Policy (`safe-outputs.urls`)

A `safe-outputs` field that controls how URLs are sanitized in AI-generated content before GitHub writes are applied. Two values are supported:

- `allowed-only` (default) — sanitizes all URLs that are not in the `allowed-domains` list, everywhere in the content.
- `allowed-or-code-region` — preserves URLs inside fenced and inline code regions while still sanitizing URLs in prose.

Use `allowed-or-code-region` when workflow output includes code examples that legitimately reference external domains — for example, Docker image references or configuration snippets with third-party API endpoints.

```aw wrap
safe-outputs:
  urls: allowed-or-code-region
  allowed-domains: [registry.example.com]
  create-issue:
```

See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#text-sanitization-allowed-domains-allowed-github-references).

### Temporary ID

A workflow-scoped identifier (format: `aw_` followed by 3–8 alphanumeric characters, e.g. `aw_abc1`) that lets an AI agent reference a resource before it is created. Safe output tools that support temporary IDs — including `create_issue`, `create_discussion`, and `add_comment` — accept a `temporary_id` field. References like `#aw_abc1` in subsequent operations are automatically resolved to actual resource numbers during execution. Useful for creating interlinked resources in a single workflow run. Azure DevOps work-item safe outputs use the same pattern: `ado_create_work_item` returns a run-scoped `#aw_...` ID that other work-item tools accept, bypassing the consuming tool's `target` policy because creation was already scoped by trusted configuration. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/).

### Jira Safe Outputs (`jira-create-issue`, `jira-update-issue`, `jira-add-comment`, `jira-add-label`)

A set of safe outputs that call Jira Cloud REST API v3 from the privileged safe-output job; Jira credentials (`JIRA_BASE_URL`, `JIRA_USER_EMAIL`, `JIRA_API_TOKEN`) are configured under `safe-outputs.env` and are never exposed to the agent or included in `agent_output`. Each output accepts `max` and `staged`; in staged mode the handler writes a Jira-specific preview without requiring credentials or sending a request. Agent-provided descriptions and comment bodies are plain strings at the agent boundary; the runtime deterministically converts them to Atlassian Document Format (ADF) version 1, preserving paragraphs and line breaks. `jira_add_label` uses Jira's additive field-update operation and does not replace existing labels. See [Jira Safe Outputs](/gh-aw/reference/safe-outputs/#jira-safe-outputs).

### Linear Safe Outputs (`linear-create-issue`, `linear-add-comment`, `linear-update-issue`)

:::caution[Experimental]
Linear Safe Outputs are experimental. Compiling a workflow that enables any Linear safe output emits `Using experimental feature: Linear safe outputs`.
:::

A set of safe outputs that call Linear's public GraphQL API from the isolated safe-output job, using a personal API key configured via `safe-outputs.linear-token` (a secret expression not available to the agent). `linear-create-issue` requires a `team-id` (the Linear team model UUID); comment and update targets accept either a Linear issue model UUID or a shorthand identifier such as `ENG-123`, and are fixed trusted configuration. `linear-update-issue` replaces only the enabled `title` and `body` fields. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#linear-safe-outputs).

### Azure DevOps Work Items (`ado-create-work-item`, `ado-update-work-item`, `ado-comment-on-work-item`, `ado-assign-work-item`, `ado-link-work-items`, `ado-upload-workitem-attachment`)

:::caution[Experimental]
Azure DevOps work-item safe outputs are experimental. Compiling a workflow emits an experimental feature warning for each configured output.
:::

A set of safe outputs, using the same public tool names as [`ado-aw`](https://githubnext.github.io/ado-aw/reference/safe-outputs/), that perform trusted Azure DevOps REST requests from the privileged safe-output job while the agent remains read-only. The organization (`AZURE_DEVOPS_ORG_URL`), project (`SYSTEM_TEAMPROJECT`), and credential (`SYSTEM_ACCESSTOKEN` or `AZURE_DEVOPS_EXT_PAT`) are provided only to the safe-output job. Work items are scoped and targeted using [Area Path](#area-path) and [Iteration Path](#iteration-path) values; `ado-update-work-item` requires each mutable field to be explicitly enabled, `ado-assign-work-item` always rejects the reserved `Agency` and `GitHub Copilot` identities, and attachments are checked for traversal, symbolic links, size, extension, and Azure Pipelines command sequences before upload. See [Azure DevOps Work Items](/gh-aw/reference/safe-outputs/#azure-devops-work-items).

### Area Path

An Azure DevOps work-item field that organizes items in a hierarchical, backslash-separated path (e.g., `MyProject\Platform\Auth`). Used in Azure DevOps safe outputs to scope work-item creation (`area-path` on `ado-create-work-item`) and to target existing work items for updates, comments, assignments, and links. See [Azure DevOps Work Items](#azure-devops-work-items-ado-create-work-item-ado-update-work-item-ado-comment-on-work-item-ado-assign-work-item-ado-link-work-items-ado-upload-workitem-attachment).

### Iteration Path

An Azure DevOps work-item field that assigns items to a sprint or release cycle using a backslash-separated hierarchical path (e.g., `MyProject\Sprint 42`). Used alongside [Area Path](#area-path) to scope and validate Azure DevOps safe-output targets. See [Azure DevOps Work Items](#azure-devops-work-items-ado-create-work-item-ado-update-work-item-ado-comment-on-work-item-ado-assign-work-item-ado-link-work-items-ado-upload-workitem-attachment).

### Approve Workflow Run (`approve-workflow-run:`)

An experimental safe output capability that approves a GitHub Actions workflow run in the "action required" state, such as runs for fork pull requests or pull requests created by Copilot. The agent supplies a positive integer `run_id`; the handler verifies the run is a pull request run tied to an authorized pull request and is still awaiting approval (status `action_required` or `waiting`, or conclusion `action_required`) before calling GitHub's approval API. Requires `actions: write` and an explicit external `github-token` or `github-app`, since the default `github.token` cannot approve workflow runs; `pull-requests: write` is required when `comment` is enabled (the default), otherwise `pull-requests: read` suffices. `allowed-workflows` is required and restricts approval to matching workflow filenames; `allowed-pull-requests` extends authorization beyond the triggering pull request. Only pull requests whose head branch lives in the current repository (including agent-initiated `copilot/*` pull requests) are approvable by default; pull requests from other repositories are refused unless their repository matches an `allowed-repos` entry. The safe output always refuses to run from `pull_request_target`. After a successful approval, a comment announcing the run has started (with a generated attribution footer) is posted on each associated pull request unless `comment: false` is set. Compiling a workflow with `approve-workflow-run` emits an experimental feature warning. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#approve-workflow-run-approve-workflow-run).

### Merge Pull Request (`merge-pull-request:`)

An experimental safe output capability for merging pull requests after policy-driven gate checks pass. Validates status checks, required approvals, resolved review threads, label and branch constraints, and GitHub mergeability before applying the merge. Merges to the repository default branch are always refused. Supports `merge`, `squash`, and `rebase` methods, cross-repository targets, and `staged: true` for dry-run gate evaluation without calling the GitHub merge API. Compiling a workflow with `merge-pull-request` emits an experimental feature warning. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#merge-pull-request-merge-pull-request) and the [Safe Outputs Specification](/gh-aw/specs/safe-outputs-specification/#type-merge_pull_request).

### Close Pull Request (`close-pull-request:`)

A safe output capability for closing pull requests without merging, with an optional comment. Supports filtering via `required-labels` and `required-title-prefix` to prevent unintended closures. Accepts `target` to identify the PR (`"triggering"`, `"*"`, or a specific number), cross-repository configuration via `target-repo`, and a `max` limit on closures. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#close-pull-request-close-pull-request).

### Update Issue

A safe output capability (`update-issue:`) for modifying existing issues without creating new ones. Each updatable field (`status`, `title`, `body`) must be explicitly enabled. Body updates accept an `operation` field: `append` (default), `prepend`, `replace`, or `replace-island` (updates a specific section delimited by HTML comments). Supports cross-repository issue updates. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#issue-updates-update-issue).

### Update Pull Request (`update-pull-request:`)

A safe output capability for modifying a pull request's `title` or `body`. Title and body updates are enabled by default unless explicitly set to `false`. The `operation` field controls how body changes are applied: `replace` (default), `append`, `prepend`, or `replace-island` (updates a run-specific section delimited by HTML comments). Accepts `target` (`"triggering"`, `"*"`, or a specific number) and cross-repository updates via `target-repo`. When `target: "*"` is used, the agent must supply `pull_request_number` in the tool output. The optional `update-branch: true` field synchronizes the PR branch with the latest base branch changes before applying other updates. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#pull-request-updates-update-pull-request).

### Protected Files

A security mechanism on `create-pull-request` and `push-to-pull-request-branch` safe outputs that prevents AI agents from modifying sensitive repository files. By default, protects dependency manifests (e.g., `package.json`, `go.mod`), GitHub Actions workflow files, and lock files. Configured via `protected-files:` with three policies: `blocked` (default — fails with error), `allowed` (no restriction), or `fallback-to-issue` (creates a review issue for human inspection instead of applying changes). Also accepts an object form `{ policy: string, exclude: [...] }` to remove specific files or path prefixes from the default protected set while keeping protection active for the remaining files. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#protected-files).

### Allow Workflows (`allow-workflows:`)

A field on `create-pull-request` and `push-to-pull-request-branch` safe outputs that adds `workflows: write` to the GitHub App token's permissions. Required when `allowed-files:` targets paths under `.github/workflows/`, because the `workflows` permission is a GitHub App-only permission that cannot be granted via `GITHUB_TOKEN`. Requires a `safe-outputs.github-app` configuration — the compiler rejects `allow-workflows: true` without one. This opt-in design keeps the elevated permission visible and auditable in the workflow source. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#allowing-workflow-file-changes-with-allow-workflows).

### Allowed Events (`allowed-events:`)

A field on `submit-pull-request-review:` safe outputs that restricts which PR review event types the agent may submit. Accepts an array of `APPROVE`, `COMMENT`, and `REQUEST_CHANGES`. When set, the safe-outputs handler rejects any review event not in the list, providing infrastructure-level enforcement regardless of what the agent attempts to output. If omitted, all three event types are allowed. Preferred default for bot reviews: `allowed-events: [COMMENT]`. Example: `allowed-events: [COMMENT, REQUEST_CHANGES]` prevents the agent from approving PRs. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#submit-pr-review-submit-pull-request-review).

### Supersede Older Reviews (`supersede-older-reviews:`)

A field on `submit-pull-request-review:` safe outputs that dismisses older `REQUEST_CHANGES` reviews from the same workflow after posting a replacement review. When `supersede-older-reviews: true` is set, the safe-output handler fetches recent reviews, identifies prior `REQUEST_CHANGES` reviews submitted by the same workflow call, and dismisses them before the new review takes effect. This is best-effort behavior — dismissal failures do not block the new review. Useful when a workflow is configured with `allowed-events: [REQUEST_CHANGES]` and repeated runs would otherwise accumulate blocking reviews. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#submit-pr-review-submit-pull-request-review).

### Deduplicate by Title (`deduplicate-by-title:`)

A `create-issue` safe-output field that drops duplicate issues before creation by comparing titles. Accepts `true` for exact matching (after normalization) or an integer `0`–`100` for fuzzy matching within the given Levenshtein edit distance (e.g., `1` allows one-character differences). Deduplication runs at MCP tool-call time (within-run) and at apply time (against open and recently-closed repository issues). Dropped items are recorded in the safe-output summary with the matched title, edit distance, and source. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#issue-creation-create-issue).

### Allowed Fields (`create-issue:`)

A configuration field on `create-issue:` safe outputs that restricts which GitHub Project custom fields the agent may set when creating issues. Accepts an array of field names (e.g., `[Priority, Iteration]`). When set, the safe-outputs handler rejects any attempt to populate a field not in the list. When omitted, all project fields are permitted. Example: `allowed-fields: [Priority, Iteration]`. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#issue-creation-create-issue).

### Blocked-By Dependency (`blocked_by`)

An optional field on the `create_issue` safe output that declares a dependency on another issue. It accepts an issue number, a Temporary ID, an `owner/repo#number` reference, a GitHub issue URL, or a list of these. Temporary IDs are resolved before the referenced issue exists, so dependent output can be emitted in any order within a single agentic run. Attaching the dependency is best-effort: if the underlying GitHub API call fails, the issue is still reported as created and the failure is logged as a warning rather than failing the workflow. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#issue-creation-create-issue).

### Allowed Files

An exclusive allowlist for `create-pull-request` and `push-to-pull-request-branch` safe outputs. When `allowed-files:` is set to a list of glob patterns, **only** files matching those patterns may be modified — every other file (including normal source files) is refused. This is a restriction, not an exception: listing `.github/workflows/*` does not additionally allow normal source files; it blocks them. Runs independently from [Protected Files](#protected-files): both checks must pass. To modify a protected file, it must both match `allowed-files` and have `protected-files: allowed`. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#restricting-changes-to-specific-files-with-allowed-files).

### Branch Prefix (`branch-prefix:`)

An optional field on `create-pull-request` safe outputs that prepends a fixed string to the agent-specified or auto-generated branch name. Useful when repository policies require branches to follow naming conventions (e.g., `signed/` for signed-commit workflows). The default prefix is `signed/`. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/).

### Preserve Branch Name (`preserve-branch-name:`)

An option on `create-pull-request` safe outputs that omits the random hex salt suffix normally appended to the agent-specified branch name. Useful when the target repository enforces naming conventions such as Jira keys in uppercase (for example, `bugfix/BR-329-red` instead of `bugfix/br-329-red-cde2a954`). Invalid characters are always replaced for safety, and casing is always preserved regardless of this setting. Defaults to `false`. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/).

### Max Patch Files (`max-patch-files:`)

A `create-pull-request` safe-output field that sets the maximum number of unique files allowed in a single PR's patch. Defaults to `100`. Workflows that regenerate large sets of files (e.g., per-package API schemas) can raise this limit. If the limit is exceeded, PR creation fails with an actionable error showing the exact file count and the field to configure. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/).

### Max Patch Size (`max-patch-size:`)

A `create-pull-request` and `push-to-pull-request-branch` safe output field that limits the total size of the git patch in kilobytes. Accepts an integer in the range 1–10,240 KB. Defaults to `4096` KB (4 MB). If the patch exceeds the limit, PR creation fails with an actionable error. Useful when workflows generate large diffs and the default limit is too restrictive or too permissive. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/).

### Implausible Shallow Range Guard

A safety check in `push-to-pull-request-branch` that detects when a shallow clone (`fetch-depth: 1`) causes `git rev-list` to report the entire local history as the commit range instead of just the new commits — for example, tens of thousands of commits on a branch with a single new commit. This happens because a shallow checkout gives `origin/<branch>` no traversable ancestry. When the reported range exceeds a threshold (100 commits) in a shallow checkout, merge-commit detection returns `false` with a warning rather than risk selecting the wrong push transport; if the range still reaches the signed-push linearization step, that step throws and refuses to proceed. Set `fetch-depth: 0` in `checkout:` to give the transport-selection logic an accurate commit range. See [Checkout Reference](/gh-aw/reference/checkout/#git-credentials-after-checkout).

### Recreate Ref (`recreate-ref:`)

An option on `create-pull-request` safe outputs that force-deletes and recreates the remote branch when the agent-supplied branch name already exists on the remote. Requires `preserve-branch-name: true`. The handler force-pushes the agent's local HEAD to the stale remote ref, enabling reuse of long-lived reusable branches whose previous PR was merged. Without `recreate-ref: true`, the default behavior is to fall back (for example, open an issue when `fallback-as-issue: true`) rather than overwrite the remote. Defaults to `false`. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/).

### Create Pull Request Review Comment (`create-pull-request-review-comment:`)

A safe output capability for posting inline review comments on specific lines in a pull request diff. Supports single-line and multi-line comments with configurable `side` (`LEFT` or `RIGHT`). When `target: "*"` is set, the agent must supply `pull_request_number` in the tool call. For cross-repository scenarios, the agent may also supply `repo` (in `owner/repo` format) matching `target-repo` or `allowed-repos`. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#pr-review-comments-create-pull-request-review-comment).

### Reply to PR Review Comment (`reply-to-pull-request-review-comment:`)

A safe output capability for replying to existing review comments on pull requests. Allows the AI agent to respond to reviewer feedback, answer questions, or acknowledge inline review comments by their numeric comment ID. Supports an optional `footer` field (`always`, `none`, or `if-body`) to control AI attribution. Configured via `reply-to-pull-request-review-comment:` in `safe-outputs`. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#reply-to-pr-review-comment-reply-to-pull-request-review-comment).

### Resolve PR Review Thread (`resolve-pull-request-review-thread:`)

A safe output capability for marking GitHub PR review threads as resolved. Uses the GitHub GraphQL `resolveReviewThread` mutation, requiring the thread's node ID. Allows AI agents to clean up addressed review comments after implementing feedback. Accepts the same `target`, `target-repo`, and `allowed-repos` options as other pull-request safe outputs. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#resolve-pr-review-thread-resolve-pull-request-review-thread).

### Dismiss Pull Request Review (`dismiss-pull-request-review:`)

A safe output capability for dismissing existing pull request reviews authored by the workflow actor. Configured via `dismiss-pull-request-review:` in `safe-outputs`. Also accepts the legacy alias `dismiss-review:`. Supports the standard target, filter, and base safe-output fields: `target` (`"triggering"`, `"*"`, or a specific PR number), `target-repo`, `allowed-repos`, `required-labels`, `required-title-prefix`, and `max` (default 10). Useful for clearing stale `REQUEST_CHANGES` reviews before submitting a replacement, or for workflows that programmatically manage the PR review lifecycle. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/).

### Report Incomplete (`report_incomplete`)

A mandatory safe output signal that agents emit when a task cannot be completed due to an infrastructure or tool failure — for example, an MCP server crash, missing authentication, or an inaccessible repository. Unlike `noop` (which signals no action was needed), `report_incomplete` indicates an active failure that prevented the task from running. The safe-outputs handler activates failure handling regardless of agent exit code. Accepts a required `reason` field (max 1024 characters) and an optional `details` field for extended diagnostic context.

### Review Attribution Pinning (`GH_AW_HEAD_SHA`)

A compiler-injected environment variable that anchors PR review safe outputs to the commit the agent actually evaluated. For `workflow_run` triggers, it is set to `${{ github.event.workflow_run.head_sha }}`; for `pull_request` and `pull_request_target` triggers, to `${{ github.event.pull_request.head.sha }}`. The safe-outputs runtime passes this value as `commit_id` when submitting a review, preventing attribution drift if a newer commit lands on the pull request while the workflow is still running. Requires no user configuration. See [Safe Outputs Specification](/gh-aw/specs/safe-outputs-specification/).

### Set Issue Type (`set-issue-type:`)

A safe output capability for setting or clearing the GitHub issue type on existing issues. The agent calls `set_issue_type` to assign a named type (e.g., `Bug`, `Feature`) to an issue. An `allowed` list restricts which types the agent may set; omitting it permits any type. Passing an empty string clears the current type. Supports cross-repository targeting via `target-repo` and `allowed-repos`. Configured via `set-issue-type:` in `safe-outputs`.

### Set Issue Field (`set-issue-field:`)

A safe output capability for setting one issue field value on existing issues. The agent calls `set_issue_field` with `value` and either `field_name` (for discovery by field label) or `field_node_id` (to skip discovery). Unknown field names return actionable errors listing available fields and suggesting explicit IDs. Supports optional `allowed-fields` restrictions (including `["*"]` wildcard) and cross-repository targeting via `target-repo` and `allowed-repos`. Configured via `set-issue-field:` in `safe-outputs`.

### Parameterized Safe-Output Fields

A pattern for `workflow_call` reuse where safe-output policy and list fields accept GitHub Actions expression strings (e.g., `${{ inputs.protected-files-policy }}`) in addition to literal values. At compile time the compiler detects the `${{...}}` form and passes it through unchanged; GitHub Actions evaluates the expression at runtime before the handler executes. Enum-valued policy fields such as `protected-files` and `patch-format` validate literal values at compile time but defer expression-based values to runtime (failing closed on unrecognized input). List-valued fields such as `labels`, `allowed-repos`, and `allowed-base-branches` accept either a YAML array or a single expression string. This enables a single reusable workflow to serve callers with different constraint configurations without duplicating files. See [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#parameterizing-policy-fields-in-reusable-workflows).

### Normalize Closing Keywords (`normalize-closing-keywords:`)

A field available on `create-issue:`, `add-comment:`, and `create-pull-request:` safe outputs that strips wrapping backticks from recognized GitHub issue-closing keywords in body text. For example, `` `Closes #123` `` becomes `Closes #123` so GitHub can process it as a closing reference. Useful when AI-generated body text inadvertently wraps closing keywords in inline code formatting. Set `normalize-closing-keywords: true` to enable. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#normalize-closing-keywords) and [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/).

### Mention Filtering (`mentions:`)

A `safe-outputs` section that controls how `@mentions` in AI-generated content are handled before GitHub writes are applied. By default, mentions of repository collaborators and event-context participants are allowed, and other usernames are escaped with backticks. Set `mentions: false` to escape all mentions.

Under `safe-outputs.mentions`, `allowed-collaborators` (deprecated alias: `allow-team-members`) and `allow-context` both default to `true`; `allowed` adds specific users or bots; `allowed-teams` adds team members (requires `read:org`); and `max` caps unescaped mentions per message (default `50`). Use `gh aw fix mentions-allow-team-members-to-allowed-collaborators` to migrate the deprecated alias. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/#mention-filtering-mentions).

```aw wrap
safe-outputs:
  mentions:
    allowed-collaborators: true
    allowed-teams:
      - myorg/eng
    max: 50
  add-comment: {}
```

## Workflow Components

### Activation Token (`on.github-token:`, `on.github-app:`)

Custom GitHub token or GitHub App used by the activation job to post reactions and status comments on the triggering item. Configured via `github-token:` (for a PAT or token expression) or `github-app:` (to mint a short-lived installation token) inside the `on:` section. Affects only the activation job — agent job tokens are configured separately via `tools.github.github-token` or `safe-outputs.github-app`. See [Authentication Reference](/gh-aw/reference/auth/).

### Ambient Folders (`ambient-folders:`)

Top-level, workspace-relative folders (for example `.squad/`, `.github/agents`) declared via the `ambient-folders:` frontmatter field that are bundled into the activation artifact and restored into the checkout before the agent runs. Enables shared workflows to prepare reusable prompt, skill, or agent context without requiring per-consumer manual artifact handling. Shared workflow components (files without a trigger event) may also declare `ambient-folders` for reuse through imports. See [Frontmatter Reference](/gh-aw/reference/frontmatter/#ambient-folders-ambient-folders).

```yaml wrap
ambient-folders:
  - .squad
  - .github/agents
```

### Bare Mode (`engine.bare`)

An engine configuration field that disables automatic loading of context and custom instructions by the engine. Set `engine.bare: true` to prevent the engine from reading memory files, `AGENTS.md`, `CLAUDE.md`, or built-in system prompts that would otherwise be loaded automatically. Useful for triage, reporting, and ops workflows where the prompt is fully self-contained and repository code context adds noise. Supported by Copilot, Claude, Codex, and Gemini engines. See [AI Engines Reference](/gh-aw/reference/engines/#bare-mode-bare).

```aw wrap
engine:
  id: copilot
  bare: true
```

### Built-in Job Needs Augmentation (`jobs.<built-in>.needs`)

A frontmatter field that adds explicit dependency ordering for compiler-generated jobs (`activation`, `agent`, `detection`, `safe_outputs`, `conclusion`). When `jobs.<built-in>.needs` is set, the listed jobs are merged additively into the compiler's computed dependency list for that built-in job — compiler-required dependencies are never removed. Use this when implicit `engine.env` inference does not detect a dependency, for example when a custom job's output is consumed inside a built-in job's configuration via `${{ jobs.my_job.outputs.value }}`. Both hyphen and underscore forms are accepted (`safe-outputs` or `safe_outputs`). Validation at compile time rejects unknown targets, self-dependencies, and augmentation of built-in jobs not present in the current workflow. See [Custom Steps and Jobs Reference](/gh-aw/reference/steps-jobs/).

```aw wrap
jobs:
  select_model:
    runs-on: ubuntu-latest
    outputs:
      model: ${{ steps.pick.outputs.model }}
    steps:
      - id: pick
        run: echo "model=gpt-4o" >> $GITHUB_OUTPUT
  agent:
    needs: [select_model]
```

### BYOK (Bring Your Own Key)

A Copilot engine mode that routes AI requests to an external LLM provider (such as OpenAI, Anthropic, or a self-hosted Ollama/vLLM instance) instead of the default GitHub Copilot backend. Activated by setting `COPILOT_PROVIDER_BASE_URL` in `engine.env`. The three BYOK credential variables (`COPILOT_PROVIDER_BASE_URL`, `COPILOT_PROVIDER_API_KEY`, `COPILOT_PROVIDER_BEARER_TOKEN`) accept `${{ secrets.* }}` references under strict mode and are never exposed to the agent container. gh-aw automatically adds the provider hostname to the AWF allow-list when it can resolve a literal BYOK URL, and otherwise reuses the concrete provider hostname from `network.allowed` for the threat-detection Copilot step. Use `COPILOT_MODEL` to specify the target model. See [AI Engines Reference](/gh-aw/reference/engines/#copilot-bring-your-own-key-byok-mode).

### Cron Schedule

A time-based trigger format. Use short syntax like `daily` or `weekly on monday` (recommended with automatic time scattering) or standard cron expressions for fixed times. Cron-based schedule items accept an optional `timezone` field with any [IANA timezone identifier](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones) (e.g., `America/New_York`) to interpret the expression in a specific timezone instead of UTC. See also [Fuzzy Scheduling](#fuzzy-scheduling) and [Time Scattering](#time-scattering).

### Ecosystem Identifiers

Named shorthand references to predefined domain sets used in `network.allowed` and `safe-outputs.allowed-domains`. Instead of listing individual domain names, ecosystem identifiers expand to curated sets for a language runtime or service category. Common identifiers: `python` (PyPI/pip), `node` (npm), `go` (proxy.golang.org), `github` (GitHub domains), `dev-tools` (CI/CD services such as Codecov, Snyk, Shields.io), `local` (loopback addresses), and `default-safe-outputs` (a compound set combining `defaults` + `dev-tools` + `github` + `local`, recommended as a baseline for `safe-outputs.allowed-domains`). See [Network Permissions Reference](/gh-aw/reference/network/#ecosystem-identifiers).

### Copilot SDK (`engine.copilot-sdk`)

An engine option that enables the Copilot engine to run in SDK mode, giving the workflow direct access to the Copilot SDK runtime for advanced integration patterns such as inline sub-agents. Set `engine.copilot-sdk: true` to activate, or set `engine.driver` on the Copilot engine to enable SDK mode automatically while replacing the built-in driver. Supports `max-tool-denials` to stop inference when tool requests are denied too frequently. See [AI Engines Reference](/gh-aw/reference/engines/#copilot-sdk-support).

```aw wrap
engine:
  id: copilot
  copilot-sdk: true
```

### Engine

An **AI engine** is the runtime and provider integration used to execute the AI agent. GitHub Agentic Workflows has four stable built-in engines—**GitHub Copilot** (default), **Claude Code**, **OpenAI Codex**, and **Google Gemini**—plus **Pi**. Set `engine:` in frontmatter to choose one; omit it to use Copilot. See [AI Engines for GitHub Agentic Workflows](/gh-aw/reference/engines/).

### Engine Version (`engine.version`)

An `engine:` field that pins the installed CLI version for the selected engine. Defaults to `latest` when omitted. Accepts a literal version string or a GitHub Actions expression (e.g., `${{ inputs.engine-version }}`) so `workflow_call` reusable workflows can parameterize the version via caller inputs. Supported consistently across Copilot, Claude, Codex, Gemini, and Pi. Pin a version for reproducible builds or to avoid breakage from new CLI releases.

```yaml wrap
engine:
  id: copilot
  version: "0.0.422"
```

### Unsupported Engine Samples

Sample CLI engine definitions bundled as reference patterns rather than officially supported engines. The repository ships **OpenCode**, **Aider**, **Crush**, **Cursor**, and **Kiro** as sample [engine behaviors](#engine-behaviors-enginebehaviors) definitions under `.github/workflows/shared/<id>.md`. These have no compatibility or maintenance commitment from `gh-aw`; copy or adapt them only under the support terms of their respective owners. See [AI Engines Reference](/gh-aw/reference/engines/#unsupported-engine-samples).

See [AI Engines Reference](/gh-aw/reference/engines/#pinning-a-specific-engine-version).

### Stall Watchdog (`GH_AW_HARNESS_STALL_WARNING_MS`)

A driver-level watchdog shared by all built-in harnesses that logs a warning when the agent CLI produces no stdout or stderr output for a configured interval, without terminating the process. Distinct from the [Post-Result Watchdog](#post-result-watchdog-engineharnesswatchdog-timeout), which only arms after a terminal safe output and can terminate a quiet process. Configure with the `GH_AW_HARNESS_STALL_WARNING_MS` environment variable (milliseconds, default `300000` — 5 minutes, min `1000`, max `3600000`); unset or non-numeric values use the default, and zero or negative values disable the warnings. See [Environment Variables Reference](/gh-aw/reference/environment-variables/).

### Post-Result Watchdog (`engine.harness.watchdog-timeout`)

A harness setting that terminates a quiet child process after the agent has already produced a terminal safe output, such as `noop` or an ordinary task output (comment, label, push, pull request creation). Diagnostic safe outputs like `missing_tool`, `missing_data`, and `report_incomplete` are not terminal and do not arm the watchdog. The watchdog stays dormant until armed, and once armed, any stdout or stderr activity resets its inactivity clock — so a quiet process doing useful work can still be terminated, with the harness treating that termination as successful because the requested safe output already exists. Configure with `engine.harness.watchdog-timeout` (seconds) or the raw `GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS` environment variable (milliseconds, default `120000`, min `50`, max `600000`). See [Harness Retry and Post-result Watchdog Policy](/gh-aw/reference/engines/#harness-retry-and-post-result-watchdog-policy) and [Environment Variables Reference](/gh-aw/reference/environment-variables/#shared-post-result-watchdog).

### Harness Retry Runner

A shared retry module used by the built-in Copilot, Claude, and Codex engine harnesses to re-launch the agent CLI with exponential backoff after a failed or interrupted run. Configurable via `engine.harness` fields (or `GH_AW_HARNESS_*` environment variables) for retry count and backoff multiplier/max-delay. Extracted as a common component so all harnesses share the same retry behavior instead of duplicating it per engine. See [AI Engines Reference](/gh-aw/reference/engines/#harness-retry-and-post-result-watchdog-policy).

### Anthropic Workload Identity Federation (WIF)

A keyless authentication method for the Claude engine that uses short-lived GitHub OIDC tokens instead of a long-lived `ANTHROPIC_API_KEY` secret. Configured via [`engine.auth`](#engine-auth-engineauth) with `type: github-oidc` and `provider: anthropic`, along with Anthropic-specific IDs (`federation-rule-id`, `organization-id`, `service-account-id`, `workspace-id`) obtained from the Anthropic Console. Requires `permissions: id-token: write`. Available since v0.79.6. See [Authentication Reference](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif).

### GitHub-hosted Inference (Codex)

An OpenAI Codex engine mode that routes model calls through the GitHub inference gateway instead of a direct OpenAI provider. Enabled by prefixing the top-level `model:` with `copilot/`; select a Codex-capable model such as `model: copilot/gpt-5.3-codex` because general-purpose models may not support the capabilities that Codex requires. The compiler configures Codex's BYOK provider to use the gateway and forwards the model name without the prefix. Requires the default agent sandbox and authenticates via `permissions: { copilot-requests: write }` (recommended) or `COPILOT_GITHUB_TOKEN`. See [Codex Engine](/gh-aw/engines/codex/).

### Engine Auth (`engine.auth`)

An `engine:` configuration field for keyless authentication. When `engine.auth` is configured, the compiler emits authentication environment variables consumed by the AWF api-proxy sidecar and suppresses the static API key requirement. Supports `type: github-oidc` with an optional `provider` for service-specific token exchange — for example, `provider: anthropic` for [Anthropic WIF](#anthropic-workload-identity-federation-wif) or without a provider for Azure Entra ID BYOK mode. Requires `permissions: id-token: write` in the workflow. See [Authentication Reference](/gh-aw/reference/auth/) and [AI Engines Reference](/gh-aw/reference/engines/).

```aw wrap
engine:
  id: claude
  auth:
    type: github-oidc
    provider: anthropic
    federation-rule-id: fdrl_xxxxxxxxxxxx
    organization-id: org_xxxxxxxxxxxx
    service-account-id: svac_xxxxxxxxxxxx
    workspace-id: ws_xxxxxxxxxxxx
```

### Engine Permission Mode (`engine.permission-mode`)

A first-class Claude engine setting that controls how Claude Code enforces tool access boundaries. Accepts one of four values: `acceptEdits` (default — Claude honors `--allowed-tools`; the workflow's declared `tools:` and `mcp-servers: allowed:` list is the effective tool boundary), `bypassPermissions` (Claude ignores `--allowed-tools`; the MCP gateway's `allowed:` filter becomes the sole boundary), `auto` (Claude selects the least-privileged mode that fits the workflow's tool configuration; the default when `tools.edit: false`), and `plan` (Claude presents changes for approval before applying them).

Previously, `bypassPermissions` was derived implicitly whenever a workflow granted unrestricted bash access (`bash: "*"`, `bash: [":*"]`, or `bash: null`), which could silently disable `--allowed-tools` enforcement. Setting `engine.permission-mode` explicitly overrides that implicit derivation and any legacy `--permission-mode` flag in `engine.args`. The compiler validates the value against the fixed enum at compile time. See [AI Engines Reference](/gh-aw/reference/engines/#claude-tool-enforcement-security-model).

```aw wrap
engine:
  id: claude
  permission-mode: acceptEdits
```

### Enterprise API Endpoint (`api-target`)

An `engine` configuration field specifying a custom API endpoint hostname for GitHub Enterprise Cloud (GHEC) or GitHub Enterprise Server (GHES) deployments. When set, the compiler automatically adds both the API domain and the base hostname to the AWF firewall `--allow-domains` list and the `GH_AW_ALLOWED_DOMAINS` environment variable, eliminating the need for manual network configuration after each recompile. The value must be a hostname only — no protocol or path (e.g., `api.acme.ghe.com`). See [Engines Reference](/gh-aw/reference/engines/#enterprise-api-endpoint-api-target).

```aw wrap
engine:
  id: copilot
  api-target: api.acme.ghe.com
```

### Inline Sub-Agents

Named agent definitions embedded directly in a workflow markdown file, without requiring a separate file in `.github/agents/`. Each sub-agent block starts with a `## agent: \`name\`` heading, contains optional YAML frontmatter (for model selection and a description), and ends at a matching `## end agent: \`name\`` marker if present, or otherwise at the next `##` heading or end of file. At compile time, inline sub-agent blocks are extracted to locations the engine can access natively. Supported for the Copilot engine. Sub-agent names must start with a lowercase letter and may only contain `a–z`, `0–9`, `_`, and `-`. See [Inline Sub-Agents Reference](/gh-aw/reference/inline-sub-agents/).

```aw wrap
engine:
  id: copilot
  copilot-sdk: true
---

## agent: `file-summarizer`
---
model: small
description: Summarizes file contents briefly
---
You are a file summarization assistant.
```

### End Marker (`## end agent:`, `## end skill:`)

Optional explicit syntax that closes an inline sub-agent or inline skill block at a precise point, instead of relying on the implicit boundary of the next `##` heading or end of file. Written as `` ## end agent: `name` `` or `` ## end skill: `name` ``, matching the opening heading's name. Recommended when a block's body legitimately needs `##`-level headings of its own, or when content follows the block in the same file. When a sub-agent or skill block is brought in via [Runtime Import](#runtime-import-runtime-import) and has no explicit end marker, the import resolver automatically inserts one at the implicit boundary, making every runtime import import-safe by default. See [Inline Sub-Agents Reference](/gh-aw/reference/inline-sub-agents/).

### Inline Engine Definition

An engine configuration format that specifies a runtime adapter and optional provider settings directly in workflow frontmatter, without requiring a named catalog entry. Uses a `runtime` object (with `id` and optional `version`) to identify the adapter and an optional `provider` object for model selection, authentication, and request shaping. Useful for connecting to self-hosted or third-party AI backends.

```aw wrap
engine:
  runtime:
    id: codex
  provider:
    id: azure-openai
    model: gpt-4o
    auth:
      strategy: oauth-client-credentials
      token-url: https://auth.example.com/oauth/token
      client-id: AZURE_CLIENT_ID
      client-secret: AZURE_CLIENT_SECRET
    request:
      path-template: /openai/deployments/{model}/chat/completions
      query:
        api-version: "2024-10-01-preview"
```

See [Engines Reference](/gh-aw/reference/engines/).

### Pi Provider Prefix (`model:`)

The provider segment of a Pi engine `model:` value (for example `copilot/gpt-5.4`, `anthropic/claude-sonnet-5`, or `openai/gpt-5`) that Pi uses to select an authentication method. Pi defaults to Copilot authentication, uses `ANTHROPIC_API_KEY` for `anthropic/...` models, and uses `CODEX_API_KEY` or `OPENAI_API_KEY` for `openai/...` and `codex/...` models. See [Quick Start](/gh-aw/setup/quick-start/).

### Pi Extensions (`engine.extensions`)

A Pi engine configuration field that loads additional plugins via `pi install <extension>` before the agent runs. Each entry is an npm package name. Only the Pi engine reads this field; other engines ignore it. Each listed extension produces one additional install step in the compiled workflow.

```aw wrap
engine:
  id: pi
  extensions:
    - "@pi/web-search"
    - "@pi/file-browser"
```

See [AI Engines Reference](/gh-aw/reference/engines/).

### Agent Plugins (`plugins:`)

An experimental top-level frontmatter field that installs [Agent Plugins](https://agent-plugins.org) through the selected agentic engine. Each entry identifies a GitHub repository and, optionally, a path to a plugin within it; `gh aw compile` resolves and pins each reference to a specific commit SHA so a moving branch or tag ref cannot silently change what gets installed at run time. Supported by Copilot, Claude, Codex, and any imported engine definition that declares a `behaviors.plugins` block (such as the shared Cursor and Kiro engines); using `plugins:` with an unsupported engine is a compile-time error. Compiling a workflow that uses `plugins:` emits a warning because the feature is experimental. See [Frontmatter Reference](/gh-aw/reference/frontmatter/#agent-plugins-plugins).

### Custom Provider (`OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL`)

A deployment pattern where the `OPENAI_BASE_URL` or `ANTHROPIC_BASE_URL` environment variable routes an engine's API calls to a non-default, OpenAI- or Anthropic-compatible endpoint. When set, gh-aw passes the configured model through verbatim and automatically emits `apiProxy.modelFallback.enabled: false` so the API proxy does not rewrite provider-specific model slugs (for example `anthropic/claude-sonnet-5` on OpenRouter) that are absent from the built-in model catalog, which would otherwise cause an HTTP 404 `model_not_found` error. See [`sandbox.agent.model-fallback`](#sandboxagentruntime) and [AI Engines Reference](/gh-aw/reference/engines/).

### Engine Driver (`engine.driver`)

An engine configuration field that replaces the built-in runtime entrypoint for engines that support driver mode. For the Pi engine, `gh aw` launches the driver with Node.js instead of the `pi` CLI; the driver must emit JSONL compatible with the existing log parser so step summaries and token tracking work unchanged. For the Copilot engine, setting `engine.driver` replaces the built-in SDK driver and enables `engine.copilot-sdk` automatically. A bare filename (e.g. `pi_agent_core_driver.cjs`) resolves from the gh-aw setup-action directory; a path containing `/` resolves workspace-relative. Copilot also accepts inline source objects with exactly one of `node`, `python`, `go`, or `java`, which gh-aw writes to generated runtime files before launch.

```aw wrap
engine:
  id: pi
  driver: pi_agent_core_driver.cjs
  # or a custom workspace-relative driver:
  # driver: .github/drivers/my-driver.cjs
```

See [AI Engines Reference](/gh-aw/reference/engines/).

### Engine Behaviors (`engine.behaviors`)

A declarative configuration block inside an engine definition file (a built-in definition under `pkg/workflow/data/engines/<id>.md`, or a shared workflow imported from a repository) that describes how the compiler should generate install, config, execution, and MCP steps for a CLI-style engine. Defining behaviors in frontmatter avoids bespoke Go wrapper code — the runtime reads the fields and generates the corresponding workflow steps automatically. Key sub-fields include `installation` (package manager, binary name, version), `config-file` (path, content, merge strategy), `execution` (command name, args, model env var, MCP config env var), `manifest` (protected files and path prefixes), and `capabilities`. Engines that use `engine.behaviors` inherit shared step generation logic via `behavior_defined_engine.go`. See [AI Engines Reference](/gh-aw/reference/engines/).

### Log-Parser (`log-parser`)

An `EngineBehaviorDefinition` field that accepts an inline JavaScript snippet containing a `parseLog(logContent)` function, enabling behavior-defined engines to produce step summaries and normalized log events without a bespoke Go log parser. At compile time, the snippet is materialized to a runtime script file with a stable ID (`<engine-id>_log_parser`). The raw `parseLog` function is auto-wrapped by `createEngineLogParser` from `log_parser_shared.cjs`, so engine authors provide only the parsing logic and inherit file reading, event enrichment, and step-summary generation; the function must return `{ markdown, logEntries, mcpFailures, maxTurnsHit }`. See [AI Engines Reference](/gh-aw/reference/engines/).

```aw wrap
engine:
  id: my-agent
  behaviors:
    installation:
      package-manager: npm
      package-name: my-agent-cli
      binary-name: my-agent
    execution:
      command-name: my-agent
      model-env-var: MY_AGENT_MODEL
```

### Experiments (`experiments:`)

A frontmatter section that enables A/B testing of workflow prompt variants across successive runs. Each key in the `experiments:` map names an experiment; the value is either a bare array of variant strings or a rich object with analysis and decision metadata. At runtime the activation job selects one variant per experiment and exposes the assignment as `${{ experiments.<name> }}` for use anywhere in the workflow body.

Experiment state is persisted to dedicated experiment branches in the workflow repository. Use `gh aw experiments list` to inspect assignments and `gh aw experiments analyze` to inspect observations, statistical analysis, `COLLECTING` / `READY` readiness, and the deterministic `EXTEND` / `PROMOTE` / `REJECT` / `INCONCLUSIVE` decision. See [A/B Experiments](/gh-aw/experimental/experiments/) and the [Experiments Specification](/gh-aw/experimental/experiments-specification/).

```aw wrap
experiments:
  prompt_style: [concise, detailed]
---
Summarize this issue in a **${{ experiments.prompt_style }}** way.
```

### Evals (`evals:`)

An experimental frontmatter section that defines binary (YES/NO) evaluation questions to run after safe-outputs and before the conclusion job. Each question has an `id` and a `question` string; the runtime evaluates each with an LLM and records results in an `evals.jsonl` artifact. Supports a shorthand form (plain array of question objects) and an extended form with a top-level `model` override and optional `runs-on` configuration. Use `gh aw audit --evals` or `gh aw logs --evals` to filter results to runs that contain evals output. See [Frontmatter Reference](/gh-aw/reference/frontmatter/).

```aw wrap
evals:
  - id: issue-resolved
    question: "Was the reported issue fully resolved?"
  - id: tests-added
    question: "Were new tests added for the changed code?"
```

### Feature Flags (`features:`)

A frontmatter section that enables experimental or optional compiler and runtime behaviors as key-value pairs. Feature flags provide controlled access to new capabilities before they become defaults or are fully stabilized. Common flags include `action-mode` (controls how custom action references are compiled), `copilot-requests` (enables GitHub Actions token authentication for Copilot; currently in **private preview** — will not work unless your account has been onboarded), `mcp-gateway` (enables the MCP gateway proxy), `integrity-reactions` (enables reaction-based integrity promotion and demotion), `cli-proxy` (enables CLI proxy mode for integrity enforcement at the network boundary), and `awf-diagnostic-logs` (enables AWF Docker operational diagnostics collection on failure). `byok-copilot` is deprecated because Copilot BYOK behavior is now the default for `engine: copilot`. See [Feature Flags Reference](/gh-aw/reference/feature-flags/).

### Force Clean Git Credentials (`force-clean-git-credentials`)

A `checkout:` option that opts into an explicit credential cleanup step instead of relying on `actions/checkout`'s `persist-credentials: false` post-step cleanup. When set to `true`, the compiler emits the checkout with `persist-credentials: true` and injects a dedicated `Clean git credentials after checkout` step immediately after it. That step scrubs the credential helper and `http.*.extraheader` entries from `.git/config` and all nested submodule configs. Use this for submodule-heavy or sparse checkouts where the default post-step cleanup fails. See [Checkout Reference](/gh-aw/reference/checkout/).

### Fuzzy Scheduling

Natural language schedule syntax that automatically distributes workflow execution times to avoid load spikes. Instead of specifying exact times with cron expressions, fuzzy schedules like `daily`, `weekly`, or `daily on weekdays` are converted by the compiler into deterministic but scattered cron expressions. The compiler automatically adds `workflow_dispatch:` trigger for manual runs. Example: `schedule: daily on weekdays` compiles to something like `43 5 * * 1-5` with varied execution times across different workflows.

### Imports

Reusable workflow components shared across multiple workflows. Specified in the `imports:` field, can include tool configurations, common instructions, or security guidelines. Shared files without an `on:` field are validated but not compiled into GitHub Actions — they are only importable by other workflows.

Imports support a parameterized form using `uses`/`with` syntax when the shared file declares an `import-schema`. The compiler validates the passed values, substitutes them into the shared file, and errors on conflicting imports of the same file. See [Imports Reference](/gh-aw/reference/imports/).

### Pre-Agent Steps (`pre-agent-steps:`)

Steps injected into the agent job after artifacts are downloaded and before the engine executes. Defined in the `pre-agent-steps:` frontmatter field and composable via imports — imported pre-agent-steps are prepended to the main workflow's steps in import order. Useful for setup tasks such as installing dependencies or configuring the environment before the AI engine runs. See [Imports Reference](/gh-aw/reference/imports/).

### Post-Steps (`post-steps:`)

Steps injected into the agent job after the engine finishes execution. Defined in the `post-steps:` frontmatter field and composable via imports — imported post-steps are appended after the main workflow's post-steps in import order. Useful for cleanup, reporting, or artifact publishing after the AI engine completes. See [Imports Reference](/gh-aw/reference/imports/).

### Import Schema (`import-schema`)

A typed parameter contract declared in a shared workflow file that enables callers to pass values via `uses`/`with` syntax. The compiler validates each caller's `with` values against the schema and substitutes them into the shared file's frontmatter and body before processing. Supports typed fields with optional defaults; required fields without defaults cause a compile-time error if omitted. See [Imports Reference](/gh-aw/reference/imports/#import-schema-import-schema).

### MCP Gateway Settings (`engine.mcp`)

`engine.mcp` is the subset of `engine:` configuration that controls MCP gateway behavior — specifically `tool-timeout` and `session-timeout`. Shared workflow files can export only these settings (without specifying an engine identifier), allowing importers to inherit MCP timeout configuration without coupling a shared component to a specific engine. The importing workflow's own `engine.mcp` values take precedence; among imports, the first-wins strategy applies. See [Imports Reference — Importing MCP Gateway Settings](/gh-aw/reference/imports/#importing-mcp-gateway-settings).

### Runtime Import (`{{#runtime-import}}`)

A body-level directive that injects the text content of another file at a specific point in the workflow markdown. Unlike the `imports:` frontmatter field (which merges configuration), `{{#runtime-import filepath}}` splices raw markdown text — useful for sharing reusable prompt snippets, tone instructions, or reference material. Use `{{#runtime-import? filepath}}` for an optional include that silently skips a missing file. Paths are resolved within the `.github` folder with or without the `.github/` prefix. See [Runtime Imports](/gh-aw/reference/templating/#runtime-imports).

### Emoji (`emoji:`)

An optional frontmatter field that attaches an emoji to represent the workflow visually in listings and UI surfaces. Accepts a single emoji character (e.g., `"🤖"`). See [Frontmatter Reference](/gh-aw/reference/frontmatter/).

### Label Trigger Shorthand

A compact syntax for label-based triggers: `on: issue labeled bug` or `on: pull_request labeled needs-review`.

### LSP (`lsp:`)

An experimental frontmatter field that declares Language Server Protocol (LSP) servers for Copilot-engine workflows. At compile time, the compiler generates `~/.copilot/settings.json` with an `lspServers` block and injects install steps for known server ecosystems. When configured, Copilot gets semantic code-intelligence tools: symbol lookup, go-to-definition, find references, hover/type info, diagnostics, and rename/refactor — enabling symbol-aware navigation of large codebases.

Only supported with `engine: copilot`; any other engine causes a compile-time error. Emits an experimental feature warning at compile time.

```aw wrap
engine:
  id: copilot
lsp:
  go:
    command: gopls
    fileExtensions:
      ".go": go
```

See [AI Engines Reference](/gh-aw/reference/engines/). The compiler expands the shorthand to standard GitHub Actions trigger syntax and automatically includes a `workflow_dispatch` trigger with an `inputs.item_number` parameter, enabling manual dispatch for a specific issue or pull request. Supported for `issue`, `pull_request`, and `discussion` events. See [LabelOps patterns](/gh-aw/patterns/label-ops/).

### Labels

Optional workflow metadata for categorization and organization. Enables filtering workflows in the CLI using the `--label` flag.

### Model Alias

A short human-friendly name (such as `sonnet` or `mini`) that gh-aw resolves to the best available concrete model at compile time. Aliases are defined as ordered lists of provider-scoped glob patterns; the first pattern that matches an available model wins. Meta-aliases reference other aliases and are resolved recursively. Built-in vendor aliases and meta-aliases are listed in the [Model Aliases & Multipliers Reference](/gh-aw/reference/model-tables/). Custom aliases can be defined in workflow frontmatter using the [Model Alias Format Specification](/gh-aw/specs/model-alias-specification/).

### Model Policy (`models.allowed`, `models.blocked`)

An experimental frontmatter feature that restricts which AI models a workflow may use at runtime. Configured under the `models:` top-level key with two fields: `allowed` (a list of model names or glob patterns the workflow is permitted to use) and `blocked` (a list of patterns unconditionally refused). Both lists are enforced by the AWF proxy via `apiProxy.allowedModels` and `disallowedModels`. Across imports, policies from multiple files are merged as unions. Environment-variable overrides are supported for runtime flexibility.

```aw wrap
models:
  allowed: ["gpt-5", "claude-*"]
  blocked: ["*-preview"]
```

### Default AI Credits Pricing (`models.default-ai-credits-pricing`)

A `models:` frontmatter field that provides a fallback per-token pricing rate (in $/1M tokens) for models not present in the AWF built-in pricing table. When the AWF API proxy encounters an unrecognized model — such as a self-hosted BYOK Ollama or vLLM instance — it normally rejects the request with HTTP 400 `unknown_model_ai_credits`. Setting `default-ai-credits-pricing` supplies `input` and `output` rates that the proxy uses instead, preventing the rejection. Set both to `0` for free or local models.

```aw wrap
models:
  default-ai-credits-pricing:
    input: 0    # $/1M input tokens
    output: 0   # $/1M output tokens
```

### Missing Model AI Credits Pricing

A specialized failure category reported when a model has no entry in the AWF built-in pricing table and no `default-ai-credits-pricing` fallback is configured, causing the API proxy to reject every inference request with HTTP 400. Detected by the conclusion job, which surfaces a dedicated warning distinguishing this configuration issue from a transient or retryable failure. Resolve by adding pricing via `models.default-ai-credits-pricing`, mapping the model name to a known model with the `models` field, or switching to a model already in the pricing table. See [Default AI Credits Pricing](#default-ai-credits-pricing-modelsdefault-ai-credits-pricing).

### Max AI Credits (`max-ai-credits`)

A top-level frontmatter field that caps the total AI Credits (AIC) the AWF proxy will spend within a single workflow run. Applies to all engines and maps to `apiProxy.maxAiCredits` in the compiled lock file. Defaults to `1000` when omitted. Accepts an integer, an optional `K`/`M` suffix string (for example, `100M`), or a GitHub Actions expression that resolves to an integer at runtime. Example:

```aw wrap
max-ai-credits: 500
```

### Max Daily AI Credits (`max-daily-ai-credits`)

A top-level frontmatter field that sets a 24-hour AI Credits cap for a single workflow, aggregated across all recent runs of the same workflow in the repository. When the activation job detects that the previous 24 hours already exceed this threshold, it warns, creates an issue, skips the agent job, and reports a specialized failure. Disabled by default when omitted. Set to `-1` to explicitly disable it. Accepts plain integers or `K`/`M` suffixes (e.g., `100M`). Skipped for `workflow_call`, `repository_dispatch`, and `workflow_dispatch` runs carrying internal `aw_context` dispatch metadata. Example:

```aw wrap
max-daily-ai-credits: 15M
```

See [Cost Management](/gh-aw/reference/cost-management/) and [Compiler Enterprise Environment Controls](/gh-aw/reference/compiler-enterprise-environment-controls/).

### Max Runs (`max-runs`, deprecated)

Deprecated top-level alias for the AWF invocation cap. Use `max-turns` instead. This cap limits the number of times the AWF proxy invokes the AI engine within a single workflow run and maps to `apiProxy.maxRuns` in the compiled lock file. Defaults to `500` when omitted. Accepts an integer or a GitHub Actions expression that resolves to an integer at runtime. Example:

```aw wrap
max-turns: 10
```

Not to be confused with `max-runs-per-window`, an unrelated current field under `user-rate-limit` that caps how often a single user can trigger the workflow. See [User Rate Limit](#user-rate-limit-user-rate-limit) and [Engines Reference](/gh-aw/reference/engines/).

### Max Tool Denials (`max-tool-denials`)

A top-level frontmatter field that stops Copilot SDK inference when tool requests are denied a specified number of times in succession. Only applies when `engine.id: copilot` and `engine.copilot-sdk: true` are set. Defaults to `5`. Useful for preventing runaway inference loops when a tool is unavailable or consistently rejected. Example:

```aw wrap
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 8
```

See [Engines Reference](/gh-aw/reference/engines/).

### Max Turn Cache Misses (`max-turn-cache-misses`)

A top-level frontmatter field that caps the number of consecutive AWF API proxy cache misses allowed before inference is halted. Maps to `apiProxy.maxCacheMisses` in the compiled AWF config. Resolved at compile time via three-tier precedence: (1) the frontmatter value, (2) the `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES` compiler process environment variable, (3) built-in constant default of `5`. Imported workflow values follow first-wins top-level guardrail merge behavior. Manage the org-wide default via `gh aw env` using the `default_max_turn_cache_misses` key. See [Compiler Enterprise Environment Controls](/gh-aw/reference/compiler-enterprise-environment-controls/).

```aw wrap
max-turn-cache-misses: 10
```

### Max Turns (`max-turns`)

A top-level frontmatter field that caps the number of chat iterations (model responses and tool calls) for a single workflow run. Each additional turn consumes more tokens and Actions compute time, so a turn limit bounds runaway loops and cost. Supported across Claude, Codex, Copilot, Gemini, and Pi engines. Compiles to the `GH_AW_MAX_TURNS` environment variable for the engine runtime. Accepts an integer or a GitHub Actions expression. The deprecated alias `engine.max-turns` continues to compile; use `gh aw fix engine-max-turns-to-top-level` to migrate. Example:

```aw wrap
max-turns: 20
```

See [Cost Management](/gh-aw/reference/cost-management/).

### Network Permissions

Controls over external domains and services a workflow can access. Configured via `network:` section with options: `defaults` (common infrastructure), custom allow-lists, or `{}` (no access).

### Network Allowed Input (`network.allowed-input`)

An opt-in frontmatter flag for `workflow_call` workflows that exposes a `network_allowed` input parameter, allowing callers to extend the compiled workflow's network allowlist at runtime. When enabled with `network.allowed-input: true`, the compiler injects a `network_allowed: string` input into the `workflow_call` interface. Callers provide a comma-separated list of ecosystem identifiers and/or domains that are unioned with the static `network.allowed` baseline before the agent starts. The compiled workflow's static allowlist acts as an immutable floor — callers can only add domains, never remove them. Useful for reusable workflows that serve consumers with varying network requirements without requiring per-consumer forks or recompilation. See ADR-33200 for implementation details.

```aw wrap
on: workflow_call
network:
  allowed: [defaults]
  allowed-input: true  # Caller can extend with network_allowed: "python,rust"
```

### Observability (`observability.otlp`)

A frontmatter field that enables OpenTelemetry trace
export from workflow runs. It supports single-endpoint and
multi-endpoint OTLP export with optional headers.

See [OpenTelemetry](/gh-aw/reference/open-telemetry/) for
setup, runtime variables, and span semantics.

### OTLP If-Missing (`observability.otlp.if-missing`)

Controls behavior when OTLP endpoint or header values resolve to empty at runtime. Accepts `error` (default — fails startup), `warn` (logs a warning and skips MCP gateway OTLP configuration), or `ignore` (silently skips MCP gateway OTLP configuration). Useful in shared imports where OTLP secrets may be absent in some repositories — set to `ignore` to make observability opt-in without breaking workflows that lack the secrets. See [Frontmatter](/gh-aw/reference/frontmatter/#observability-observability).

### OTLP Resource Attributes (`observability.otlp.resource-attributes`)

A frontmatter option that appends custom key/value pairs to the standard gh-aw and GitHub resource attribute set exported with each OTLP trace. Use static strings or GitHub Actions expressions. Do not use `secrets.*` or `vars.*` values because resource attributes are sent to external observability backends and are not treated as secret values. See [OpenTelemetry](/gh-aw/reference/open-telemetry/#custom-resource-attributes).

### OTLP Authorization Header Guard

A compiler-enforced rule for `observability.otlp` headers requiring the bare secret expression for `Authorization` values rather than a scheme prefix such as `Bearer`. For Sentry endpoints, gh-aw automatically rewrites `Authorization` to `x-sentry-auth`, so no prefix is needed. Prevents scheme-only header values (a stray auth scheme with no token) from being sent to the OTLP backend. See [OpenTelemetry](/gh-aw/reference/open-telemetry/).

### Custom Span (`logSpan`)

A telemetry API provided by the `otlp.cjs` helper that lets shared workflow imports emit their own OTLP spans alongside built-in gh-aw telemetry. Call `otlp.logSpan(toolName, attributes, options)` inside a `github-script` step to attach domain-specific measurements to the same distributed trace as the workflow run. The function is non-fatal and never throws — export failures are surfaced as warnings. See [OpenTelemetry](/gh-aw/reference/open-telemetry/#custom-spans-from-shared-imports).

### Activation Steps (`jobs.activation.steps`)

An activation-only built-in job injection field. The compiler inserts these steps after the generated activation checkout/gate sequence and before the activation artifact is staged and uploaded. Useful for shared workflows (for example, Squad initialization) that need the activation checkout available before preparing content for the activation artifact. Imported activation `steps` are merged in import declaration order before the main workflow's activation `steps`. `jobs.<other-built-in>.steps` is rejected at compile time. See [Custom Jobs](/gh-aw/reference/steps-jobs/#jobs-and-steps).

### Setup-Steps (`jobs.<job-id>.setup-steps`)

Steps injected immediately after the compiler-generated `actions/setup` step for a custom or built-in job. Defined under `jobs.<job-id>.setup-steps` in workflow frontmatter. When both a main workflow and an imported workflow define `setup-steps` for the same job, imported setup-steps run first. `setup-steps` remain distinct from `pre-steps` and are not merged across keys. `jobs.activation.setup-steps` and `jobs.pre_activation`/`jobs.pre-activation` `setup-steps` are refused at compile time because they can short-circuit protections. See [Custom Jobs](/gh-aw/reference/steps-jobs/#jobs-and-steps).

### Pre-Steps (`jobs.<job-id>.pre-steps`)

Steps injected after any compiler setup scaffolding and job-level `setup-steps`, but before the main work for that job. Defined under `jobs.<job-id>.pre-steps` in workflow frontmatter. When both a main workflow and an imported workflow define `pre-steps` for the same job, imported pre-steps run first. This is distinct from both `jobs.<job-id>.setup-steps` and the top-level `pre-steps` field, which injects steps into the agent job only. See [Custom Jobs](/gh-aw/reference/steps-jobs/#jobs-and-steps).

### Restore Memory (`jobs.<job-id>.restore-memory`)

A custom job field that restores configured memory stores (`cache-memory`, `repo-memory`, `comment-memory`) in read-only mode before a deterministic job's steps execute. Set `jobs.<job-id>.restore-memory: true` to have gh-aw inject restore, clone, and prepare steps immediately before `pre-steps` and `steps`. This surface is read-only — gh-aw never emits cache save, repo push, or safe-output write-back steps for custom jobs, preventing accidental state mutation. Useful when a deterministic job (for example, a reporting or analysis job) needs to read prior agent state without participating in memory writes. See [Custom Jobs](/gh-aw/reference/steps-jobs/#jobs-and-steps).

### Pre-Activation Dependencies (`on.needs:`)

A frontmatter field that declares custom jobs that both the `pre_activation` and `activation` built-in jobs depend on. Use this when credentials or secrets must be fetched by a custom job before activation runs — for example, when `on.github-app` tokens come from a secrets-manager job. Values must reference custom jobs defined in the top-level `jobs:` section; built-in job names are rejected at compile time. See [Triggers Reference](/gh-aw/reference/triggers/).

### Stop After

A workflow configuration field (`stop-after:`) that automatically prevents new runs after a specified time limit. Accepts absolute dates (`YYYY-MM-DD`, ISO 8601), relative time deltas (`+48h`, `+7d`), or a GitHub Actions expression (for example `${{ inputs.stop-after }}`) resolved at workflow runtime. Literal values have a minimum granularity of hours and are resolved at compile time via a typed `on.stop-after` field; expression values pass through compilation unresolved and are evaluated when the workflow runs, enabling parameterized stop times from `workflow_dispatch` inputs or other runtime state. Useful for trial periods, experimental features, and cost-controlled schedules. Recompile with `gh aw compile --refresh-stop-time` to reset a literal deadline. See [Ephemerals](/gh-aw/reference/ephemerals/).

### Cooldown (`on.cooldown`)

A frontmatter field that blocks the `agent` job from starting again shortly after a recent completed run. The value must be a literal Go duration string of at least five minutes; GitHub Actions expressions are rejected so the compiler can validate the setting deterministically. The compiler injects a pre-activation run-history check (granting `actions: read` when needed) that inspects the most recent completed workflow run for a started `agent` job and skips activation if that run finished within the cooldown window; runs where the `agent` job was skipped are excluded, and the check fails open (allows activation) if run history cannot be queried. See [Triggers Reference](/gh-aw/reference/triggers/).

### `deployment_status` Trigger

A GitHub Actions trigger that fires when an external deployment changes state. Supported states are `error`, `failure`, `pending`, `queued`, `in_progress`, `success`, `inactive`, and `waiting`. The gh-aw compiler accepts an optional `state:` filter in the trigger definition and synthesizes a job-level `if:` condition so that the agent only runs for the specified states. A natural-language shorthand is also supported — `on: "deployment failed"` expands to `deployment_status` with `state: [failure]`. See [Frontmatter Reference](/gh-aw/reference/frontmatter/).

```aw wrap
on:
  deployment_status:
    state: [error, failure]
```

### Triggers

Events that cause a workflow to run, defined in the `on:` section of frontmatter. Includes issue events, pull requests, schedules, manual runs, and slash commands.

### Bot Filtering (`on.bots:`, `on.skip-bots:`)

An authorization control configuring which GitHub bot accounts can trigger a workflow. `bots:` accepts an allowlist of bot account names; `skip-bots:` is the inverse, allowing all bots except those listed. The `[bot]` suffix is optional — `github-actions` matches `github-actions[bot]` automatically. Can be combined with other trigger types such as `workflow_run`. See [Triggers Reference](/gh-aw/reference/triggers/).

```aw wrap
on:
  issues:
    types: [opened]
  bots: ["dependabot[bot]", "renovate[bot]"]
```

### Role Filtering (`on.roles:`, `on.skip-roles:`)

An authorization control restricting which repository access roles can trigger a workflow. `roles:` is an exact-match allowlist for standard GitHub roles — each value must match the actor's role exactly, with no privilege hierarchy. Defaults to `[admin, maintainer, write]`. `skip-roles:` is the inverse.

Available roles: `admin`, `maintainer`/`maintain`, `write`, `triage`, `read`, `all`. Workflows with unsafe triggers (`push`, `issues`, `pull_request`) automatically enforce role checks.

> [!WARNING]
> `roles` is not a privilege threshold. Setting `roles: [write]` rejects admins and maintainers because `admin !== write`. To accept all typical contributors, list every role explicitly.

Actors assigned a **custom organization repository role** (e.g. `Security Champions`) are authorized via the inherited standard role that GitHub reports for that custom role — not the custom role name. A user with a custom role inherited from `write` is authorized whenever `write` is in the required set, while a custom role inherited from `maintain` is still rejected by `roles: [write]`.

See [Triggers Reference](/gh-aw/reference/triggers/).

```aw wrap
on:
  issues:
    types: [opened]
  roles: [admin, maintainer, write]
```

### Skip Author Associations (`on.skip-author-associations`)

A pre-activation gating mechanism that skips workflow execution when the triggering event's author has a specific `author_association` value (such as `contributor`, `first_time_contributor`, or `none`). Configured per-event in the `on.skip-author-associations` field. Compiles to a job-level `if` expression — no runtime script step cost for skipped runs. Values are case-insensitive and accept a single string or array of strings per event key. See [Triggers Reference](/gh-aw/reference/triggers/).

### Trigger File

A plain GitHub Actions workflow (`.yml`) that separates trigger definitions from agentic workflow logic. Calls a compiled orchestrator's `workflow_call` entry point in response to any GitHub event (issues, pushes, labels, manual dispatch). Decouples trigger changes from the compilation cycle — updating when an orchestrator runs requires editing only the trigger file, not recompiling the agentic workflow.

Trigger files can live in the **same repository** as the orchestrator or in a **different repository** (cross-repo `workflow_call`). Cross-repo usage requires the callee repository to be public, internal, or to have explicitly granted Actions access. When using `secrets: inherit`, the caller's secrets are passed through — including `COPILOT_GITHUB_TOKEN`, which must be configured in the caller's repository. See [MultiRepoOps](/gh-aw/patterns/multi-repo-ops/).

### User Rate Limit (`user-rate-limit`)

A frontmatter field that prevents individual users from triggering a workflow too frequently. Configured with `max-runs-per-window` (maximum runs per time window, 1–10), an optional `window` in minutes (default 60, max 180), an optional `events` list to restrict which trigger types count, and an optional `ignored-roles` list of exempt roles (default: `[admin, maintain, write]`). The pre-activation job checks recent runs and cancels the current run if the limit is exceeded. Example:

```aw wrap
user-rate-limit:
  max-runs-per-window: 5
  window: 60
  ignored-roles: []
```

`max-runs-per-window` is unrelated to the deprecated top-level `max-runs` field, which caps AI engine invocations — see [Max Runs](#max-runs-max-runs-deprecated).

See [Rate Limiting Controls](/gh-aw/reference/rate-limiting-controls/).

### Weekday Schedules

Scheduled workflows configured to run only Monday through Friday using `daily on weekdays` syntax. Recommended for daily workflows to avoid the "Monday wall of work" where tasks accumulate over weekends and create a backlog on Monday morning. The compiler converts this to cron expressions with `1-5` in the day-of-week field. Example: `schedule: daily on weekdays` generates a cron like `43 5 * * 1-5`.

### workflow_call

A trigger enabling a compiled workflow to be invoked by another workflow in the same organization. Adding `workflow_call` to the `on:` section exposes the lock file as a callable workflow, with optional inputs callers can pass for context. Commonly used with a [Trigger File](#trigger-file) to decouple trigger definitions from agentic workflow compilation. See [MultiRepoOps](/gh-aw/patterns/multi-repo-ops/).

### workflow_dispatch

A manual trigger that runs a workflow on demand from the GitHub Actions UI or via the GitHub API. Requires explicit user initiation.

## GitHub and Infrastructure Terms

### GitHub Actions

GitHub's built-in automation platform that runs workflows in response to repository events. Agentic workflows compile to GitHub Actions YAML format, leveraging existing infrastructure for execution, permissions, and secrets.

### GitHub Projects (Projects v2)

GitHub's project management and tracking system organizing issues and pull requests using customizable boards, tables, and roadmaps. Provides flexible custom fields, automation, and GraphQL API access. Agentic workflows can manage project boards using the `update-project` safe output. Requires organization-level Projects permissions.

### GitHub Actions Secret

A secure, encrypted variable stored in repository or organization settings holding sensitive values like API keys or tokens. Access via `${{ secrets.SECRET_NAME }}` syntax.

### GitHub App (`github-app:`)

A GitHub App installation used for authentication and token minting in workflows. The `github-app:` field (which replaces the deprecated `app:` key) accepts `client-id` (preferred) or `app-id` (deprecated alias) together with `private-key` to mint short-lived installation access tokens with fine-grained, automatically-revoked permissions. Can be configured in `safe-outputs:` to override the default `GITHUB_TOKEN` for all safe output operations, or in `checkout:` for accessing private repositories. Run `gh aw fix` to automatically migrate `app-id` to `client-id`. See [Authentication Reference](/gh-aw/reference/auth/#using-a-github-app-for-authentication).

### YAML

A human-friendly data format for configuration files using indentation and simple syntax to represent structured data. In agentic workflows, YAML appears in frontmatter and compiled `.lock.yml` files.

### Personal Access Token (PAT)

A token authenticating you to GitHub's APIs with specific permissions. Required for GitHub Copilot CLI to access Copilot services. Created at github.com/settings/personal-access-tokens.

### Skill Files

Markdown files with YAML frontmatter stored in `.github/skills/` for repository-scoped Copilot Chat skills. Created by `gh aw init`, these files can be invoked by calling the skill in Copilot Chat to guide workflow creation, debugging, and updates. The `agentic-workflows` skill is a unified dispatcher routing requests to specialized prompts.

### Frontmatter Skills (`skills:`)

A frontmatter field that declares skills to install in the activation job before the agent runs. Entries can be local development paths (for example, `skills/name` or `.github/skills/name`) or external skill specs (for example, `owner/repo` or `owner/repo/path@sha`) pointing to a `.github/skills/` skill directory. Local paths install via `gh skill install ... --from-local`, while static external references must be pinned to a full 40-character lowercase commit SHA. When a skill fails to install, the failure is captured in the agent failure context and surfaces in failure issue/comment reports. Requires a recent version of the `gh` CLI. See [Skill Install Failure](#skill-install-failure) for error handling.

### Non-SHA Refs (Skills)

Branch or tag names (as opposed to a full 40-character lowercase commit SHA) supplied as the `<ref>` in a skill reference (`owner/repo@<ref>`, `owner/repo/skill/path@<ref>`). At compile time, `gh aw` resolves a non-SHA ref and rewrites the reference to the matching commit SHA in the generated lock file, so the installed skill is pinned even though the source workflow specifies a mutable ref. If resolution fails (for example, no network access or authentication), the compiler keeps the original unpinned ref and emits a warning. Omitting the ref entirely (`owner/repo@`) installs from the repository's default branch, is never pinned, and always triggers a compiler warning. See [Frontmatter Reference](/gh-aw/reference/frontmatter/#frontmatter-skills-skills).

### Skill Install Failure

A failure category reported when one or more frontmatter skills could not be installed before the agent ran. Triggered by invalid skill references, inaccessible repositories, insufficient token permissions, or unsupported `gh` CLI versions. When skill install failures occur, they are captured by the `collect-skill-install-failures` activation step and included in the agent failure issue or comment via the `{skill_install_failure_context}` template. Resolve by verifying the skill reference format (local path such as `skills/name` or external reference such as `owner/repo` / `owner/repo/skill/path@sha`), confirming the token has read access to external skill repositories when applicable, and ensuring a recent `gh` CLI version is available. See [Safe Outputs Reference](/gh-aw/reference/safe-outputs/).

### Fine-grained Personal Access Token

A GitHub Personal Access Token with granular permission control, specifying exactly which repositories the token can access and what permissions it has. Created at github.com/settings/personal-access-tokens.

### Copilot-Only Artifacts (`--no-mcp`, `--no-agent`)

Configuration files that `gh aw init` creates only when the Copilot engine is selected (`--engine copilot`): the Agentic Workflows custom agent (`.github/agents/agentic-workflows.md`) and MCP server integration. Both are enabled by default with the Copilot engine; pass `--no-mcp` to skip MCP server integration or `--no-agent` to skip custom agent creation. Non-Copilot engines skip these artifacts automatically. See [CLI Reference](/gh-aw/setup/cli/#init).

### `RUNNER_TEMP` / `${{ runner.temp }}`

A GitHub Actions environment variable pointing to a per-job temporary directory on the runner. Agentic workflows store compiled scripts and runtime artifacts under `${RUNNER_TEMP}/gh-aw/` for compatibility with self-hosted runners that may not have write access to system directories. In shell `run:` blocks, use the shell variable form `${RUNNER_TEMP}`; in `with:` or `env:` YAML fields, use the expression form `${{ runner.temp }}`.

## Development and Compilation

### CLI (Command Line Interface)

The `gh aw` extension for GitHub CLI providing commands for managing agentic workflows: compile, run, status, logs, add, deploy, and project management.

### Codemod

An automated transformation script applied by `gh aw fix` that updates workflow markdown files from deprecated syntax to the current format. Codemods rename frontmatter keys, restructure values, or remove obsolete settings without changing workflow behavior. They run in dry-run mode by default; pass `--write` to apply changes. `gh aw upgrade` applies all relevant codemods automatically as part of the upgrade process. List available codemods with `gh aw fix --list-codemods`. See [Upgrading Workflows](/gh-aw/guides/working-with-workflows/#upgrading-workflows).

### Doctor (`gh aw doctor`)

A CLI diagnostic command that verifies `gh` CLI authentication, repository ownership and access, and local checkout state before setup or troubleshooting work. Inside a GitHub Enterprise checkout, it auto-detects the host from the git remote when `GH_HOST` is unset; outside a checkout, authenticate with `gh auth login --hostname <host>` and set `GH_HOST` so diagnostics target the correct host. Supports `--json`, `--repo`, and `--require-owner-type` options. See [CLI Reference](/gh-aw/setup/cli/#doctor).

### `gh aw models`

A CLI command that lists the model catalog with AI Credits pricing weights, built-in model aliases and their resolution order, and models observed in local automation artifacts. By default, refreshes observed-model data from recent run artifacts (`summary.json` token usage, per-run token usage artifacts, and `awf-reflect.json` endpoint model lists) before reporting. Accepts `--json` for machine-readable output, `--logs-dir` to read observed models from a non-default logs directory, `--refresh-count` to control how many recent runs are inspected, and `--refresh-observed=false` to skip the artifact refresh and use local data only. See [CLI Reference](/gh-aw/setup/cli/#models).

### Playground

An interactive web-based editor for authoring, compiling, and previewing agentic workflows without local installation. The Playground runs the gh-aw compiler in the browser using [WebAssembly](#webassembly-wasm) and auto-saves editor content to `localStorage` so work is preserved across sessions. Available at `/gh-aw/editor/`.

### Audit (`gh aw audit`)

A CLI command that downloads workflow run artifacts and logs, analyzes MCP tool usage and network behavior, and generates a structured Markdown or JSON report. The report covers failure analysis, tool usage, MCP server status, firewall activity, token/cost metrics, behavior fingerprint, and safe-output summary. Accepts a numeric run ID or any GitHub Actions run or job URL. Both `gh aw audit` and `gh aw logs` accept a `--runtime` flag (for example, `--runtime gvisor` or `--runtime docker-sbx`) that filters results to runs whose [`sandbox.agent.runtime`](#sandboxagentruntime) matches the given value, using the value persisted in each run's `aw_info.json`. See [Audit Commands](/gh-aw/reference/audit/).

### Audit Diff (multi-run mode)

Passing two or more run IDs to `gh aw audit` activates diff mode: the first ID is the base and the rest are compared against it. Reports domain additions and removals, allowed/denied status changes, request volume drift, and anomaly flags across firewall, MCP tool usage, and run metrics dimensions. Useful for detecting regressions and behavioral drift between runs. See [Audit Commands](/gh-aw/reference/audit/).

### `usage` Artifact

A compact artifact produced by the conclusion job, containing workflow-run metadata and aggregated token-usage data used by lightweight reporting and forecasting paths. Unlike the full `agent` artifact, `usage` carries only summarized usage summaries — making it faster to download when detailed agent logs are not needed. Download with `gh aw logs <run-id> --artifacts usage`. See [Artifacts Reference](/gh-aw/reference/artifacts/).

### Behavior Fingerprint

A multi-dimensional characterization of a single workflow run produced by `gh aw audit`. Captures the task domain, network access patterns, tool usage profile, token consumption, and agentic assessments in a compact summary. Two runs with the same fingerprint exhibit identical observable behavior; diverging fingerprints signal regressions or unexpected changes. See [Audit Commands](/gh-aw/reference/audit/).

### Cross-Run Audit Report (`gh aw logs --format`)

A feature of `gh aw logs` that aggregates firewall, MCP, and metrics data across multiple workflow runs to produce a security and performance report. Includes an executive summary, domain inventory, and per-run breakdown with anomaly detection. Designed for security reviews, compliance checks, and feeding optimization agents. See [Audit Commands](/gh-aw/reference/audit/#gh-aw-logs---format-fmt).

### Deploy (`gh aw deploy`)

A CLI command that orchestrates full workflow rollout to a target repository in a single invocation. `gh aw deploy` clones the target repository, runs `update` to refresh any sourced workflows, runs `add` to install the requested workflows, runs `compile --purge` to regenerate lock files and remove stale outputs, then opens a pull request with all changes for review. Replaces the manual sequence of `clone → update → add → compile → pr` commands and skips the add phase for workflows that already carry a `source:` frontmatter field to prevent duplicate installations. Accepts `--repo` to specify the target repository and `--cool-down` to set the default scheduling interval. See [CLI Reference](/gh-aw/setup/cli/).

### `gh aw env`

A CLI command that reads and writes [`GH_AW_DEFAULT_*`](#gh_aw_default_) governance variables as GitHub Actions variables at enterprise, organization, or repository scope. Use `gh aw env get` to export current values to a YAML file and `gh aw env update` to apply changes from a YAML file. Supports `--dry-run` to preview changes before applying and `--yes` to skip the confirmation prompt in automation. Defaults percolate through scopes following a most-specific-wins model: workflow frontmatter overrides repository variables, which override organization variables, which override enterprise defaults. See [Governance](/gh-aw/reference/governance/).

### AI Credits (AIC)

The primary inference-cost metric for GitHub Agentic Workflows. One AI Credit equals `0.01 USD` and is computed from input, output, cache-read, cache-write, and reasoning tokens multiplied by per-model pricing weights. AIC provides a model-normalized spend unit across all supported engines, enabling consistent budget governance and cost comparison. Reports from `gh aw audit` and `gh aw logs` expose AIC as `total_aic` (per episode or run) and per-request values. Use `max-ai-credits` and `max-daily-ai-credits` in workflow frontmatter to set budget caps. See [AI Credits Specification](/gh-aw/specs/ai-credits-specification/).

### Effective Tokens (ET)

The predecessor cost metric to [AI Credits (AIC)](#ai-credits-aic), computed as a weighted sum of input and output tokens to approximate relative inference cost. Deprecated in 2026 in favor of AI Credits, which provides direct monetary normalization (1 AIC = 0.01 USD) across all supported model providers. The `effective_tokens` field in audit JSON output is retained for backward compatibility; use `total_aic` for cost analysis in new workflows. See [Effective Tokens Specification](/gh-aw/specs/effective-tokens-specification/).

### Forecast (`gh aw forecast`)

A CLI command that projects future AI Credits (AIC) consumption using a statistical simulation. It samples historical workflow runs, applies a Poisson-bootstrap algorithm to model run frequency, and returns P10/P50/P90 percentile estimates over a configurable time horizon. Supports both local (`.github/workflows/`) and remote (`--repo`) discovery modes. Output is available as a console table or machine-readable JSON (`--json`). Forecasts are estimates and may be inaccurate. Useful for capacity planning, budget governance, and detecting cost regressions before they occur. See [Forecast Specification](/gh-aw/specs/forecast-specification/).

### Time Between Turns (TBT)

The elapsed time between consecutive LLM API calls in a workflow run. TBT matters because provider-side prompt caches expire: Anthropic currently uses a 5-minute TTL, and OpenAI has a similar cache with variable TTL. If TBT exceeds the cache window, the full prompt must be reprocessed, increasing cost.

`gh aw audit` reports average and maximum TBT in the Session Analysis section and warns when cache analysis exceeds Anthropic's 5-minute threshold. To keep caches warm, minimize blocking tool calls, parallelize independent work, and avoid long-running shell commands between turns.

### Ambient Context

The token footprint of the first LLM invocation in a workflow run, used as a proxy for the static context loaded at startup (system prompt, tools list, memory). Because the first invocation fires before the agent has accumulated any conversation history, its input token count primarily reflects the overhead of the configured environment rather than task-specific content. Reported as an optional `ambient_context` object in `gh aw audit` and `gh aw logs` JSON output with three fields: `input_tokens`, `cached_tokens`, and `effective_tokens`. Useful for comparing context overhead across different workflow configurations. See [Audit Commands](/gh-aw/reference/audit/).

### Firewall Analysis

A section of the `gh aw audit` report that breaks down all network requests made during a workflow run — showing allowed domains, denied domains, request volumes, and policy attribution. Derived from AWF firewall logs. Pass multiple run IDs to `gh aw audit` (e.g. `gh aw audit <base> <compare>`) to compare firewall behavior across runs and identify new or removed domain accesses. See [Audit Commands](/gh-aw/reference/audit/) and [Network Permissions](/gh-aw/reference/network/).

### Body Hash

A deterministic SHA-256 hash of the markdown body (the natural-language prompt text) of a workflow file. Complements the [Frontmatter Hash](#frontmatter-hash), which covers only configuration fields. The body hash is stored in the lock file metadata and checked at activation time when `on.stale-check: "full"` is set. Use `stale-check: "full"` when prompt-body edits should also trigger recompilation detection, not just configuration changes.

```aw wrap
on:
  stale-check: "full"
```

### Frontmatter Hash

A deterministic SHA-256 hash of a workflow's frontmatter configuration, including all imported workflow frontmatter collected in breadth-first order. The hash covers security-relevant fields (`engine`, `on`, `permissions`, `tools`, `network`, `safe-outputs`, etc.) while excluding the markdown body. Identical configurations produce identical hashes across the Go and JavaScript compiler implementations, enabling change detection, tamper verification, and reproducibility checks. To also hash the prompt body, use `on.stale-check: "full"` (see [Body Hash](#body-hash)). See [Frontmatter Hash Specification](/gh-aw/specs/frontmatter-hash-specification/).

### Custom Routing Signal (`engine_base_url_customized`)

A boolean field in compiled lock file metadata (`# gh-aw-metadata:`) that authoritatively records whether a Copilot engine is running with custom provider/base-URL routing — via BYOK mode, a non-GitHub `engine.model-provider`, or an explicit `engine.api-target` — instead of default GitHub-hosted routing. Uses `omitempty` so default configurations omit the field rather than emit `false`. Replaces fragile downstream inference from compiled step `env:` blocks. See [Lock File Metadata](/gh-aw/reference/compilation-process/).

### Action-Pin Mapping (`action_pins`)

An `aw.json` configuration field that redirects action references to replacement references before pin resolution occurs. Enables enterprises in private-cloud or air-gapped environments to use internally mirrored actions without modifying individual workflow files. Keys and values use `owner/repo@ref` format; each source version must be mapped individually. The redirect is applied at the start of the pin resolution pipeline, so the standard resolution steps (cache, GitHub API, embedded pins) operate on the mapped target.

```json title=".github/aw.json"
{
  "action_pins": {
    "actions/checkout@v4": "acme-corp/checkout-mirror@v4"
  }
}
```

### actionlint

A static analysis tool for GitHub Actions workflow files that detects syntax errors, type mismatches, and other issues. Integrated into `gh aw compile` via the `--actionlint` flag. Runs in a Docker container and reports lint findings separately from tooling/integration errors (such as Docker failures or timeouts) that prevent the linter from running. See `--actionlint --zizmor --poutine` in the [Compilation Reference](/gh-aw/reference/compilation-process/).

### grant

A license scanning tool that analyzes container images for software licenses. Integrated into `gh aw compile` via the `--grant` flag. Uses a `.grant.yaml` policy file to define allowed and disallowed licenses, then evaluates each container image referenced in the workflow. Typically used alongside [syft](#syft) for SBOM generation. See [Compilation Reference](/gh-aw/reference/compilation-process/).

### poutine

A security linter for GitHub Actions workflows that detects supply-chain vulnerabilities such as unpinned actions and dangerous use of pull request events. Integrated into `gh aw compile` via the `--poutine` flag. Typically used alongside [actionlint](#actionlint) and [zizmor](#zizmor).

### syft

A Software Bill of Materials (SBOM) generation tool that catalogs packages and dependencies in container images. Integrated into `gh aw compile` via the `--syft` flag. Produces a structured inventory of all software components in Docker images used by the workflow. Typically used alongside [grant](#grant) for license policy enforcement. See [Compilation Reference](/gh-aw/reference/compilation-process/).

### ssljson

A custom Go static-analysis linter (`pkg/linters/ssljson`) that validates Scheduling-Structural-Logical (SSL) JSON scene and logic-step graphs, reporting duplicate scene or logic-step IDs and dangling `entry_logic_step` or `scene_id` references between scenes and steps. Part of the gh-aw linter registry used in CI. See [Linters README](https://github.com/github/gh-aw/blob/main/pkg/linters/README.md).

### manualpathconcat

A custom Go static-analysis linter (`pkg/linters/manualpathconcat`) that flags manual `"/"`-based path concatenation (for example, `dir + "/" + name`) in favor of `filepath.Join`. Part of the gh-aw linter registry used in CI to enforce internal Go code-quality conventions. See [Linters README](https://github.com/github/gh-aw/blob/main/pkg/linters/README.md).

### Validation

Checking workflow files for errors, security issues, and best practices. Occurs during compilation and can be enhanced with strict mode and security scanners.

### `gh aw lint`

A CLI command that runs actionlint on existing `.lock.yml` workflow files without recompiling the source Markdown. Unlike `gh aw compile --actionlint`, it reads lock files directly from disk, skipping `zizmor` and `poutine`. Supports `--shellcheck` and `--pyflakes` flags to enable script integrations for shell and Python analysis. Useful for fast local feedback after manual lock-file edits. See [CLI Reference](/gh-aw/setup/cli/).

### yamllint

A YAML linting tool that validates the syntax and style of generated workflow files. Integrated into `gh aw compile` via the `--yamllint` flag. Runs against the compiled `.lock.yml` output to catch YAML formatting issues before the workflow is executed. See [Compilation Reference](/gh-aw/reference/compilation-process/).

### zizmor

A security auditing tool for GitHub Actions workflows that identifies vulnerabilities including script injections, excessive permissions, and unsafe use of GitHub context expressions. Integrated into `gh aw compile` via the `--zizmor` flag. Typically used alongside [actionlint](#actionlint) and [poutine](#poutine).

### Deterministic Lineage

The causal graph of edges between workflow runs computed by `gh aw logs --json`. Each edge connects a source run to a target run and captures how one run triggered another — via `workflow_dispatch`, `workflow_call`, or `workflow_run` events — along with a confidence rating and the reasons the link was established. Available under `.edges[]` in the JSON output. Use lineage data to reconstruct orchestrator-to-worker relationships without manually correlating run IDs.

### Episode

A deterministic rollup of related workflow runs that belong to a single logical execution. When an orchestrator dispatches workers, all participating runs are grouped into one episode with aggregate metrics including `total_runs`, `total_tokens`, `total_aic`, and `risky_node_count`. Available under `.episodes[]` in `gh aw logs --json` output. Episodes are more useful than per-run metrics when one logical job spans multiple workflow runs. For cost analysis, prefer `total_aic` (AI Credits).

```bash
gh aw logs --start-date -30d --json | \
  jq '.episodes[] | {id: .episode_id, workflow: .primary_workflow, aic: .total_aic}'
```

### WebAssembly (Wasm)

A compilation target allowing the gh-aw compiler to run in browser environments without server-side Go installation. The compiler is built as a `.wasm` module that packages markdown parsing, frontmatter extraction, import resolution, and YAML generation into a single file loaded with Go's `wasm_exec.js` runtime. Enables interactive playgrounds, editor integrations, and offline workflow compilation tools. See [WebAssembly Compilation](/gh-aw/reference/wasm-compilation/).

## Advanced Features

### Autoloop

A GitHub Next project that builds on GitHub Agentic Workflows to enable continuous, metric-driven optimization. Define a goal, a set of files the agent may modify, and an evaluation command that outputs a numeric metric — Autoloop runs on a schedule, proposes changes, and retains only those that improve the metric. Useful for continuously improving test coverage, bundle size, build times, or custom research objectives. See [Autoloop on GitHub](https://github.com/githubnext/autoloop).

### ARC (Actions Runner Controller)

A Kubernetes operator that manages GitHub Actions self-hosted runners as pods. When combined with the Docker-in-Docker (DinD) sidecar pattern, the runner container and the Docker daemon container have separate `/tmp` filesystems. AWF detects this topology at runtime by inspecting `DOCKER_HOST` and automatically passes `--docker-host-path-prefix` to bridge the split mount paths. From AWF `v0.27.1`+, AWF also automatically injects the `chroot.binariesSourcePath` and `chroot.identity.*` config fields at runtime, so workflows no longer need a bootstrap action to copy binaries or pre-seed `/etc/passwd`. No manual configuration is required. See the [AWF sandbox reference](/gh-aw/reference/sandbox/).

### Runner Topology (`runner.topology`)

A frontmatter field that declares the runner infrastructure topology for a workflow. The only supported value is `arc-dind`, which targets GitHub ARC (Actions Runner Controller) runners using rootless Docker-in-Docker. When set, the compiler emits the topology in the AWF config, redirects the tool cache to a shared volume, and validates that no generated step requires root. AWF then activates split-filesystem handling, network isolation, sysroot staging, and DinD pre-staging automatically.

```aw wrap
runner:
  topology: arc-dind
```

See [ARC](#arc-actions-runner-controller) and [Sandbox Configuration](/gh-aw/reference/sandbox/).

### AWF (Agent Workflow Firewall)

The default coding agent sandbox that isolates AI agent execution in a container with network egress control through domain-based access lists. AWF makes the host filesystem and environment variables available inside the container while restricting outbound network access to configured domains. Enabled with `sandbox.agent: awf` (the default when `sandbox` is not specified). Use `sandbox.agent.version` to pin a specific AWF release for reproducible builds. See [Sandbox Configuration](/gh-aw/reference/sandbox/).

### AWF Reflect Route (`/reflect`)

A runtime HTTP endpoint exposed by the AWF API proxy at `http://api-proxy:10000/reflect`. Returns the currently configured inference providers and their model availability for the active run. Use this route in shared workflows or tools that need to discover gateway endpoints, check provider availability, or select a model dynamically at runtime without hardcoding upstream API URLs. The response includes an `endpoints` array (with `provider`, `base_url`, `configured`, and `models` fields) and a `models_fetch_complete` flag. See [AWF Reflect Route](/gh-aw/experimental/awf-reflect/).

### Bridge Pattern

A cross-repository event forwarding architecture for side repository workflows. Because GitHub Actions only delivers webhook events to the repository where they occur, `slash_command:` triggers cannot fire directly in a side repository. The bridge pattern solves this with two workflows: a thin relay workflow in the main repository that receives the slash command and forwards it to the side repository via `workflow_dispatch`, and a worker workflow in the side repository that performs the actual work. See [Triage from Side Repo](/gh-aw/gallery/multi-repo/triage-from-side-repo/).

### Cache Memory

Persistent storage for workflows preserving data between runs using GitHub Actions cache. Cache memory is branch-scoped: runs restore from the current branch and may restore from the default branch (`main` in most repositories). After a non-default branch restores from default, later saves remain branch-local. Configured via `cache-memory:` in the tools section with 7-day retention. See [Cache Memory](/gh-aw/reference/cache-memory/).

### Comment Memory (`tools.comment-memory`)

Persistent agent memory backed by a managed GitHub issue or PR comment. Before each agent run, content from `<gh-aw-comment-memory>` blocks in the target comment is extracted into markdown files under `/tmp/gh-aw/comment-memory/`. Agents edit these files using standard file tools; the safe-output handler automatically upserts the managed comment after the run. Unlike [Cache Memory](#cache-memory) (7-day GitHub Actions cache retention) and [Repo Memory](#repo-memory) (permanent git branch storage), comment memory uses the triggering issue or PR as its backing store — no separate infrastructure required. Configured via `tools.comment-memory:` in frontmatter.

### Command Triggers

Special triggers responding to slash commands in issue and PR comments. Configured using the `slash_command:` section with a command name.

### Centralized Slash-Command Strategy (`strategy: centralized`)

An opt-in compilation mode for `slash_command:` workflows where the compiler generates a single shared `agentic_commands.yml` router workflow. The router listens to merged slash-command events and dispatches matching target workflows via `workflow_dispatch` with an `aw_context` payload. Enables combining slash commands with non-slash events (such as `issues` or `pull_request`) without trigger conflicts. Opt in by setting `on.slash_command.strategy: centralized`. See [Command Triggers](/gh-aw/reference/command-triggers/).

### Builtin `/help` Command

An auto-generated slash command available when [centralized routing](#centralized-slash-command-strategy-strategy-centralized) (`on.slash_command.strategy: centralized`) is active. Intercepts `/help` before normal route dispatch and posts a comment listing all available slash commands with their descriptions and a link to the command-triggers documentation. The command inventory is computed at compile time and embedded as environment variables in the generated central workflow. Disable per-repository by setting `help_command: false` in `.github/workflows/aw.json`. See [Command Triggers](/gh-aw/reference/command-triggers/).

### `aw_context`

A structured context payload passed by the centralized slash-command router (`agentic_commands.yml`) when dispatching target workflows via `workflow_dispatch`. Contains the original GitHub event context (issue number, repository, actor, etc.) so the dispatched workflow can act on the correct resource even though it is triggered as a `workflow_dispatch`. See [Command Triggers](/gh-aw/reference/command-triggers/).

### Conclusion Job

An automatically generated job in compiled workflows that handles post-agent reporting and cleanup. Receives a workflow-specific concurrency group (`gh-aw-conclusion-{workflow-name}`) to prevent collision when multiple agent instances run the same workflow concurrently. Requires no manual configuration — the compiler sets the group automatically. See [Concurrency Control](/gh-aw/reference/concurrency/).

### Concurrency Control

Settings limiting how many workflow instances can run simultaneously. Configured via `concurrency:` field to prevent resource conflicts or rate limiting.

### `queue: max`

A GitHub Actions concurrency modifier the compiler emits on the top-level workflow concurrency group of compiled lock files, alongside the `group` key. It replaces cancel-in-progress semantics for schedule- and push-triggered agentic workflows: queued runs execute sequentially instead of cancelling pending runs, since `queue: max` cannot be combined with `cancel-in-progress: true`. This avoids false-positive failures caused by cancelled runs on frequently triggered workflows.

### Custom Agents

Specialized instructions customizing AI agent behavior for specific tasks or repositories. Stored as agent files (`.github/agents/*.agent.md`) for Copilot Chat or instruction files (`.github/copilot/instructions/`) for path-specific Copilot instructions.

### Ephemerals

A category of features for automatically expiring workflow resources to reduce repository noise and control costs. Includes workflow stop-after scheduling, safe output expiration (auto-closing issues, discussions, and pull requests), and hidden older status comments. See [Ephemerals](/gh-aw/reference/ephemerals/).

### Source-to-Destination Mapping (`includes:`)

An `includes` entry in an `aw.yml` package manifest that pairs a `source` path, resolved relative to the package root, with a `destination` path, resolved relative to the consuming repository root, plus an optional `kind` of `agentic-workflow` or `action-workflow`. This lets a distribution repository keep workflow assets outside `.github/workflows/` — so they stay inert in the source repository — while installing them into the consuming repository's `.github/workflows/` via `gh aw add`, `gh aw add-wizard`, or `gh aw update`. A string or mapping `source` may end in a single non-recursive `/*` wildcard to include supported direct children of that directory; for wildcard mappings, `destination` must be the `.github/workflows/` folder, and each matching file keeps its source filename. The compiler rejects mappings that use absolute paths, `..` traversal, symlinks, unsupported or `.lock.yml` extensions, extension mismatches between source and destination, or destinations outside `.github/workflows/` (wildcard mappings excepted, since they target the folder itself). Two entries installing to the same destination are rejected before any file is written. See [Package Manifest Reference](/gh-aw/reference/aw-yml-package-manifest/).

### Package Visibility Metadata (`private`, `experimental`)

Two optional boolean fields on an `aw.yml` package manifest that signal installation availability and stability. `private` (default `false`) marks the package as unavailable for installation; `gh aw add` refuses packages set to `true`. `experimental` (default `false`) marks the package as lower-stability; `gh aw add` displays a warning but still allows installation. Manifests that omit either field retain the default (public, non-experimental) behavior. See [Package Manifest Reference](/gh-aw/reference/aw-yml-package-manifest/).

### Environment Variables (env)

Configuration section in frontmatter defining environment variables for the workflow. Variables can reference GitHub context values, workflow inputs, or static values. Accessible via `${{ env.VARIABLE_NAME }}` syntax.

### `GITHUB_AW`

A system-injected environment variable set to `"true"` in every gh-aw engine execution step (both the agent run and the threat-detection run). Agents can check this variable to confirm they are running inside a GitHub Agentic Workflow. Cannot be overridden by user-defined `env:` blocks. See [Environment Variables Reference](/gh-aw/reference/environment-variables/).

### `GH_AW_PHASE`

A system-injected environment variable identifying the active execution phase. Set to `"agent"` during the main agent run and `"detection"` during the threat-detection safety check run that precedes it. Cannot be overridden by user-defined `env:` blocks. See [Environment Variables Reference](/gh-aw/reference/environment-variables/).

### `GH_AW_VERSION`

A system-injected environment variable containing the gh-aw compiler version that generated the workflow (e.g. `"0.40.1"`). Useful for writing conditional logic that depends on a minimum feature version. Cannot be overridden by user-defined `env:` blocks. See [Environment Variables Reference](/gh-aw/reference/environment-variables/).

### `GH_AW_ALLOWED_DOMAINS`

A system-injected environment variable containing the comma-separated list of domains allowed by the workflow's network configuration. Used by safe output jobs for URL sanitization — URLs from unlisted domains are redacted in AI-generated content before it is applied. Automatically populated from `network.allowed` domains and, when `engine.api-target` is set, includes the GHES/GHEC API hostname and base domain. Cannot be overridden by user-defined `env:` blocks. See [Environment Variables Reference](/gh-aw/reference/environment-variables/).

### `GH_AW_DEFAULT_*`

A family of environment variables set in the compiler process environment or as GitHub Actions `vars.*` to apply organization- or repository-wide defaults without editing individual workflow frontmatter. Compiler-process variables (`GH_AW_DEFAULT_MAX_TURNS`, `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES`, `GH_AW_DEFAULT_DETECTION_MODEL`) inject defaults at compile time by being read when `gh aw compile` runs; runtime repository variables (`GH_AW_DEFAULT_MAX_AI_CREDITS`, `GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS`, `GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS`, `GH_AW_DEFAULT_TIMEOUT_MINUTES`, `GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES`, `GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES`, `GH_AW_DEFAULT_MODEL_COPILOT`, `GH_AW_DEFAULT_MODEL_CLAUDE`, `GH_AW_DEFAULT_MODEL_CODEX`) are embedded as `${{ vars.* }}` expressions in the compiled workflow and resolved by the GitHub Actions runner at execution time. Frontmatter settings always take precedence over `GH_AW_DEFAULT_*`. Managed in batch via `gh aw env`. See [Compiler Enterprise Environment Controls](/gh-aw/reference/compiler-enterprise-environment-controls/).

### `GH_AW_POLICY_*`

A family of boolean GitHub Actions variables that enforce runtime capability gates without requiring workflow recompilation. Where [`GH_AW_DEFAULT_*`](#gh_aw_default_) variables tune numeric and model settings, policy variables permit or refuse specific behaviors at runtime — any value other than `"false"` leaves the feature enabled. Set at repository, organization, or enterprise scope via `vars.*`; the most-specific-wins rule applies, so a repository-level `"true"` overrides an org-level `"false"`.

Currently defined:
- `GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST` — disables `safe-outputs.create-pull-request` when set to `"false"`.

See [Governance](/gh-aw/reference/governance/#disabling-create-pull-request-org-wide) and [Runtime Policy Variables](/gh-aw/reference/environment-variables/#runtime-policy-variables).

### `GH_DEBUG=api`

An environment variable that enables verbose request/response logging for GitHub API calls made through go-gh's native REST and GraphQL clients. Setting `GH_DEBUG=api` before a `gh aw` command (for example, `GH_DEBUG=api gh aw compile my-workflow`) prints detailed API call diagnostics with no extra flags required, useful for troubleshooting authentication, rate-limiting, or unexpected API responses. See [Debugging Guide](/gh-aw/troubleshooting/debugging/).

### `GH_HOST`

An environment variable recognized by the `gh` CLI that specifies the GitHub hostname for GitHub Enterprise Server (GHES) or GitHub Enterprise Cloud (GHEC) deployments. When set, `gh` commands target the specified enterprise instance instead of `github.com`. Agentic workflows automatically configure this from `GITHUB_SERVER_URL` at agent job startup; the variable is also propagated to custom frontmatter jobs and the safe-outputs job so all `gh` calls target the correct enterprise host. `gh aw trial` (including `--clone-repo` and `--trigger-context`) also honors `GH_HOST` — all trial repository URLs are built against the resolved host instead of hard-coded `github.com`, so trials work correctly against GHES instances. See [Environment Variables Reference](/gh-aw/reference/environment-variables/).

### Label Command Trigger (`label_command`)

A trigger that activates a workflow when a specific label is added to an issue, pull request, or discussion. Unlike standard label filtering, the label command trigger automatically removes the applied label on activation so it can be reapplied to re-trigger the workflow. Configured via `label_command:` in the `on:` section; exposes `needs.activation.outputs.label_command` with the matched label name for downstream jobs. Can be combined with `slash_command:` to support both label-based and comment-based triggering. See [LabelOps patterns](/gh-aw/patterns/label-ops/).

```yaml wrap
on:
  label_command: deploy
```

### Repo Memory

Persistent file storage via Git branches with unlimited retention. Unlike cache-memory (7-day retention), repo-memory stores files permanently in dedicated Git branches with automatic branch cloning, file access, commits, pushes, and merge conflict resolution. Setting `wiki: true` switches the backing to the GitHub Wiki's git endpoint (`{repo}.wiki.git`), and the agent receives guidance to follow GitHub Wiki Markdown conventions (e.g. `[[Page Name]]` links). See [Repo Memory](/gh-aw/reference/repo-memory/).

### Sandbox

Configuration for the AI agent execution environment, providing two isolation layers: the **Coding Agent Sandbox** ([AWF](#awf-agent-workflow-firewall) by default) for network egress control, and the **MCP Gateway** for routing MCP server calls through a unified HTTP endpoint. Configured via the `sandbox:` field in frontmatter. See [Sandbox Configuration](/gh-aw/reference/sandbox/).

### `sandbox.agent.runtime` (`sandbox.agent.runtime`)

A `sandbox.agent` field that selects the sandbox security and topology profile. It is the single selector for the supported combinations of container runtime, AWF privileges, and host access; the compiler derives every privilege the selected profile needs.

- `docker` (default) — Default Docker runtime, rootless AWF, network isolation.
- `docker-sudo-iptables` — Docker with privileged AWF, legacy `iptables` networking, and host/service access.
- `gvisor` — gVisor with strict network isolation.
- `docker-sbx` — KVM microVM; the compiler handles the required privileged setup.
- `cloud-hypervisor` — Preview KVM runtime with its required privileged launcher.

Omitting `runtime` is equivalent to `runtime: docker`. The removed `sandbox.agent.sudo` and `sandbox.agent.legacy-security` fields are migrated by `gh aw fix --write`. See [Sandbox Configuration](/gh-aw/reference/sandbox/) and [Agent Runtimes](/gh-aw/reference/agent-runtimes/).

```aw wrap
sandbox:
  agent:
    runtime: docker-sudo-iptables
    allow-host-ports: [9000]
```

### `sandbox.agent.mounts` (`sandbox.agent.mounts`)

A `sandbox.agent` field that specifies additional container mounts for the AWF coding agent sandbox. Each entry uses the format `"source:dest:mode"` (e.g., `"/host/path:/container/path:ro"`). Shared workflow partials can define their own `sandbox.agent.mounts` entries; the compiler merges them additively with the importing workflow's mounts, so partial authors can declare the mounts they need without requiring the importer to know about them. See [Sandbox Configuration](/gh-aw/reference/sandbox/).

```aw wrap
sandbox:
  agent:
    mounts:
      - /run/docker.sock:/var/run/docker.sock:ro
```

### Host Service Ports (`services:`)

A GitHub Actions `services:` container port that the AWF coding agent sandbox reaches via `--allow-host-service-ports`, which resolves each service's actual (possibly dynamically assigned) host port at runtime. Requires `sandbox.agent.runtime: docker-sudo-iptables`, since the default (strict) runtime profile provides no route to host services. For host daemons not declared under `services:`, the related `allow-host-ports` escape hatch (also `docker-sudo-iptables` only) allowlists specific TCP ports; the compiler rejects ports outside the `1`–`65535` range and blocks dangerous ports (e.g. `22`, `3306`, `5432`, `6379`, `9200`). See [Sandbox Configuration](/gh-aw/reference/sandbox/#host-service-ports-services).

```aw wrap
sandbox:
  agent:
    runtime: docker-sudo-iptables

services:
  postgres:
    image: postgres:18
    ports:
      - 5432:5432
```

### Token Steering (`sandbox.agent.token-steering`)

A `sandbox.agent` boolean field, enabled by default, that controls whether AWF's API proxy actively steers Copilot requests through dynamic token/provider routing. Set `token-steering: false` to preserve the explicitly configured provider and model without proxy interception. See [Sandbox Configuration](/gh-aw/reference/sandbox/#token-steering-sandboxagenttoken-steering).

```yaml wrap
sandbox:
  agent:
    token-steering: false
```

### Excluded Env (`excluded-env`)

A top-level frontmatter field that lists environment variable names to unconditionally exclude from the AWF agent container via `--exclude-env`. Use this when an environment variable is set from a source the compiler cannot automatically detect as credential-bearing — for example, a `workflow_dispatch` input that carries an ephemeral token generated by an upstream job. Names are deduplicated and merged with those auto-detected from `secrets.*` and `needs.*.outputs.*` references.

```aw wrap
excluded-env:
  - MY_DISPATCH_TOKEN
  - ANOTHER_SENSITIVE_VAR
```

See [Sandbox Configuration](/gh-aw/reference/sandbox/).

### `sandbox.agent.runtime`

A `sandbox.agent` field that selects the container runtime used to execute the AI agent. Supported values:

- `gvisor` — Runs the agent container under [gVisor](#gvisor-runsc) (`runsc`) for kernel-level isolation. Best for workflows processing untrusted input.
- `docker-sbx` — Runs the agent inside a [docker-sbx](#docker-sbx) KVM-isolated microVM while keeping infrastructure containers on the host.
- `cloud-hypervisor` — Runs the agent inside AWF's preview Cloud Hypervisor microVM runtime (GitHub-hosted Ubuntu x86_64 with `/dev/kvm` only).

When omitted, the default Docker runtime is used. See [Sandbox Configuration](/gh-aw/reference/sandbox/).

```aw wrap
sandbox:
  agent:
    runtime: gvisor
```

### gVisor (runsc)

A container runtime from Google that interposes a user-space kernel between the containerized application and the host OS kernel. When `sandbox.agent.runtime: gvisor` is set, the agent container runs under gVisor's `runsc` runtime, providing stronger isolation than standard Docker — useful for workflows that process untrusted input. gh-aw installs and registers gVisor automatically before the agent container starts. See [Sandbox Configuration](/gh-aw/reference/sandbox/).

### docker-sbx

A KVM-hardware-virtualized microVM runtime. When `sandbox.agent.runtime: docker-sbx` is set, the AI agent runs inside a hardware-isolated microVM while infrastructure containers (MCP servers, gateway, etc.) remain on the host. Provides stronger isolation than gVisor for workloads that require full hardware-virtualization boundaries. gh-aw automatically refreshes Docker Hub OAuth credentials immediately before agent execution to prevent token expiry errors. See [Sandbox Configuration](/gh-aw/reference/sandbox/).

### cloud-hypervisor

A preview KVM-hardware-virtualized microVM runtime in AWF. When `sandbox.agent.runtime: cloud-hypervisor` is set, gh-aw grants the runner scoped KVM access and emits host eligibility checks plus digest-pinned release-asset provisioning for the Cloud Hypervisor binary, `virtiofsd`, kernel, rootfs, and supervisor bundle. AWF uses the host privileges needed to create the VM while retaining strict topology isolation. Support is intentionally limited to GitHub-hosted Ubuntu x86_64 runners with `/dev/kvm`. See [Sandbox Configuration](/gh-aw/reference/sandbox/).

### Strict Mode

Enhanced validation mode enforcing additional security checks and best practices. Enabled via `strict: true` in frontmatter or `--strict` flag when compiling. Workflows compiled with `strict: false` cannot run on public repositories. A repository can set `"strict": true` at the top level of `.github/workflows/aw.json` to force strict mode for every `gh aw compile` invocation, preventing individual workflows from opting out; the repository setting only accepts `true` (`"strict": false` is rejected as invalid). See [Frontmatter](/gh-aw/reference/frontmatter/#strict-mode-strict).

### Time Scattering

Automatic distribution of workflow execution times across the day to reduce load spikes on GitHub Actions infrastructure. When using fuzzy scheduling, the compiler deterministically assigns different start times to each workflow based on repository and workflow name. Prevents all scheduled workflows from running simultaneously at common times like midnight or the top of the hour.

### Timeout

Maximum duration a workflow can run before automatic cancellation. Configured via `timeout-minutes:` in frontmatter. The agent execution step defaults to 20 minutes; other jobs (custom jobs, safe-output jobs) use the GitHub Actions platform default of 360 minutes unless explicitly set. Custom runners support longer timeouts beyond the GitHub-hosted runner limit.

### Toolsets

Predefined collections of related MCP tools enabled together. Used with the GitHub MCP server to group capabilities like `repos`, `issues`, and `pull_requests`. Configured in the `toolsets:` field.

### Tracker ID

A unique identifier enabling external monitoring and coordination without bidirectional coupling. Orchestrator workflows use tracker IDs to correlate worker runs and discover outputs while workers operate independently.

### Workflow Inputs

Parameters provided when manually triggering a workflow with `workflow_dispatch`. Defined in the `on.workflow_dispatch.inputs` section with type, description, default value, and required status.

### Workflow Documentation URL (`metadata.docs`)

An optional field under top-level `metadata:` frontmatter holding an absolute HTTPS URL to human-facing documentation for a workflow. The compiler preserves the value in the generated lock file's metadata without fetching it or altering workflow execution. See [Frontmatter Reference](/gh-aw/reference/frontmatter-full/).

### Graders (`graders:`)

Deterministic, non-LLM checks that compute metrics from a workflow run's post-agent execution trace. Configured under the top-level `graders:` frontmatter field; an empty map (`graders: {}`) enables all built-in graders with default settings, and omitting the field disables grading entirely. Built-in graders cover tool success rate, retries, loop detection, trajectory efficiency, execution duration, and similar metrics. Custom inline graders run a trusted, sandboxed JavaScript expression against the preprocessed `trace` object. Graders are an experimental feature. See [Graders Reference](/gh-aw/experimental/trace-graders/).

### Operational Value Grader (`graders.operational-value`)

A reserved grader that evaluates operational repository outcomes using a repository-relative Bash evaluator script, frozen at compile time with its SHA-256 digest recorded for reproducibility. It returns an absolute operational attainment value in `[0,1]` for the run's assigned case, alongside the evaluation's evidence timestamp, maturity, and provenance. A frozen baseline is optional metadata used to derive `deltaFromBaseline` without changing the primary operational value. The evaluator receives the workflow token via `GH_TOKEN` with the agent job's declared permissions, but not workflow secrets, and enabling the grader does not add evidence permissions to the agent job. Historical runs can be recomputed with `gh aw graders operational-value <run-id> --evidence-at <timestamp>`. See [Graders Reference](/gh-aw/experimental/trace-graders/#operational-value-grader) and the `operational value designer` skill (`/operational-value-designer`) for inferring operational value from an agentic workflow.

### Recurrence Trapping Time (RQA TT) (`graders.recurrence-trapping-time`)

A Recurrence Quantification Analysis (RQA) grader that measures the mean length of vertical line structures (length ≥ 2) in the state-recurrence matrix built from a run's canonical state/event sequence. A vertical line means consecutive steps all match one previously visited state, so trapping time estimates how long the agent stays stuck once it enters a stagnation episode; lower values are better. It complements `recurrence-laminarity`, which measures how much of the recurrent structure is vertical rather than how long each vertical episode lasts. It is not a built-in grader: it is an importable `graders:` fragment in the trajectory graders catalog (`.github/workflows/shared/graders/recurrence-trapping-time.md`) that a workflow opts into via `imports:`. See [Graders Reference](/gh-aw/experimental/trace-graders/) for built-in graders and grader configuration.

### Trajectory IR

The canonical, schema-defined intermediate representation of an agent run's execution trace, consumed by trajectory graders. It normalizes a run into `events[]` (ordered kinds such as `tool_call` and `state_change`), `observations[]` (each optionally carrying a `consumedByActionIds` list identifying which later actions referenced it), declared `states[]` and `objectives[]`, and optional `config.constraints`. Graders project purely from this structure rather than parsing raw logs, so new graders only need to reason about the IR's fields. See [Graders Reference](/gh-aw/experimental/trace-graders/).

### Trajectory Graders (importable `graders:` fragments)

A catalog of non-built-in, importable grader definitions under `.github/workflows/shared/graders/` that a workflow opts into individually via `imports:`. Each fragment computes a single metric from the [Trajectory IR](#trajectory-ir) — for example `tool-output-consumption-rate` (fraction of tool outputs later referenced by an action), `exploration-error` and `exploitation-error` (complementary attributions of objective failure to insufficient search versus unused evidence), `event-entropy-rate` and `lempel-ziv-trajectory-complexity` (information-theoretic measures of event-sequence unpredictability and compressibility), `policy-near-miss` (successful runs that skipped guard-shaped objectives), the Recurrence Quantification Analysis family `recurrence-rate`, `recurrence-laminarity`, `recurrence-determinism`, and `recurrence-trapping-time`, `skill-constraint-coverage` (fraction of declared behavioral constraints exercised and satisfied), and `state-revisit-probability-rep` (fraction of visited states that are redundant revisits). See [Graders Reference](/gh-aw/experimental/trace-graders/) and `.github/workflows/shared/graders/README.md`.

## Operational Patterns

Operational patterns (suffixed with "-Ops") are established workflow architectures for common automation scenarios. Each pattern addresses specific use cases with recommended triggers, tools, and safe outputs.

### AgenticOps

Repository-wide observability pattern where a scheduled workflow inspects other agentic workflows, classifies notable behavior, and publishes a structured report. When it detects repeated failures, abnormal token consumption, or other unhealthy patterns, it escalates findings into issues for follow-up. Creates a durable operational record instead of relying on ad hoc inspection of individual runs. See [MonitorOps](/gh-aw/patterns/monitor-ops/).

### BatchOps

Pattern for processing large volumes of work items efficiently using chunked pagination, matrix fan-out, or rate-limit-aware sub-batching. BatchOps splits a backlog into parallel or sequential chunks, handles partial failures with `fail-fast: false`, and aggregates results into a consolidated report. Use when items are independent and order doesn't matter. See [BatchOps](/gh-aw/patterns/batch-ops/).

### Central Control Plane

A [MultiRepoOps](#multirepoops) topology where a single private repository acts as a control plane for coordinating large-scale operations across many repositories. An orchestrator workflow filters and prioritizes targets, then dispatches per-repo worker workflows. Enables phased rollouts, policy updates, and centralized tracking using cross-repository safe outputs and secure authentication. See [MultiRepoOps — Central Control Plane](/gh-aw/patterns/central-repo-ops/#using-a-central-control-repository).

### CorrectionOps

Pattern for improving workflows from trusted human corrections without retraining the underlying model. CorrectionOps stores predictions, compares them with later authoritative human decisions, and uses grouped diffs to update instructions, routing, thresholds, or rollout policy. See [CorrectionOps](/gh-aw/experimental/correction-ops/).

### ChatOps

Interactive automation triggered by slash commands (`/review`, `/deploy`) in issues and pull requests, enabling human-in-the-loop automation where developers invoke AI assistance on demand. See [ChatOps](/gh-aw/patterns/chat-ops/).

### DailyOps

Scheduled workflows for incremental daily improvements, automating progress toward large goals through small, manageable changes on weekday schedules.

### DispatchOps

Manual workflow execution via GitHub Actions UI or CLI using `workflow_dispatch` trigger. Enables on-demand tasks, testing, and workflows requiring human judgment about timing. Workflows can accept custom input parameters. See [DispatchOps](/gh-aw/patterns/dispatch-ops/).

### IssueOps

Automated issue management that analyzes, categorizes, and responds to issues when created. Uses issue event triggers with safe outputs for secure automated triage without requiring write permissions for the AI job. See [IssueOps Examples](/gh-aw/patterns/issue-ops/).

### LabelOps

Workflows triggered by label changes on issues and pull requests. Uses labels as triggers, metadata, and state markers with filtering for specific label additions or removals. See [LabelOps Examples](/gh-aw/patterns/label-ops/).

### MemoryOps

Stateful workflows that persist data between runs using `cache-memory` and `repo-memory`, enabling progress tracking, resumption after interruptions, and incremental processing to avoid API throttling. See [MemoryOps](/gh-aw/patterns/memory-ops/).

### MultiRepoOps

Cross-repository coordination extending automation patterns across multiple repositories. Uses secure authentication and cross-repository safe outputs to synchronize features, centralize tracking, and enforce organization-wide policies. See [MultiRepoOps](/gh-aw/patterns/multi-repo-ops/).

### ProjectOps

AI-powered GitHub Projects board management automating issue triage, routing, and field updates. Analyzes issue/PR content to make intelligent decisions about project assignment, status, priority, and custom fields using the `update-project` safe output. See [ProjectOps](/gh-aw/patterns/project-ops/).

### Side Repository

A [MultiRepoOps](#multirepoops) topology where workflows run from a separate dedicated automation repository targeting your main codebase. Keeps AI-generated issues, comments, and workflow runs isolated from the main repository for cleaner separation between automation infrastructure and production code. See [MultiRepoOps — Side Repository](/gh-aw/patterns/multi-repo-ops/#using-a-side-repository).

### SpecOps

Maintaining and propagating W3C-style specifications using the `w3c-specification-writer` agent. Creates formal specifications with RFC 2119 keywords and automatically synchronizes changes to consuming implementations. See [SpecOps](/gh-aw/patterns/spec-ops/).

### ResearchPlanAssignOps

Scaffolded AI-powered code improvement strategy with four phases: research agent investigates and publishes findings, developer reviews and invokes planner agent to create actionable issues, developer assigns approved issues to Copilot for automated implementation, then reviews and merges PRs. Keeps developers in control with clear decision points at every transition. See [ResearchPlanAssignOps](/gh-aw/patterns/research-plan-assign-ops/).

### TrialOps

Testing and validation pattern executing workflows in isolated trial repositories before production deployment. Creates temporary private repositories where workflows run safely, capturing safe outputs without modifying your actual codebase. See [TrialOps](/gh-aw/experimental/trial-ops/).

### Logical Repository Mode (`--logical-repo`)

A `gh aw trial` flag that simulates running a workflow as if it targeted a different repository, while workflow outputs and safe-output side effects remain contained in the trial repository. Lets you validate repository-specific behavior — such as `target-repo` routing or repo-scoped permissions — without touching the real target repository. See [TrialOps](/gh-aw/experimental/trial-ops/).

### Trial Result Success (`success`, `safe_output_errors`)

Fields in the JSON result written to `trials/*.json` by `gh aw trial`. The boolean `success` field gives an explicit pass/fail signal for each workflow result, and the `safe_output_errors` array is populated with rejected-message errors whenever safe-output processing rejects one or more requested actions. When `safe_output_errors` is non-empty, `gh aw trial` exits with a non-zero status instead of reporting unconditional success. See [TrialOps](/gh-aw/experimental/trial-ops/).

### WorkQueueOps

Pattern for incrementally processing a backlog of work items using a durable queue backend — issue checklists, sub-issues, [cache-memory](#cache-memory), or GitHub Discussions. Each run picks up where the last left off, making it resilient to interruptions and rate limits. Items should be idempotent and independently processable. See [WorkQueueOps](/gh-aw/patterns/workqueue-ops/).

## Related Resources

For more detail, see the [Frontmatter Reference](/gh-aw/reference/frontmatter/), [Tools Reference](/gh-aw/reference/tools/), [MCP Scripts Reference](/gh-aw/reference/mcp-scripts/), [Safe Outputs Reference](/gh-aw/reference/safe-outputs/), [Using MCPs Guide](/gh-aw/guides/mcps/), [Security Guide](/gh-aw/introduction/architecture/), and [AI Engines Reference](/gh-aw/reference/engines/).
